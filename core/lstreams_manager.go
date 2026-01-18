package core

import (
	"fmt"
	"math/rand"
	"os/user"
	"sort"
	"strings"
	"time"

	"github.com/dimonomid/clock"
	"github.com/dimonomid/ssh_config"
	"github.com/juju/errors"

	"github.com/dimonomid/nerdlog/log"
)

var ErrBusyWithAnotherQuery = errors.Errorf("busy with another query")
var ErrNotYetConnected = errors.Errorf("not connected to all lstreams yet")

type LStreamsManager struct {
	// 入参
	params LStreamsManagerParams
	// 解析后的 logstreams 字符串
	lstreamsStr string
	// 解析后的 logstreams 列表
	parsedLogStreams map[string]LogStream
	// log stream 客户端列表
	lscs map[string]*LStreamClient
	// log stream 客户端状态列表
	lscStates map[string]LStreamClientState
	// lscConnDetails contains items for all selected lstreams, even after the
	// connection is done (which is indicated by ConnDetails.Connected being
	// true).
	lscConnDetails map[string]ConnDetails
	// lscBusyStages only contains items for lstreams which are in the
	// LStreamClientStateConnectedBusy state.
	lscBusyStages map[string]BusyStage

	// lscPendingTeardown contains info about LStreamClient-s that are being torn
	// down. NOTE that when a LStreamClient starts tearing down, its key changes
	// (gets prepended with OLD_XXXX_), so re remove an item from the `has` map
	// with one key, and add an item here with a different key.
	// 包含正在关闭的 LStreamClient 信息，注意当 LStreamClient 开始关闭时，
	// 它的 key 会改变（前面会加上 OLD_XXXX_），所以要用不同的 key 从 `has` map 中删除一个项，并在这里添加一个项。
	lscPendingTeardown map[string]int

	lstreamsByState map[LStreamClientState]map[string]struct{}
	numNotConnected int

	lstreamUpdatesCh chan *LStreamClientUpdate
	reqCh            chan lstreamsManagerReq
	respCh           chan lstreamCmdRes

	// teardownReqCh is written to once when Close is called.
	teardownReqCh chan struct{}
	// tearingDown is true if the teardown is in progress (after Close is called).
	tearingDown bool
	// torndownCh is closed once the teardown is fully completed.
	// Wait waits for it.
	torndownCh chan struct{}

	curQueryLogsCtx *manQueryLogsCtx

	curLogs manLogsCtx

	defaultTransportMode *TransportMode
}

type LStreamsManagerParams struct {
	// ConfigLogStreams contains nerdlog-specific config, typically coming from
	// ~/.config/nerdlog/logstreams.yaml.
	ConfigLogStreams ConfigLogStreams

	// SSHConfig contains the general ssh config, typically coming from
	// ~/.ssh/config.
	SSHConfig *ssh_config.Config

	// SSHKeys specifies paths to ssh keys to try, in the given order, until
	// an existing key is found.
	SSHKeys []string

	Logger *log.Logger

	InitialLStreams string

	InitialDefaultTransportMode *TransportMode

	// ClientID is just an arbitrary string (should be filename-friendly though)
	// which will be appended to the nerdlog_agent.sh and its index filenames.
	//
	// Needed to make sure that different clients won't get conflicts over those
	// files when using the tool concurrently on the same nodes.
	ClientID string

	UpdatesCh chan<- LStreamsManagerUpdate

	Clock clock.Clock
}

// 初始化 Log stream 管理器
func NewLStreamsManager(params LStreamsManagerParams) *LStreamsManager {
	// 检查时钟
	if params.Clock == nil {
		// For details on why not default to the real clock:
		// https://dmitryfrank.com/articles/mocking_time_in_go#caveat_with_defaulting_to_real_clock
		panic("Clock is nil")
	}

	// 配置LSMan命名空间的日志
	params.Logger = params.Logger.WithNamespaceAppended("LSMan")

	// 初始化管理器
	lsman := &LStreamsManager{
		params: params,

		lscs:               map[string]*LStreamClient{},
		lscStates:          map[string]LStreamClientState{},
		lscConnDetails:     map[string]ConnDetails{},
		lscBusyStages:      map[string]BusyStage{},
		lscPendingTeardown: map[string]int{},

		lstreamUpdatesCh: make(chan *LStreamClientUpdate, 1024),
		// 请求channel，接收从该client发送的查询请求
		reqCh: make(chan lstreamsManagerReq, 8),
		// 响应channel
		respCh: make(chan lstreamCmdRes),

		teardownReqCh: make(chan struct{}, 1),
		torndownCh:    make(chan struct{}, 1),

		defaultTransportMode: params.InitialDefaultTransportMode,
	}

	// 设置初始的 logstreams
	if err := lsman.setLStreams(params.InitialLStreams); err != nil {
		panic("setLStreams didn't like the initial logStreamsSpec: " + err.Error())
	}

	// 更新 HA 列表，本质上是更新 logstream clients 列表和状态
	lsman.updateHAs()
	// 统计各个状态的 logstream client 列表，并统计未连接的 logstream client 数量
	lsman.updateLStreamsByState()
	// 发送状态更新
	lsman.sendStateUpdate()
	// 启动管理器的运行，主要是处理channel中的请求
	go lsman.run()

	return lsman
}

