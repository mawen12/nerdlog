package v2

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// LStreamClientV2 基于Actor模型的日志流客户端V2版本
type LStreamClientV2 struct {
	// Actor系统
	actorSystem *ActorSystem

	// 主Actor引用
	mainActor *ActorRef

	// 事件总线
	eventBus *EventBus

	// 状态机
	stateMachine *StateMachine

	// 配置参数
	params LStreamClientParamsV2

	// 命令解释器
	interpreter *DSLInterpreter

	// 上下文和取消函数
	ctx    context.Context
	cancel context.CancelFunc

	// 互斥锁
	mu sync.RWMutex

	// 连接信息
	connection *ConnectionInfo

	// 日志函数
	logFunc func(format string, args ...interface{})
}

// LStreamClientParamsV2 V2版本的参数
type LStreamClientParamsV2 struct {
	// 日志流配置
	LogStream LogStreamConfig

	// SSH密钥路径
	SSHKeys []string

	// 客户端ID
	ClientID string

	// 更新通道
	UpdatesCh chan<- *LStreamClientUpdateV2

	// 日志记录器（可选，用于调试）
	LoggerFunc func(format string, args ...interface{})

	// 邮箱大小
	MailboxSize int

	// 事件缓冲区大小
	EventBufferSize int

	// 命令队列大小
	CommandQueueSize int
}

// LogStreamConfig 日志流配置
type LogStreamConfig struct {
	Name     string
	Hostname string
	Username string
	Port     int
}

// LStreamClientUpdateV2 客户端更新消息
type LStreamClientUpdateV2 struct {
	Name             string
	StateChange      *StateChangeUpdate
	Event            Event
	Command          *CommandResult
	TornDown         bool
}

// StateChangeUpdate 状态变更更新
type StateChangeUpdate struct {
	OldState string
	NewState string
	Reason   string
}

// ConnectionInfo 连接信息
type ConnectionInfo struct {
	RemoteHost   string
	Port         int
	Username     string
	Connected    bool
	ConnectedAt  time.Time
	Timezone     string
	Location     *time.Location
	ExampleLines []string
}

// NewLStreamClientV2 创建新的V2客户端
func NewLStreamClientV2(params LStreamClientParamsV2) (*LStreamClientV2, error) {
	if params.MailboxSize <= 0 {
		params.MailboxSize = 100
	}
	if params.EventBufferSize <= 0 {
		params.EventBufferSize = 1000
	}
	if params.CommandQueueSize <= 0 {
		params.CommandQueueSize = 100
	}

	ctx, cancel := context.WithCancel(context.Background())

	// 创建事件总线
	eventBus := NewEventBus(params.EventBufferSize)

	// 创建Actor系统
	actorSystem := NewActorSystem(fmt.Sprintf("lstream-client-%s", params.ClientID))

	// 创建命令解释器
	interpreter := NewDSLInterpreter()

	client := &LStreamClientV2{
		actorSystem: actorSystem,
		eventBus:    eventBus,
		params:      params,
		interpreter: interpreter,
		ctx:         ctx,
		cancel:      cancel,
		logger:      params.Logger,
	}

	// 创建并启动主Actor
	mainActor := NewLStreamActor(client)
	ref, err := actorSystem.Spawn(
		fmt.Sprintf("main-actor-%s", params.ClientID),
		mainActor,
		params.MailboxSize,
		eventBus,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn main actor: %w", err)
	}

	client.mainActor = ref

	// 启动事件总线
	eventBus.Start()

	// 注册事件处理器
	client.registerEventHandlers()

	// 初始化状态机
	if err := client.initializeStateMachine(); err != nil {
		return nil, fmt.Errorf("failed to initialize state machine: %w", err)
	}

	return client, nil
}

// registerEventHandlers 注册事件处理器
func (lsc *LStreamClientV2) registerEventHandlers() {
	// 注册状态变更事件处理器
	lsc.eventBus.Register(EventTypeStateChanged, &StateChangeHandler{client: lsc})

	// 注册连接事件处理器
	lsc.eventBus.Register(EventTypeConnectSucceeded, &ConnectionHandler{client: lsc})
	lsc.eventBus.Register(EventTypeConnectFailed, &ConnectionHandler{client: lsc})

	// 注册命令事件处理器
	lsc.eventBus.Register(EventTypeCommandCompleted, &CommandHandler{client: lsc})
	lsc.eventBus.Register(EventTypeCommandFailed, &CommandHandler{client: lsc})

	// 注册日志行事件处理器
	lsc.eventBus.Register(EventTypeLogLineReceived, &LogLineHandler{client: lsc})
}

