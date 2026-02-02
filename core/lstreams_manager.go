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
	// 原始的 --lstreams 字符串
	lstreamsStr string
	// 从 --lstreams 解析后的 logstreams 列表
	parsedLogStreams map[string]LogStream
	// 基于已解析的 log streams 列表（parsedLogStreams）而创建 log stream client
	lscs map[string]*LStreamClient
	// log stream 客户端状态列表
	lscStates map[string]LStreamClientState
	// lscConnDetails contains items for all selected lstreams, even after the
	// connection is done (which is indicated by ConnDetails.Connected being
	// true).
	// log stream 的连接时信息
	lscConnDetails map[string]ConnDetails
	// lscBusyStages only contains items for lstreams which are in the
	// LStreamClientStateConnectedBusy state.
	// log stream 的繁忙状态
	lscBusyStages map[string]BusyStage

	// lscPendingTeardown contains info about LStreamClient-s that are being torn
	// down. NOTE that when a LStreamClient starts tearing down, its key changes
	// (gets prepended with OLD_XXXX_), so re remove an item from the `has` map
	// with one key, and add an item here with a different key.
	// 包含正在关闭的 LStreamClient 信息，注意当 LStreamClient 开始关闭时，
	// 它的 key 会改变（前面会加上 OLD_XXXX_），所以要用不同的 key 从 `has` map 中删除一个项，并在这里添加一个项。
	lscPendingTeardown map[string]int

	lstreamsByState map[LStreamClientState]map[string]struct{}

	// 未连接的数量
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

	// 上游接收信息
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

	// 此处主要是根据 parsedLogStreams 来创建log stream client 连接
	lsman.updateHAs()
	// 因为此时并没有log stream client的连接状态还保存在channel中，并未消费，
	// 因此此时只是用于初始化 lstreamsByState 和 numNotConnected
	lsman.updateLStreamsByState()
	// 此时将状态通知到上游，还上一个类似，此时并没有log stream client的连接状态还保存在channel中，并未消费，
	// 因此此时只是将之前的状态投送给上游
	lsman.sendStateUpdate()
	// goroutines 异步启动
	go lsman.run()

	return lsman
}

// 处理页面设置
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

// 更新 log stream client 的 transport mode
func (lsman *LStreamsManager) setDefaultTransportMode(defaultTransportMode *TransportMode) {
	// If unchanged, then do nothing.
	if lsman.defaultTransportMode == defaultTransportMode {
		return
	}

	// Transport mode has changed: remember it, and reconnect using it.

	lsman.defaultTransportMode = defaultTransportMode

	lstreamsStr := lsman.lstreamsStr
	// 先清理所有的连接
	lsman.setLStreams("")
	lsman.updateHAs()
	lsman.updateLStreamsByState()

	// 重建所有的连接
	lsman.setLStreams(lstreamsStr)
	lsman.updateHAs()
	lsman.updateLStreamsByState()

	// 将最新的状态发送给上游
	lsman.sendStateUpdate()
}

// LocalShellCommand is used when the host is "localhost".
const LocalShellCommand = "/bin/sh"

// 解析，将 --lstreams 更新到 LStreamsManager 中
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
	// 保留原始字符串
	lsman.lstreamsStr = lstreamsStr
	// 保存解析后的结果
	lsman.parsedLogStreams = parsedLogStreams

	return nil
}