func (lsman *LStreamsManager) SetDefaultTransportMode(defaultTransportMode *TransportMode) {
	resCh := make(chan struct{}, 1)

	lsman.reqCh <- lstreamsManagerReq{
		setDefaultTransportMode: &lstreamsManagerReqSetDefaultTransportMode{
			defaultTransportMode: defaultTransportMode,
			resCh:                resCh,
		},
	}

	<-resCh
}

func (lsman *LStreamsManager) setDefaultTransportMode(defaultTransportMode *TransportMode) {
	// If unchanged, then do nothing.
	if lsman.defaultTransportMode == defaultTransportMode {
		return
	}

	// Transport mode has changed: remember it, and reconnect using it.

	lsman.defaultTransportMode = defaultTransportMode

	lstreamsStr := lsman.lstreamsStr
	lsman.setLStreams("")
	lsman.updateHAs()
	lsman.updateLStreamsByState()

	lsman.setLStreams(lstreamsStr)
	lsman.updateHAs()
	lsman.updateLStreamsByState()

	lsman.sendStateUpdate()
}

// LocalShellCommand is used when the host is "localhost".
const LocalShellCommand = "/bin/sh"

// 将 --lstreams 更新到 LStreamsManager 中
func (lsman *LStreamsManager) setLStreams(lstreamsStr string) error {
	// 获取当前用户
	u, err := user.Current()
	if err != nil {
		return errors.Annotatef(err, "getting current OS user")
	}

	// 使用当前用户、传输模式、当前lstreams、SSH 配置解析
	resolver := NewLStreamsResolver(LStreamsResolverParams{
		CurOSUser: u.Username,

		DefaultTransportMode: lsman.defaultTransportMode,

		ConfigLogStreams: lsman.params.ConfigLogStreams,
		SSHConfig:        lsman.params.SSHConfig,
	})

	// 开始解析
	parsedLogStreams, err := resolver.Resolve(lstreamsStr)
	if err != nil {
		return errors.Trace(err)
	}

	// All went well, remember the logstreams spec
	lsman.lstreamsStr = lstreamsStr
	lsman.parsedLogStreams = parsedLogStreams

	return nil
}

func (lsman *LStreamsManager) updateHAs() {
	// Close unused logstream clients
	// 关闭不再使用的 logstream clients
	for key, oldHA := range lsman.lscs {
		// 检查该 logstream 是否还在使用，如果在使用，则跳过
		if _, ok := lsman.parsedLogStreams[key]; ok {
			// The logstream is still used
			continue
		}

		// We used to use this logstream, but now it's filtered out, so close it
		lsman.params.Logger.Verbose1f("Closing LSClient %s", key)
		// 删除相关的客户端信息
		delete(lsman.lscs, key)
		// 删除相关的状态信息
		delete(lsman.lscStates, key)
		// 删除连接详情
		delete(lsman.lscConnDetails, key)
		// 删除繁忙阶段信息
		delete(lsman.lscBusyStages, key)

		// 标识该 logstream client 正在关闭
		keyNew := fmt.Sprintf("OLD_%s_%s", lsman.randomString(4), key)
		// 正在关闭的 logstream client 数量加 1
		lsman.lscPendingTeardown[keyNew] += 1
		// 发送关闭请求到 channel
		oldHA.Close(keyNew)
	}

	// Create new logstream clients
	// 创建新的 logstream clients
	for key, ls := range lsman.parsedLogStreams {
		// 检查该 logstream client 是否已经存在，如果存在，则跳过
		if _, ok := lsman.lscs[key]; ok {
			// This logstream client already exists
			continue
		}

		// We need to create a new logstream client
		// 需要创建一个新的 logstream client
		lsc := NewLStreamClient(LStreamClientParams{
			LogStream: ls,
			SSHKeys:   lsman.params.SSHKeys,
			Logger:    lsman.params.Logger,
			ClientID:  lsman.params.ClientID, //fmt.Sprintf("%s-%d", lsman.params.ClientID, rand.Int()),
			UpdatesCh: lsman.lstreamUpdatesCh,
			Clock:     lsman.params.Clock,
		})
		// 保存 logstream client 信息
		lsman.lscs[key] = lsc
		// 初始化 logstream client 状态为未连接
		lsman.lscStates[key] = LStreamClientStateDisconnected
	}
}

