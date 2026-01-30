package v2

// State 表示系统状态
type State interface {
	// Name 返回状态名称
	Name() string
	// Enter 进入状态时的操作
	Enter(ctx *StateContext) error
	// Exit 退出状态时的操作
	Exit(ctx *StateContext) error
	// Handle 处理在该状态下的事件
	Handle(ctx *StateContext, event Event) (State, error)
}

// StateContext 状态上下文，包含状态机需要的所有数据
type StateContext struct {
	// 当前状态
	currentState State

	// 状态数据
	data map[string]interface{}

	// 事件总线
	eventBus *EventBus

	// Actor引用（用于发送消息）
	actor *Actor
}

func NewStateContext(eventBus *EventBus, actor *Actor) *StateContext {
	return &StateContext{
		data:     make(map[string]interface{}),
		eventBus: eventBus,
		actor:    actor,
	}
}

func (sc *StateContext) GetData(key string) (interface{}, bool) {
	val, ok := sc.data[key]
	return val, ok
}

func (sc *StateContext) SetData(key string, value interface{}) {
	sc.data[key] = value
}

func (sc *StateContext) CurrentState() State {
	return sc.currentState
}

// StateMachine 状态机
type StateMachine struct {
	context      *StateContext
	currentState State
	states       map[string]State
}

func NewStateMachine(ctx *StateContext) *StateMachine {
	return &StateMachine{
		context: ctx,
		states:  make(map[string]State),
	}
}

// RegisterState 注册状态
func (sm *StateMachine) RegisterState(state State) {
	sm.states[state.Name()] = state
}

// SetInitialState 设置初始状态
func (sm *StateMachine) SetInitialState(stateName string) error {
	state, ok := sm.states[stateName]
	if !ok {
		return ErrStateNotFound
	}

	sm.currentState = state
	sm.context.currentState = state
	return state.Enter(sm.context)
}

// HandleEvent 处理事件并可能触发状态转换
func (sm *StateMachine) HandleEvent(event Event) error {
	if sm.currentState == nil {
		return ErrNoCurrentState
	}

	// 让当前状态处理事件
	nextState, err := sm.currentState.Handle(sm.context, event)
	if err != nil {
		return err
	}

	// 如果返回的下一个状态不为空且与当前状态不同，则执行状态转换
	if nextState != nil && nextState.Name() != sm.currentState.Name() {
		return sm.TransitionTo(nextState)
	}

	return nil
}

// TransitionTo 转换到新状态
func (sm *StateMachine) TransitionTo(newState State) error {
	oldState := sm.currentState

	// 退出旧状态
	if err := oldState.Exit(sm.context); err != nil {
		return err
	}

	// 进入新状态
	if err := newState.Enter(sm.context); err != nil {
		return err
	}

	sm.currentState = newState
	sm.context.currentState = newState

	// 发布状态变更事件
	stateChangeEvent := NewStateChangeEvent(
		oldState,
		newState,
		"State transition",
	)
	sm.context.eventBus.Publish(stateChangeEvent)

	return nil
}

// CurrentState 返回当前状态
func (sm *StateMachine) CurrentState() State {
	return sm.currentState
}

// 具体状态实现

// BaseState 提供状态的基础实现
type BaseState struct {
	name string
}

func NewBaseState(name string) *BaseState {
	return &BaseState{name: name}
}

func (bs *BaseState) Name() string {
	return bs.name
}

func (bs *BaseState) Enter(ctx *StateContext) error {
	// 默认实现：什么都不做
	return nil
}

func (bs *BaseState) Exit(ctx *StateContext) error {
	// 默认实现：什么都不做
	return nil
}

func (bs *BaseState) Handle(ctx *StateContext, event Event) (State, error) {
	// 默认实现：保持当前状态
	return nil, nil
}

// DisconnectedState 断开连接状态
type DisconnectedState struct {
	*BaseState
	nextStates map[EventType]State
}

func NewDisconnectedState() *DisconnectedState {
	return &DisconnectedState{
		BaseState:  NewBaseState("disconnected"),
		nextStates: make(map[EventType]State),
	}
}

func (ds *DisconnectedState) RegisterTransition(eventType EventType, nextState State) {
	ds.nextStates[eventType] = nextState
}

func (ds *DisconnectedState) Enter(ctx *StateContext) error {
	// 进入断开连接状态时的清理工作
	ctx.SetData("connection", nil)
	ctx.SetData("numConnAttempts", 0)
	return nil
}

func (ds *DisconnectedState) Handle(ctx *StateContext, event Event) (State, error) {
	// 处理连接请求
	if event.Type() == EventTypeConnectRequested {
		if nextState, ok := ds.nextStates[EventTypeConnectRequested]; ok {
			return nextState, nil
		}
	}
	return nil, nil
}