// initializeStateMachine 初始化状态机
func (lsc *LStreamClientV2) initializeStateMachine() error {
	// 创建状态上下文
	stateCtx := NewStateContext(lsc.eventBus, lsc.mainActor)

	// 创建状态机
	sm := NewStateMachine(stateCtx)

	// 创建所有状态
	disconnectedState := NewDisconnectedState()
	connectingState := NewConnectingState()
	connectedIdleState := NewConnectedIdleState()
	connectedBusyState := NewConnectedBusyState()
	disconnectingState := NewDisconnectingState()

	// 注册状态
	sm.RegisterState(disconnectedState)
	sm.RegisterState(connectingState)
	sm.RegisterState(connectedIdleState)
	sm.RegisterState(connectedBusyState)
	sm.RegisterState(disconnectingState)

	// 配置状态转换
	// Disconnected -> Connecting
	disconnectedState.RegisterTransition(EventTypeConnectRequested, connectingState)

	// Connecting -> ConnectedIdle (成功)
	connectingState.RegisterTransition(EventTypeConnectSucceeded, connectedIdleState)

	// Connecting -> Disconnected (失败)
	connectingState.RegisterTransition(EventTypeConnectFailed, disconnectedState)
	connectingState.RegisterTransition(EventTypeDisconnectRequested, disconnectingState)

	// ConnectedIdle -> ConnectedBusy
	connectedIdleState.RegisterTransition(EventTypeCommandEnqueued, connectedBusyState)
	connectedIdleState.RegisterTransition(EventTypeDisconnectRequested, disconnectingState)

	// ConnectedBusy -> ConnectedIdle
	connectedBusyState.RegisterTransition(EventTypeCommandCompleted, connectedIdleState)
	connectedBusyState.RegisterTransition(EventTypeDisconnectRequested, disconnectingState)

	// Disconnecting -> Disconnected
	disconnectingState.RegisterTransition(EventTypeDisconnected, disconnectedState)

	// 设置初始状态
	if err := sm.SetInitialState("disconnected"); err != nil {
		return err
	}

	lsc.stateMachine = sm

	// 将状态机设置到Actor上下文
	if ra, ok := lsc.actorSystem.actors[lsc.mainActor.ID()]; ok {
		ra.ctx.SetStateMachine(sm)
	}

	return nil
}

// Connect 连接到远程主机
func (lsc *LStreamClientV2) Connect() error {
	// 创建连接命令
	cmd := NewConnectCommand(
		fmt.Sprintf("connect-%d", time.Now().UnixNano()),
		lsc.params.LogStream.Hostname,
		22, // 默认SSH端口
		lsc.params.LogStream.Username,
		lsc.params.SSHKeys,
	)

	// 验证命令
	if err := cmd.Validate(); err != nil {
		return fmt.Errorf("invalid connect command: %w", err)
	}

	// 发送命令消息给Actor
	msg := NewCommandMessage(cmd)
	if err := lsc.mainActor.Tell(msg); err != nil {
		return fmt.Errorf("failed to send connect command: %w", err)
	}

	// 发布连接请求事件
	lsc.eventBus.Publish(NewConnectEvent(
		EventTypeConnectRequested,
		lsc.params.LogStream.Hostname,
		nil,
	))

	return nil
}

// Disconnect 断开连接
func (lsc *LStreamClientV2) Disconnect() error {
	cmd := NewDisconnectCommand(
		fmt.Sprintf("disconnect-%d", time.Now().UnixNano()),
		false,
	)

	msg := NewCommandMessage(cmd)
	if err := lsc.mainActor.Tell(msg); err != nil {
		return fmt.Errorf("failed to send disconnect command: %w", err)
	}

	// 发布断开连接请求事件
	lsc.eventBus.Publish(NewBaseEvent(EventTypeDisconnectRequested))

	return nil
}

// QueryLogs 查询日志
func (lsc *LStreamClientV2) QueryLogs(query string, from, to time.Time, limit int, responseCh chan<- string) error {
	cmd := NewQueryLogsCommand(
		fmt.Sprintf("query-%d", time.Now().UnixNano()),
		query,
		from,
		to,
		limit,
		responseCh,
	)

	if err := cmd.Validate(); err != nil {
		return fmt.Errorf("invalid query command: %w", err)
	}

	msg := NewCommandMessage(cmd)
	if err := lsc.mainActor.Tell(msg); err != nil {
		return fmt.Errorf("failed to send query command: %w", err)
	}

	// 发布命令入队事件
	lsc.eventBus.Publish(NewCommandEvent(
		EventTypeCommandEnqueued,
		cmd.ID(),
		cmd.Type(),
	))

	return nil
}