func (lsman *LStreamsManager) run() {
	// 初始化各个状态的 logstream client 列表
	lsclientsByState := map[LStreamClientState]map[string]struct{}{}
	for name := range lsman.lscs {
		// 初始状态为未连接
		lsclientsByState[LStreamClientStateDisconnected] = map[string]struct{}{
			name: {},
		}
	}

	for {
		select {
		// 处理 logstream client 的更新请求
		case upd := <-lsman.lstreamUpdatesCh:
			if upd.State != nil { // 处理状态更新
				// 仅处理已知的 logstream client 的状态更新
				if _, ok := lsman.lscStates[upd.Name]; ok {
					lsman.params.Logger.Verbose1f(
						"Got state update from %s: %s -> %s",
						upd.Name, upd.State.OldState, upd.State.NewState,
					)

					// 更新为新状态
					lsman.lscStates[upd.Name] = upd.State.NewState

					// Maintain lsman.lscConnDetails
					// 如果新状态为已连接闲置或已连接繁忙，则标识为已连接
					if upd.State.NewState == LStreamClientStateConnectedIdle ||
						upd.State.NewState == LStreamClientStateConnectedBusy {
						cd := lsman.lscConnDetails[upd.Name]
						cd.Connected = true
						lsman.lscConnDetails[upd.Name] = cd
					}

					// Maintain lsman.lscBusyStages
					// 如果新状态非已连接繁忙，则删除繁忙阶段信息
					if upd.State.NewState != LStreamClientStateConnectedBusy {
						delete(lsman.lscBusyStages, upd.Name)
					}
				} else if _, ok := lsman.lscPendingTeardown[upd.Name]; ok { // 处理正在关闭的 logstream client 的状态更新
					lsman.params.Logger.Verbose1f(
						"Got state update from tearing-down %s: %s -> %s",
						upd.Name, upd.State.OldState, upd.State.NewState,
					)
				} else { // 处理未知的 logstream client 的状态更新
					lsman.params.Logger.Warnf(
						"Got state update from unknown %s: %s -> %s",
						upd.Name, upd.State.OldState, upd.State.NewState,
					)
				}

				// 统计各个状态的 logstream client 列表，并统计未连接的 logstream client 数量
				lsman.updateLStreamsByState()
				// 发送状态更新
				lsman.sendStateUpdate()
			} else if upd.ConnDetails != nil { // 处理连接详情更新
				lsman.params.Logger.Verbose1f("ConnDetails for %s: %+v", upd.Name, *upd.ConnDetails)
				lsman.lscConnDetails[upd.Name] = *upd.ConnDetails // 保存连接详情
				lsman.sendStateUpdate()                           // 发送状态更新
			} else if upd.BootstrapDetails != nil { // 处理引导详情更新
				lsman.params.Logger.Verbose1f("BootstrapDetails for %s: %+v", upd.Name, *upd.BootstrapDetails)

				// 发送引导问题更新
				upd := LStreamsManagerUpdate{
					BootstrapIssue: &BootstrapIssue{
						LStreamName: upd.Name,
						Err:         upd.BootstrapDetails.Err,

						WarnJournalctlNoAdminAccess: upd.BootstrapDetails.WarnJournalctlNoAdminAccess,
					},
				}
				lsman.params.UpdatesCh <- upd
			} else if upd.BusyStage != nil { // 处理繁忙阶段更新
				lsman.lscBusyStages[upd.Name] = *upd.BusyStage
				lsman.sendStateUpdate() // 发送状态更新
			} else if upd.DataRequest != nil { // 处理数据请求更新
				// 发送数据请求更新
				lsman.params.UpdatesCh <- LStreamsManagerUpdate{
					DataRequest: upd.DataRequest,
				}
			} else if upd.TornDown { // 处理关闭完成更新
				// One of our LStreamClient-s has just shut down, account for it properly.
				// 一台 LStreamClient 刚刚关闭，正确地计算它。
				lsman.lscPendingTeardown[upd.Name] -= 1

				// Sanity check.
				// 健全性检查
				if lsman.lscPendingTeardown[upd.Name] < 0 {
					panic(fmt.Sprintf("got TornDown update and lscPendingTeardown[%s] becomes %d", upd.Name, lsman.lscPendingTeardown[upd.Name]))
				}

				// Check how many LStreamClient-s are still in the process of teardown,
				// and if needed, finish the teardown of the whole LStreamsManager.
				// 检查有多少 LStreamClient 正在关闭过程中，如果需要，完成整个 LStreamsManager 的关闭。
				numPending := lsman.getNumLStreamClientsTearingDown()
				// 如果还有正在关闭的 logstream client，则打印日志
				if numPending != 0 {
					pendingSB := strings.Builder{}
					i := 0
					for k, v := range lsman.lscPendingTeardown {
						if v == 0 {
							continue
						}

						i++
						if i > 3 {
							pendingSB.WriteString("...")
							break
						}

						if pendingSB.Len() > 0 {
							pendingSB.WriteString(", ")
						}

						pendingSB.WriteString(k)
					}

					lsman.params.Logger.Verbose1f(
						"LStreamClient %s teardown is completed, %d more are still pending: %s",
						upd.Name, numPending, pendingSB.String(),
					)
				} else { // 没有正在关闭的 logstream client，则打印日志
					lsman.params.Logger.Verbose1f("LStreamClient %s teardown is completed, no more pending teardowns", upd.Name)

					// If the whole LStreamsManager was shutting down, we're done now.
					if lsman.tearingDown { // 如果整个 LStreamsManager 正在关闭，则现在完成
						lsman.params.Logger.Infof("LStreamsManager teardown is completed")
						close(lsman.torndownCh)
						return
					}
				}

				// 发送状态更新
				lsman.sendStateUpdate()
			}

		// 综合处理所有请求的入口
		case req := <-lsman.reqCh:
			switch {
			case req.queryLogs != nil:
				if len(lsman.lscs) == 0 {
					lsman.sendLogRespUpdate(&LogRespTotal{
						Errs: []error{errors.Errorf("no matching lstreams to get logs from")},
					})
					continue
				}

				if lsman.numNotConnected > 0 {
					lsman.sendLogRespUpdate(&LogRespTotal{
						Errs: []error{ErrNotYetConnected},
					})
					continue
				}

				if lsman.curQueryLogsCtx != nil {
					lsman.sendLogRespUpdate(&LogRespTotal{
						Errs: []error{ErrBusyWithAnotherQuery},
					})
					continue
				}

				if req.queryLogs.MaxNumLines == 0 {
					panic("req.queryLogs.MaxNumLines is zero")
				}

				lsman.curQueryLogsCtx = &manQueryLogsCtx{
					req:       req.queryLogs,
					startTime: lsman.params.Clock.Now(),
					resps:     make(map[string]*LogResp, len(lsman.lscs)),
					errs:      map[string]error{},
				}

				// sendStateUpdate must be done after setting curQueryLogsCtx.
				lsman.sendStateUpdate()

				for lstreamName, lsc := range lsman.lscs {
					cmdQueryLogs := lstreamCmdQueryLogs{
						maxNumLines: req.queryLogs.MaxNumLines,

						from:  req.queryLogs.From,
						to:    req.queryLogs.To,
						query: req.queryLogs.Query,

						refreshIndex: req.queryLogs.RefreshIndex,
					}

					if req.queryLogs.LoadEarlier {
						// TODO: right now, this loadEarlier case isn't optimized at all:
						// we again query the whole timerange, and every node goes through
						// all same lines and builds all the same mstats again (which we
						// then ignore). We can optimize it; however honestly the actual
						// performance, as per my experiments, isn't going to be
						// SPECTACULARLY better. Just kinda marginally better (try loading
						// older logs with time period 5h or 1m: the 1m is somewhat faster,
						// but not super fast. That's the difference we're talking about)
						//
						// Anyway, the way to optimize it is as follows: we already have
						// mstats, so we know what kind of timeframe we should query to get
						// the next maxNumLines messages. So we should query only this time
						// range, and we should avoid building any mstats. This way, no
						// matter how large the current time period is, loading more
						// messages will be as fast as possible.

						if nodeCtx, ok := lsman.curLogs.perNode[lstreamName]; ok {
							if len(nodeCtx.logs) > 0 {
								if nodeCtx.logs[0].LogFilename == SpecialFilenameJournalctl {
									cmdQueryLogs.timestampUntil = getEarliestTimeAndNumMsgs(nodeCtx.logs)
								} else {
									cmdQueryLogs.linesUntil = nodeCtx.logs[0].CombinedLinenumber
								}
							}
						}
					}

					lsc.EnqueueCmd(lstreamCmd{
						respCh:    lsman.respCh,
						queryLogs: &cmdQueryLogs,
					})
				}

			// 更新 lstreams 的请求
			case req.updLStreams != nil:
				// 获取 --lstreams
				r := req.updLStreams
				lsman.params.Logger.Infof("LStreams manager: update logstreams spec: %s", r.logStreamsSpec)

				// 存在该值，代表当前有查询正在进行，此时不允许更新 lstreams,必须要等待查询结束后，才能更新
				if lsman.curQueryLogsCtx != nil {
					r.resCh <- ErrBusyWithAnotherQuery
					continue
				}

				// 设置
				if err := lsman.setLStreams(r.logStreamsSpec); err != nil {
					r.resCh <- errors.Trace(err)
					continue
				}

				lsman.updateHAs()
				lsman.updateLStreamsByState()
				lsman.sendStateUpdate()

				r.resCh <- nil

			case req.setDefaultTransportMode != nil:
				r := req.setDefaultTransportMode
				lsman.params.Logger.Infof("LStreams manager: setting defaultTransportMode: %s", r.defaultTransportMode.String())

				lsman.setDefaultTransportMode(r.defaultTransportMode)

				r.resCh <- struct{}{}

			// ping 请求
			case req.ping:
				for _, lsc := range lsman.lscs {
					// ping 入队
					lsc.EnqueueCmd(lstreamCmd{
						ping: &lstreamCmdPing{},
					})
				}

			case req.reconnect:
				lsman.params.Logger.Infof("Reconnect command")
				if lsman.curQueryLogsCtx != nil {
					lsman.params.Logger.Infof("Forgetting the in-progress query")
					lsman.curQueryLogsCtx = nil
				}
				for _, lsc := range lsman.lscs {
					lsc.Reconnect()
				}

				// NOTE: we don't call updateHAs, updateLStreamsByState and sendStateUpdate
				// here, because it would operate on outdated info: after we've called
				// Reconnect for every LStreamClient just above, their statuses are changing
				// already, but we don't know it yet (we'll know once we receive updates
				// in this same event loop, and _then_ we'll update all the data etc).
			// 断开连接请求
			case req.disconnect:
				// 打印日志
				lsman.params.Logger.Infof("Disconnect command")
				//
				if lsman.curQueryLogsCtx != nil {
					lsman.params.Logger.Infof("Forgetting the in-progress query")
					lsman.curQueryLogsCtx = nil
				}
				// 置空
				lsman.setLStreams("")

				lsman.updateHAs()
				lsman.updateLStreamsByState()
				lsman.sendStateUpdate()
			}

		// 接收请求的响应
		case resp := <-lsman.respCh:
			// 写入 verbose1 日志
			lsman.params.Logger.Verbose1f("Got a response from %v: %+v", resp.hostname, resp)

			switch {
			// 处理查询日志的响应
			case lsman.curQueryLogsCtx != nil:
				// 存在报错，则写入日志并记录
				if resp.err != nil {
					lsman.params.Logger.Errorf("Got an error response from %v: %s", resp.hostname, resp.err)
					lsman.curQueryLogsCtx.errs[resp.hostname] = resp.err
				}

				// 根据响应类型进行处理
				switch v := resp.resp.(type) {
				// 日志响应
				case *LogResp:
					// 保存响应
					lsman.curQueryLogsCtx.resps[resp.hostname] = v

					// If we collected responses from all nodes, handle them.
					// 如果已经从所有的节点获取了信息，则开始处理
					if len(lsman.curQueryLogsCtx.resps) == len(lsman.lscs) {
						// 写入 verbose1 日志
						lsman.params.Logger.Verbose1f(
							"Got logs from %v, this was the last one, query is completed",
							resp.hostname,
						)

						// 合并日志响应，并发送到 UI
						lsman.mergeLogRespsAndSend()

						lsman.curQueryLogsCtx = nil

						// sendStateUpdate must be done after setting curQueryLogsCtx.
						lsman.sendStateUpdate()
					} else {
						lsman.params.Logger.Verbose1f(
							"Got logs from %v, %d more to go",
							resp.hostname,
							len(lsman.lscs)-len(lsman.curQueryLogsCtx.resps),
						)
					}

				default:
					panic(fmt.Sprintf("unexpected resp type %T", v))
				}

			default:
				lsman.params.Logger.Errorf("Dropping update from %s on the floor", resp.hostname)
			}

		// 处理关闭请求
		case <-lsman.teardownReqCh:
			lsman.params.Logger.Infof("LStreamsManager teardown is started")
			// 标识正在关闭
			lsman.tearingDown = true
			// 置空 lstreams
			lsman.setLStreams("")
			// 更新 HA 列表，本质上是更新 logstream clients 列表和状态
			lsman.updateHAs()
			// 统计各个状态的 logstream client 列表，并统计未连接的 logstream client 数量
			lsman.updateLStreamsByState()

			// Check if we don't need to wait for anything, and can teardown right away.
			// 检查是否不需要等待任何东西，可以立即关闭
			numPending := lsman.getNumLStreamClientsTearingDown()
			// 如果没有正在关闭的 logstream client，则直接关闭
			if numPending == 0 {
				lsman.params.Logger.Infof("LStreamsManager teardown is completed")
				close(lsman.torndownCh)
				return
			}

			// We still need to wait for some LStreamClient-s to teardown, so send an
			// update for now and keep going.
			// 发送状态更新
			lsman.sendStateUpdate()
		}
	}
}