// parsedLogStreams 作为基准来更新 lscs，如果已经不在 parsedLogStreams 中存在，则移除；
// 反之，检查其是否已经连接，未连接则开始连接
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
	// 基于解析后的 --lstreams 创建 log stream clients
	for key, ls := range lsman.parsedLogStreams {
		// 检查该 logstream client 是否已经存在，如果存在，则跳过
		if _, ok := lsman.lscs[key]; ok {
			// This logstream client already exists
			continue
		}

		// We need to create a new logstream client
		// 需要创建一个新的 logstream client，并在后台开始连接
		lsc := NewLStreamClient(LStreamClientParams{
			LogStream: ls,
			SSHKeys:   lsman.params.SSHKeys,
			Logger:    lsman.params.Logger,
			ClientID:  lsman.params.ClientID,  //fmt.Sprintf("%s-%d", lsman.params.ClientID, rand.Int()),
			UpdatesCh: lsman.lstreamUpdatesCh, // 接收log stream client 状态变更
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

	// 针对通道进行处理
	for {
		select {
		// 处理来自log stream client 的状态更新通知
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
					// 如果是已连接状态，则更新该log stream的连接=true
					if upd.State.NewState == LStreamClientStateConnectedIdle ||
						upd.State.NewState == LStreamClientStateConnectedBusy {
						cd := lsman.lscConnDetails[upd.Name]
						cd.Connected = true
						lsman.lscConnDetails[upd.Name] = cd
					}

					// Maintain lsman.lscBusyStages
					// 非繁忙状态，则更新繁忙中的log stream
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
				// 发送状态更新到上游
				lsman.sendStateUpdate()
			} else if upd.ConnDetails != nil { // 处理连接详情更新，主要是 Connecting 中的连接信息
				lsman.params.Logger.Verbose1f("ConnDetails for %s: %+v", upd.Name, *upd.ConnDetails)
				lsman.lscConnDetails[upd.Name] = *upd.ConnDetails // 保存最新连接详情
				lsman.sendStateUpdate()                           // 发送状态更新到上游
			} else if upd.BootstrapDetails != nil { // 处理引导详情更新，主要是 Connecting => ConnectedIdle 时触发的 bootstrap 命令
				lsman.params.Logger.Verbose1f("BootstrapDetails for %s: %+v", upd.Name, *upd.BootstrapDetails)

				// 将引导信息发送给上游
				upd := LStreamsManagerUpdate{
					BootstrapIssue: &BootstrapIssue{
						LStreamName: upd.Name,
						Err:         upd.BootstrapDetails.Err,

						WarnJournalctlNoAdminAccess: upd.BootstrapDetails.WarnJournalctlNoAdminAccess,
					},
				}

				// 投递到上游的通知channel中
				lsman.params.UpdatesCh <- upd
			} else if upd.BusyStage != nil { // 处理繁忙阶段更新，主要是 ConnectedBusy 处理的进展
				lsman.lscBusyStages[upd.Name] = *upd.BusyStage // 保存最新的处理进展
				lsman.sendStateUpdate()                        // 发送状态更新到上游
			} else if upd.DataRequest != nil { // 处理数据请求更新，比如底层 log stream 连接时需要输入额外的信息时，会发送到页面上进行展示
				// 发送数据请求更新到上游
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

		// 处理所有来自页面的请求操作
		case req := <-lsman.reqCh:
			switch {
			// 处理查询日志请求
			case req.queryLogs != nil:
				// 当前没有可用的 log stream client，无法处理
				if len(lsman.lscs) == 0 {
					lsman.sendLogRespUpdate(&LogRespTotal{
						Errs: []error{errors.Errorf("no matching lstreams to get logs from")},
					})
					continue
				}

				// 当前尚未连接好，无法处理
				if lsman.numNotConnected > 0 {
					lsman.sendLogRespUpdate(&LogRespTotal{
						Errs: []error{ErrNotYetConnected},
					})
					continue
				}

				// 当前正在查询，无法处理
				if lsman.curQueryLogsCtx != nil {
					lsman.sendLogRespUpdate(&LogRespTotal{
						Errs: []error{ErrBusyWithAnotherQuery},
					})
					continue
				}

				// 允许最大行的参数配置错误，无法处理
				if req.queryLogs.MaxNumLines == 0 {
					panic("req.queryLogs.MaxNumLines is zero")
				}

				//
				lsman.curQueryLogsCtx = &manQueryLogsCtx{
					req:       req.queryLogs,
					startTime: lsman.params.Clock.Now(),
					resps:     make(map[string]*LogResp, len(lsman.lscs)), // 存放各个log stream client响应结果
					errs:      map[string]error{},                         // 存放各个log stream client错误
				}

				// sendStateUpdate must be done after setting curQueryLogsCtx.
				// 将当前的log stream client信息发送给上游
				lsman.sendStateUpdate()

				for lstreamName, lsc := range lsman.lscs {
					// 构建查询请求，核心参数：--max-num-lines, --from, --to, --query, --refresh-index
					cmdQueryLogs := lstreamCmdQueryLogs{
						maxNumLines: req.queryLogs.MaxNumLines, // 读取最大行数

						from:  req.queryLogs.From,  // 起始行数
						to:    req.queryLogs.To,    // 结束行数
						query: req.queryLogs.Query, // awk 查询

						refreshIndex: req.queryLogs.RefreshIndex, // 是否刷新索引
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

					// 将请求发送到 log stream client
					lsc.EnqueueCmd(lstreamCmd{
						respCh:    lsman.respCh,  // 接收响应的通道
						queryLogs: &cmdQueryLogs, // 请求内容
					})
				}

			// 处理更新 lstreams 的请求
			case req.updLStreams != nil:
				// 获取 --lstreams
				r := req.updLStreams
				lsman.params.Logger.Infof("LStreams manager: update logstreams spec: %s", r.logStreamsSpec)

				// 如果当前有查询正在进行，则不允许更新
				if lsman.curQueryLogsCtx != nil {
					r.resCh <- ErrBusyWithAnotherQuery
					continue
				}

				// 更新 lstreamStr 和 parsedLogStreams
				if err := lsman.setLStreams(r.logStreamsSpec); err != nil {
					r.resCh <- errors.Trace(err)
					continue
				}

				// 重建 log stream client，去除不存在的log stream
				lsman.updateHAs()
				// 更新 log stream client 状态，此时可以同步更新
				lsman.updateLStreamsByState()
				// 将最新的 log stream client 发送给上游
				lsman.sendStateUpdate()

				//
				r.resCh <- nil

			// 处理更新 transport mode 的请求
			case req.setDefaultTransportMode != nil:
				r := req.setDefaultTransportMode
				lsman.params.Logger.Infof("LStreams manager: setting defaultTransportMode: %s", r.defaultTransportMode.String())

				// 关闭所有连接，并重新连接，然后将结果发送给上游
				lsman.setDefaultTransportMode(r.defaultTransportMode)

				r.resCh <- struct{}{}

			// 处理 ping 请求
			case req.ping:
				for _, lsc := range lsman.lscs {
					// ping 入队
					lsc.EnqueueCmd(lstreamCmd{
						ping: &lstreamCmdPing{},
					})
				}

			// 处理 reconnect 请求
			case req.reconnect:
				lsman.params.Logger.Infof("Reconnect command")
				// 如果当前有查询正在进行，则清空并强制关闭
				if lsman.curQueryLogsCtx != nil {
					lsman.params.Logger.Infof("Forgetting the in-progress query")
					lsman.curQueryLogsCtx = nil
				}

				// 重连，仅在通道未满的情况下才会执行
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
				// 清除当前查询上下文
				if lsman.curQueryLogsCtx != nil {
					lsman.params.Logger.Infof("Forgetting the in-progress query")
					lsman.curQueryLogsCtx = nil
				}
				// 将 lstreamStr 和 parsedLogStreamStr 置为空
				lsman.setLStreams("")

				// 关闭使用中的连接，并清除
				lsman.updateHAs()
				// 同步更新状态
				lsman.updateLStreamsByState()
				// 将状态信息送给上游
				lsman.sendStateUpdate()
			}

		// 接收请求的响应，仅处理 querylogs 的响应
		case resp := <-lsman.respCh:
			// 写入 verbose1 日志
			lsman.params.Logger.Verbose1f("Got a response from %v: %+v", resp.hostname, resp)

			switch {
			// 处理查询日志的响应，由 reqCh#queryLogs 发起
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
					// 如果已经从所有的节点获取了信息，则需要合并消息，并发送给前端
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
						// 将状态同步给上游
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
			// 置空 lstreamStr 和 parsedLogStreams
			lsman.setLStreams("")
			// 更新，因为上一个设置了 ""，因此此处将清除所有的 lscs，以及关闭所有的client
			lsman.updateHAs()
			// 更新状态
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

// 从最早的消息中提取其数量
func getEarliestTimeAndNumMsgs(logs []LogMsg) *timeAndNumMsgs {
	if len(logs) == 0 {
		return nil
	}

	ret := &timeAndNumMsgs{
		time:    logs[0].Time,
		numMsgs: 1,
	}

	// 比较时间一致的日期，统计其数量
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
	// 用于页面发起的 query logs 请求
	queryLogs *QueryLogsParams
	// 用于页面修改数据后，发起更新的请求
	updLStreams *lstreamsManagerReqUpdLStreams
	// 用于页面切换transport mode的请求
	setDefaultTransportMode *lstreamsManagerReqSetDefaultTransportMode
	// 用于页面发起的 ping 请求
	ping bool
	// 用于页面发起的 reconnect 请求
	reconnect bool
	// 用于页面发起的 disconnect 请求
	disconnect bool
}

type lstreamsManagerReqUpdLStreams struct {
	logStreamsSpec string
	resCh          chan<- error
}

type lstreamsManagerReqSetDefaultTransportMode struct {
	defaultTransportMode *TransportMode
	resCh                chan<- struct{}
}

// 处理页面发起日志查询的请求
func (lsman *LStreamsManager) QueryLogs(params QueryLogsParams) {
	// 以 verbose1 级别写入到日志中
	lsman.params.Logger.Verbose1f("QueryLogs: %+v", params)
	// 写入到 req channel 中
	lsman.reqCh <- lstreamsManagerReq{
		queryLogs: &params,
	}
}

// 处理页面发起的 --lstreams 更新
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

// 处理页面发起的 Ping 请求
func (lsman *LStreamsManager) Ping() {
	lsman.reqCh <- lstreamsManagerReq{
		ping: true,
	}
}

// 处理页面发起的 Reconnect 请求
func (lsman *LStreamsManager) Reconnect() {
	// 发送重新连接请求
	lsman.reqCh <- lstreamsManagerReq{
		reconnect: true,
	}
}

// 处理页面发起的 Disconnect 请求
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

	// 发送更新内容到 上游
	lsman.params.UpdatesCh <- upd
}

func (lsman *LStreamsManager) sendLogRespUpdate(resp *LogRespTotal) {
	// 如果当前查询尚未结束，则计算查询间隔
	if lsman.curQueryLogsCtx != nil {
		resp.QueryDur = time.Since(lsman.curQueryLogsCtx.startTime)
	}

	// 通知上游
	lsman.params.UpdatesCh <- LStreamsManagerUpdate{
		LogResp: resp,
	}
}

// 合并日期并发送到
func (lsman *LStreamsManager) mergeLogRespsAndSend() {
	// 获取所有的响应
	resps := lsman.curQueryLogsCtx.resps
	// 获取所有的错误
	errs := lsman.curQueryLogsCtx.errs

	// 合并错误
	if len(errs) != 0 {
		errs2 := make([]error, 0, len(errs))
		// 追加错误
		for hostname, err := range errs {
			errs2 = append(errs2, errors.Annotatef(err, "%s", hostname))
		}

		// 按照hostname+err进行升序排序
		sort.Slice(errs2, func(i, j int) bool {
			return errs2[i].Error() < errs2[j].Error()
		})

		// 发送响应给上游
		lsman.sendLogRespUpdate(&LogRespTotal{
			Errs: errs2,
		})

		return
	}

	// If we're not adding to already existing logs, reset w/e we've had already,
	// and calculate minuteStats from the resps.
	if !lsman.curQueryLogsCtx.req.LoadEarlier {
		// 构造保存日志的映射
		lsman.curLogs = manLogsCtx{
			minuteStats: map[int64]MinuteStatsItem{},
			perNode:     map[string]*manLogsNodeCtx{},
		}

		// 遍历日志及其节点
		for nodeName, resp := range resps {
			// 获取分钟统计
			for k, v := range resp.MinuteStats {
				// 统计日志条数
				lsman.curLogs.minuteStats[k] = MinuteStatsItem{
					NumMsgs: lsman.curLogs.minuteStats[k].NumMsgs + v.NumMsgs,
				}

				// 累加总条数
				lsman.curLogs.numMsgsTotal += v.NumMsgs
				写入日志
			}

			// 保存日志
			lsman.curLogs.perNode[nodeName] = &manLogsNodeCtx{
				logs:          resp.Logs,
				isMaxNumLines: len(resp.Logs) == lsman.curQueryLogsCtx.req.MaxNumLines, // 检查其是否超过最大行数
			}
		}
	} else {
		// Add to existing logs
		// 因为此场景是为了获取更早的日志，因此需要保留已有的日志，此时需要将日志进行合并
		for nodeName, resp := range resps {
			// 获取节点
			pn := lsman.curLogs.perNode[nodeName]
			// 将已有日志追加到之后
			pn.logs = append(resp.Logs, pn.logs...)
			// 检查是否超过最大行数
			pn.isMaxNumLines = len(resp.Logs) == lsman.curQueryLogsCtx.req.MaxNumLines
		}
	}

	// Collect debug info
	// 收集响应中的 debug 信息
	debugInfo := make(map[string]LogstreamDebugInfo, len(resps))
	for lstreamName, resp := range resps {
		debugInfo[lstreamName] = resp.DebugInfo
	}

	// 构造结果
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

	// 将更新发送到上游
	lsman.sendLogRespUpdate(ret)
}

// 生成指定长度的随机字符串
func (lsman *LStreamsManager) randomString(length int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"

	rand.Seed(lsman.params.Clock.Now().UnixNano()) // Seed once per call
	prefix := make([]byte, length)
	for i := range prefix {
		prefix[i] = charset[rand.Intn(len(charset))]
	}
	return string(prefix)
}