// ConnectingState 连接中状态
type ConnectingState struct {
	*BaseState
	nextStates map[EventType]State
}

func NewConnectingState() *ConnectingState {
	return &ConnectingState{
		BaseState:  NewBaseState("connecting"),
		nextStates: make(map[EventType]State),
	}
}

func (cs *ConnectingState) RegisterTransition(eventType EventType, nextState State) {
	cs.nextStates[eventType] = nextState
}

func (cs *ConnectingState) Enter(ctx *StateContext) error {
	// 增加连接尝试次数
	attempts, _ := ctx.GetData("numConnAttempts")
	if attempts == nil {
		attempts = 0
	}
	ctx.SetData("numConnAttempts", attempts.(int)+1)

	// 发布连接开始事件
	ctx.eventBus.Publish(NewBaseEvent(EventTypeConnectStarted))
	return nil
}

func (cs *ConnectingState) Handle(ctx *StateContext, event Event) (State, error) {
	switch event.Type() {
	case EventTypeConnectSucceeded:
		if nextState, ok := cs.nextStates[EventTypeConnectSucceeded]; ok {
			// 连接成功，重置尝试次数
			ctx.SetData("numConnAttempts", 0)
			return nextState, nil
		}
	case EventTypeConnectFailed:
		if nextState, ok := cs.nextStates[EventTypeConnectFailed]; ok {
			return nextState, nil
		}
	case EventTypeDisconnectRequested:
		if nextState, ok := cs.nextStates[EventTypeDisconnectRequested]; ok {
			return nextState, nil
		}
	}
	return nil, nil
}

// ConnectedIdleState 已连接空闲状态
type ConnectedIdleState struct {
	*BaseState
	nextStates map[EventType]State
}

func NewConnectedIdleState() *ConnectedIdleState {
	return &ConnectedIdleState{
		BaseState:  NewBaseState("connected_idle"),
		nextStates: make(map[EventType]State),
	}
}

func (cis *ConnectedIdleState) RegisterTransition(eventType EventType, nextState State) {
	cis.nextStates[eventType] = nextState
}

func (cis *ConnectedIdleState) Enter(ctx *StateContext) error {
	// 进入空闲状态，准备接收命令
	return nil
}

func (cis *ConnectedIdleState) Handle(ctx *StateContext, event Event) (State, error) {
	switch event.Type() {
	case EventTypeCommandEnqueued:
		if nextState, ok := cis.nextStates[EventTypeCommandEnqueued]; ok {
			return nextState, nil
		}
	case EventTypeDisconnectRequested, EventTypeDisconnected:
		if nextState, ok := cis.nextStates[EventTypeDisconnectRequested]; ok {
			return nextState, nil
		}
	}
	return nil, nil
}

// ConnectedBusyState 已连接繁忙状态
type ConnectedBusyState struct {
	*BaseState
	nextStates map[EventType]State
}

func NewConnectedBusyState() *ConnectedBusyState {
	return &ConnectedBusyState{
		BaseState:  NewBaseState("connected_busy"),
		nextStates: make(map[EventType]State),
	}
}

func (cbs *ConnectedBusyState) RegisterTransition(eventType EventType, nextState State) {
	cbs.nextStates[eventType] = nextState
}

func (cbs *ConnectedBusyState) Enter(ctx *StateContext) error {
	// 进入繁忙状态，开始执行命令
	return nil
}

func (cbs *ConnectedBusyState) Handle(ctx *StateContext, event Event) (State, error) {
	switch event.Type() {
	case EventTypeCommandCompleted, EventTypeCommandFailed:
		// 命令完成，检查是否还有待处理的命令
		if nextState, ok := cbs.nextStates[EventTypeCommandCompleted]; ok {
			return nextState, nil
		}
	case EventTypeDisconnectRequested, EventTypeDisconnected:
		if nextState, ok := cbs.nextStates[EventTypeDisconnectRequested]; ok {
			return nextState, nil
		}
	}
	return nil, nil
}

// DisconnectingState 断开连接中状态
type DisconnectingState struct {
	*BaseState
	nextStates map[EventType]State
}

func NewDisconnectingState() *DisconnectingState {
	return &DisconnectingState{
		BaseState:  NewBaseState("disconnecting"),
		nextStates: make(map[EventType]State),
	}
}

func (ds *DisconnectingState) RegisterTransition(eventType EventType, nextState State) {
	ds.nextStates[eventType] = nextState
}

func (ds *DisconnectingState) Enter(ctx *StateContext) error {
	// 进入断开连接状态，清理资源
	return nil
}

func (ds *DisconnectingState) Handle(ctx *StateContext, event Event) (State, error) {
	if event.Type() == EventTypeDisconnected {
		if nextState, ok := ds.nextStates[EventTypeDisconnected]; ok {
			return nextState, nil
		}
	}
	return nil, nil
}