type timeAndNumMsgs struct {
	// time is the timestamp of some log message.
	time time.Time
	// numMsgs is the number of messages on the timestamp time.
	numMsgs int
}

func getEarliestTimeAndNumMsgs(logs []LogMsg) *timeAndNumMsgs {
	if len(logs) == 0 {
		return nil
	}

	ret := &timeAndNumMsgs{
		time:    logs[0].Time,
		numMsgs: 1,
	}

	for _, logMsg := range logs[1:] {
		if !logMsg.Time.Equal(ret.time) {
			break
		}

		ret.numMsgs++
	}

	return ret
}

func (lsman *LStreamsManager) getNumLStreamClientsTearingDown() int {
	numPending := 0
	for _, v := range lsman.lscPendingTeardown {
		numPending += v
	}

	return numPending
}

// Close initiates the shutdown. It doesn't wait for the shutdown to complete;
// use Wait for it.
// 
func (lsman *LStreamsManager) Close() {
	select {
	case lsman.teardownReqCh <- struct{}{}:
	default:
	}
}

// Wait waits for the LStreamsManager to tear down. Typically used after calling Close().
func (lsman *LStreamsManager) Wait() {
	<-lsman.torndownCh
}

type lstreamsManagerReq struct {
	// Exactly one field must be non-nil

	queryLogs               *QueryLogsParams
	updLStreams             *lstreamsManagerReqUpdLStreams
	setDefaultTransportMode *lstreamsManagerReqSetDefaultTransportMode
	ping                    bool
	reconnect               bool
	disconnect              bool
}