// Close 关闭客户端
func (lsc *LStreamClientV2) Close() error {
	// 发送关闭消息
	msg := NewMessage(MsgTypeShutdown, nil)
	if err := lsc.mainActor.Tell(msg); err != nil {
		lsc.logger.Printf("Failed to send shutdown message: %v", err)
	}

	// 停止事件总线
	lsc.eventBus.Stop()

	// 关闭Actor系统
	if err := lsc.actorSystem.Shutdown(10 * time.Second); err != nil {
		return fmt.Errorf("failed to shutdown actor system: %w", err)
	}

	// 取消上下文
	lsc.cancel()

	// 发送最终更新
	if lsc.params.UpdatesCh != nil {
		lsc.params.UpdatesCh <- &LStreamClientUpdateV2{
			Name:     lsc.params.ClientID,
			TornDown: true,
		}
	}

	return nil
}

// GetState 获取当前状态
func (lsc *LStreamClientV2) GetState() string {
	if lsc.stateMachine == nil || lsc.stateMachine.CurrentState() == nil {
		return "unknown"
	}
	return lsc.stateMachine.CurrentState().Name()
}

// sendUpdate 发送更新到更新通道
func (lsc *LStreamClientV2) sendUpdate(update *LStreamClientUpdateV2) {
	if lsc.params.UpdatesCh != nil {
		select {
		case lsc.params.UpdatesCh <- update:
		case <-time.After(1 * time.Second):
			lsc.logger.Printf("Failed to send update: timeout")
		}
	}
}

// 事件处理器实现

// StateChangeHandler 状态变更事件处理器
type StateChangeHandler struct {
	client *LStreamClientV2
}

func (sch *StateChangeHandler) CanHandle(eventType EventType) bool {
	return eventType == EventTypeStateChanged
}

func (sch *StateChangeHandler) HandleEvent(event Event) error {
	stateChangeEvent, ok := event.(*StateChangeEvent)
	if !ok {
		return ErrInvalidMessage
	}

	update := &LStreamClientUpdateV2{
		Name: sch.client.params.ClientID,
		StateChange: &StateChangeUpdate{
			OldState: stateChangeEvent.FromState.Name(),
			NewState: stateChangeEvent.ToState.Name(),
			Reason:   stateChangeEvent.Reason,
		},
	}

	sch.client.sendUpdate(update)
	return nil
}

// ConnectionHandler 连接事件处理器
type ConnectionHandler struct {
	client *LStreamClientV2
}

func (ch *ConnectionHandler) CanHandle(eventType EventType) bool {
	return eventType == EventTypeConnectSucceeded || eventType == EventTypeConnectFailed
}

func (ch *ConnectionHandler) HandleEvent(event Event) error {
	connectEvent, ok := event.(*ConnectEvent)
	if !ok {
		return ErrInvalidMessage
	}

	if event.Type() == EventTypeConnectSucceeded {
		ch.client.mu.Lock()
		if ch.client.connection == nil {
			ch.client.connection = &ConnectionInfo{}
		}
		ch.client.connection.Connected = true
		ch.client.connection.ConnectedAt = time.Now()
		ch.client.connection.RemoteHost = connectEvent.RemoteHost
		ch.client.mu.Unlock()
	}

	update := &LStreamClientUpdateV2{
		Name:  ch.client.params.ClientID,
		Event: event,
	}

	ch.client.sendUpdate(update)
	return nil
}

// CommandHandler 命令事件处理器
type CommandHandler struct {
	client *LStreamClientV2
}

func (cmdh *CommandHandler) CanHandle(eventType EventType) bool {
	return eventType == EventTypeCommandCompleted || eventType == EventTypeCommandFailed
}

func (cmdh *CommandHandler) HandleEvent(event Event) error {
	commandEvent, ok := event.(*CommandEvent)
	if !ok {
		return ErrInvalidMessage
	}

	result := &CommandResult{
		CommandID: commandEvent.CommandID,
		Success:   event.Type() == EventTypeCommandCompleted,
		Data:      commandEvent.Result,
		Error:     commandEvent.Error,
	}

	update := &LStreamClientUpdateV2{
		Name:    cmdh.client.params.ClientID,
		Command: result,
	}

	cmdh.client.sendUpdate(update)
	return nil
}

// LogLineHandler 日志行事件处理器
type LogLineHandler struct {
	client *LStreamClientV2
}

func (llh *LogLineHandler) CanHandle(eventType EventType) bool {
	return eventType == EventTypeLogLineReceived
}

func (llh *LogLineHandler) HandleEvent(event Event) error {
	// 处理日志行，可以进行过滤、解析等
	// 这里只是简单示例
	return nil
}