type lstreamsManagerReqUpdLStreams struct {
	logStreamsSpec string
	resCh          chan<- error
}

type lstreamsManagerReqSetDefaultTransportMode struct {
	defaultTransportMode *TransportMode
	resCh                chan<- struct{}
}

func (lsman *LStreamsManager) QueryLogs(params QueryLogsParams) {
	// 以 verbose1 级别写入到日志中
	lsman.params.Logger.Verbose1f("QueryLogs: %+v", params)
	// 写入到 req channel 中
	lsman.reqCh <- lstreamsManagerReq{
		queryLogs: &params,
	}
}

// 将 --lstreams 解析，并更新到 LStreamsManager
func (lsman *LStreamsManager) SetLStreams(logStreamsSpec string) error {
	// 构造一个允许保存单个错误的chan
	resCh := make(chan error, 1)

	// 将要解析的内容和解析错误，封装为请求，投递到 reqCh channel
	lsman.reqCh <- lstreamsManagerReq{
		// 这是一个 update lstreams
		updLStreams: &lstreamsManagerReqUpdLStreams{
			logStreamsSpec: logStreamsSpec,
			resCh:          resCh,
		},
	}

	// 等待 resCh 的返回，本质上是等待解析结束，出现错误或nil，将其投递到 resCh
	return <-resCh
}

func (lsman *LStreamsManager) Ping() {
	lsman.reqCh <- lstreamsManagerReq{
		ping: true,
	}
}

func (lsman *LStreamsManager) Reconnect() {
	// 发送重新连接请求
	lsman.reqCh <- lstreamsManagerReq{
		reconnect: true,
	}
}

func (lsman *LStreamsManager) Disconnect() {
	// 发送断开连接请求
	lsman.reqCh <- lstreamsManagerReq{
		disconnect: true,
	}
}

// queryLogs的上下文
type manQueryLogsCtx struct {
	req *QueryLogsParams

	startTime time.Time

	// resps is a map from logstream name to its response. Once all responses have
	// been collected, we'll start merging them together.
	resps map[string]*LogResp
	errs  map[string]error
}

type manLogsCtx struct {
	minuteStats  map[int64]MinuteStatsItem
	numMsgsTotal int

	perNode map[string]*manLogsNodeCtx
}

type manLogsNodeCtx struct {
	logs          []LogMsg
	isMaxNumLines bool
}

type LStreamsManagerUpdate struct {
	// Exactly one of the fields below must be non-nil

	State   *LStreamsManagerState
	LogResp *LogRespTotal

	BootstrapIssue *BootstrapIssue

	DataRequest *ShellConnDataRequest
}

type LStreamsManagerState struct {
	NumLStreams int

	LStreamsByState map[LStreamClientState]map[string]struct{}

	// NumConnected is how many nodes are actually connected
	NumConnected int

	// NoMatchingLStreams is true when there are no matching lstreams.
	NoMatchingLStreams bool

	// Connected is true when all matching lstreams (which should be more than 0)
	// are connected.
	Connected bool

	// Busy is true when a query is in progress.
	Busy bool

	ConnDetailsByLStream map[string]ConnDetails
	BusyStageByLStream   map[string]BusyStage

	// TearingDown contains logstream names whic are in the process of teardown.
	TearingDown []string
}

type BootstrapIssue struct {
	LStreamName string
	Err         string

	// WarnJournalctlNoAdminAccess is set to true if journalctl is used and the
	// user doesn't have access to all the system logs. It's a separate bool
	// instead of a generic warning message to make it possible to suppress it
	// with a flag.
	WarnJournalctlNoAdminAccess bool
}

// 统计各个状态的 logstream client 列表，并统计未连接的 logstream client 数量
func (lsman *LStreamsManager) updateLStreamsByState() {
	// 统计未连接的 logstream client 数量，初始化为 0
	lsman.numNotConnected = 0
	// 统计各个状态的 logstream client 列表
	lsman.lstreamsByState = map[LStreamClientState]map[string]struct{}{}

	// 遍历所有的 logstream client 状态
	for name, state := range lsman.lscStates {
		// 获取该状态对应的 logstream client 列表
		set, ok := lsman.lstreamsByState[state]
		// 如果不存在，则创建一个新的列表
		if !ok {
			set = map[string]struct{}{}
			lsman.lstreamsByState[state] = set
		}

		// 将该 logstream name 添加到对应状态的列表中
		set[name] = struct{}{}

		// 如果该状态是未连接状态，则未连接数量加 1
		if !isStateConnected(state) {
			lsman.numNotConnected++
		}
	}
}

// 复制当前状态，并发送到 channel
func (lsman *LStreamsManager) sendStateUpdate() {
	// 已连接数量为 0
	numConnected := 0
	for _, state := range lsman.lscStates {
		// 如果该状态是已连接状态，则已连接数量加 1
		if isStateConnected(state) {
			numConnected++
		}
	}

	// 复制连接详情
	connDetailsCopy := make(map[string]ConnDetails, len(lsman.lscConnDetails))
	// 遍历连接详情，进行复制
	for k, v := range lsman.lscConnDetails {
		connDetailsCopy[k] = v
	}

	// 复制繁忙阶段信息
	busyStagesCopy := make(map[string]BusyStage, len(lsman.lscBusyStages))
	// 遍历繁忙阶段信息，进行复制
	for k, v := range lsman.lscBusyStages {
		busyStagesCopy[k] = v
	}

	// 正在关闭的 logstream 列表
	tearingDown := make([]string, 0, len(lsman.lscPendingTeardown))
	// 遍历正在关闭的 logstream 列表，进行复制
	for k, num := range lsman.lscPendingTeardown {
		for i := 0; i < num; i++ {
			tearingDown = append(tearingDown, k)
		}
	}
	// 按字典序排序
	sort.Strings(tearingDown)

	// 构造更新内容
	upd := LStreamsManagerUpdate{
		State: &LStreamsManagerState{
			NumLStreams:          len(lsman.lscs),
			LStreamsByState:      lsman.lstreamsByState,
			NumConnected:         numConnected,
			NoMatchingLStreams:   lsman.numNotConnected == 0 && numConnected == 0,
			Connected:            lsman.numNotConnected == 0 && numConnected > 0,
			Busy:                 lsman.curQueryLogsCtx != nil,
			ConnDetailsByLStream: connDetailsCopy,
			BusyStageByLStream:   busyStagesCopy,
			TearingDown:          tearingDown,
		},
	}

	// 发送更新内容到 channel
	lsman.params.UpdatesCh <- upd
}

func (lsman *LStreamsManager) sendLogRespUpdate(resp *LogRespTotal) {
	if lsman.curQueryLogsCtx != nil {
		resp.QueryDur = time.Since(lsman.curQueryLogsCtx.startTime)
	}

	lsman.params.UpdatesCh <- LStreamsManagerUpdate{
		LogResp: resp,
	}
}

func (lsman *LStreamsManager) mergeLogRespsAndSend() {
	// 获取所有的响应
	resps := lsman.curQueryLogsCtx.resps
	// 获取所有的错误
	errs := lsman.curQueryLogsCtx.errs

	// 处理错误，
	if len(errs) != 0 {
		errs2 := make([]error, 0, len(errs))
		for hostname, err := range errs {
			errs2 = append(errs2, errors.Annotatef(err, "%s", hostname))
		}

		sort.Slice(errs2, func(i, j int) bool {
			return errs2[i].Error() < errs2[j].Error()
		})

		// 发送响应
		lsman.sendLogRespUpdate(&LogRespTotal{
			Errs: errs2,
		})

		return
	}

	// If we're not adding to already existing logs, reset w/e we've had already,
	// and calculate minuteStats from the resps.
	if !lsman.curQueryLogsCtx.req.LoadEarlier {
		lsman.curLogs = manLogsCtx{
			minuteStats: map[int64]MinuteStatsItem{},
			perNode:     map[string]*manLogsNodeCtx{},
		}

		for nodeName, resp := range resps {
			for k, v := range resp.MinuteStats {
				lsman.curLogs.minuteStats[k] = MinuteStatsItem{
					NumMsgs: lsman.curLogs.minuteStats[k].NumMsgs + v.NumMsgs,
				}

				lsman.curLogs.numMsgsTotal += v.NumMsgs
			}

			lsman.curLogs.perNode[nodeName] = &manLogsNodeCtx{
				logs:          resp.Logs,
				isMaxNumLines: len(resp.Logs) == lsman.curQueryLogsCtx.req.MaxNumLines,
			}
		}
	} else {
		// Add to existing logs
		for nodeName, resp := range resps {
			pn := lsman.curLogs.perNode[nodeName]
			pn.logs = append(resp.Logs, pn.logs...)
			pn.isMaxNumLines = len(resp.Logs) == lsman.curQueryLogsCtx.req.MaxNumLines
		}
	}

	// Collect debug info
	debugInfo := make(map[string]LogstreamDebugInfo, len(resps))
	for lstreamName, resp := range resps {
		debugInfo[lstreamName] = resp.DebugInfo
	}

	ret := &LogRespTotal{
		MinuteStats:   lsman.curLogs.minuteStats,
		NumMsgsTotal:  lsman.curLogs.numMsgsTotal,
		LoadedEarlier: lsman.curQueryLogsCtx.req.LoadEarlier,
		DebugInfo:     debugInfo,
	}

	var logsCoveredSince time.Time

	for _, pn := range lsman.curLogs.perNode {
		ret.Logs = append(ret.Logs, pn.logs...)

		// If the timespan covered by logs from this logstream is shorter than what
		// we've seen before, remember it.
		if pn.isMaxNumLines && logsCoveredSince.Before(pn.logs[0].Time) {
			logsCoveredSince = pn.logs[0].Time
		}
	}

	sort.SliceStable(ret.Logs, func(i, j int) bool {
		if !ret.Logs[i].Time.Equal(ret.Logs[j].Time) {
			return ret.Logs[i].Time.Before(ret.Logs[j].Time)
		}

		// TODO: make it less hacky, store lstream somewhere outside of Context as well.
		return ret.Logs[i].Context["lstream"] < ret.Logs[j].Context["lstream"]
	})

	// Cut all potentially incomplete logs, only leave timespan that we're sure
	// we have covered from all nodes
	coveredSinceIdx := sort.Search(len(ret.Logs), func(i int) bool {
		return !ret.Logs[i].Time.Before(logsCoveredSince)
	})
	ret.Logs = ret.Logs[coveredSinceIdx:]

	lsman.sendLogRespUpdate(ret)
}

func (lsman *LStreamsManager) randomString(length int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"

	rand.Seed(lsman.params.Clock.Now().UnixNano()) // Seed once per call
	prefix := make([]byte, length)
	for i := range prefix {
		prefix[i] = charset[rand.Intn(len(charset))]
	}
	return string(prefix)
}
