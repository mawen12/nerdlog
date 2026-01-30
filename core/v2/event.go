package v2

import (
	"time"
)

// Event 表示系统中的事件
type Event interface {
	// Type 返回事件类型
	Type() EventType
	// Timestamp 返回事件发生时间
	Timestamp() time.Time
	// Metadata 返回事件元数据
	Metadata() map[string]interface{}
}

// EventType 定义事件类型
type EventType string

const (
	// 连接相关事件
	EventTypeConnectRequested    EventType = "connect_requested"
	EventTypeConnectStarted      EventType = "connect_started"
	EventTypeConnectSucceeded    EventType = "connect_succeeded"
	EventTypeConnectFailed       EventType = "connect_failed"
	EventTypeDisconnectRequested EventType = "disconnect_requested"
	EventTypeDisconnected        EventType = "disconnected"

	// 命令相关事件
	EventTypeCommandEnqueued  EventType = "command_enqueued"
	EventTypeCommandStarted   EventType = "command_started"
	EventTypeCommandCompleted EventType = "command_completed"
	EventTypeCommandFailed    EventType = "command_failed"

	// 状态相关事件
	EventTypeStateChanged EventType = "state_changed"

	// 数据流事件
	EventTypeLogLineReceived EventType = "log_line_received"
	EventTypeDataReceived    EventType = "data_received"
	EventTypeDataRequest     EventType = "data_request"

	// Bootstrap相关事件
	EventTypeBootstrapStarted   EventType = "bootstrap_started"
	EventTypeBootstrapCompleted EventType = "bootstrap_completed"
	EventTypeBootstrapFailed    EventType = "bootstrap_failed"

	// 错误事件
	EventTypeError EventType = "error"
)

// BaseEvent 实现Event接口的基础结构
type BaseEvent struct {
	eventType EventType
	timestamp time.Time
	metadata  map[string]interface{}
}

func NewBaseEvent(eventType EventType) *BaseEvent {
	return &BaseEvent{
		eventType: eventType,
		timestamp: time.Now(),
		metadata:  make(map[string]interface{}),
	}
}

func (e *BaseEvent) Type() EventType {
	return e.eventType
}

func (e *BaseEvent) Timestamp() time.Time {
	return e.timestamp
}

func (e *BaseEvent) Metadata() map[string]interface{} {
	return e.metadata
}

func (e *BaseEvent) SetMetadata(key string, value interface{}) {
	e.metadata[key] = value
}

// 具体事件类型

// ConnectEvent 连接事件
type ConnectEvent struct {
	*BaseEvent
	RemoteHost string
	Error      error
}

func NewConnectEvent(eventType EventType, remoteHost string, err error) *ConnectEvent {
	return &ConnectEvent{
		BaseEvent:  NewBaseEvent(eventType),
		RemoteHost: remoteHost,
		Error:      err,
	}
}

// CommandEvent 命令事件
type CommandEvent struct {
	*BaseEvent
	CommandID   string
	CommandType string
	Error       error
	Result      interface{}
}

func NewCommandEvent(eventType EventType, cmdID, cmdType string) *CommandEvent {
	return &CommandEvent{
		BaseEvent:   NewBaseEvent(eventType),
		CommandID:   cmdID,
		CommandType: cmdType,
	}
}

// StateChangeEvent 状态变更事件
type StateChangeEvent struct {
	*BaseEvent
	FromState State
	ToState   State
	Reason    string
}

func NewStateChangeEvent(from, to State, reason string) *StateChangeEvent {
	return &StateChangeEvent{
		BaseEvent: NewBaseEvent(EventTypeStateChanged),
		FromState: from,
		ToState:   to,
		Reason:    reason,
	}
}

// LogLineEvent 日志行事件
type LogLineEvent struct {
	*BaseEvent
	Line     string
	IsStderr bool
}

func NewLogLineEvent(line string, isStderr bool) *LogLineEvent {
	return &LogLineEvent{
		BaseEvent: NewBaseEvent(EventTypeLogLineReceived),
		Line:      line,
		IsStderr:  isStderr,
	}
}

// DataRequestEvent 数据请求事件
type DataRequestEvent struct {
	*BaseEvent
	RequestID   string
	RequestType string
	Prompt      string
	ResponseCh  chan<- string
}

func NewDataRequestEvent(reqID, reqType, prompt string, respCh chan<- string) *DataRequestEvent {
	return &DataRequestEvent{
		BaseEvent:   NewBaseEvent(EventTypeDataRequest),
		RequestID:   reqID,
		RequestType: reqType,
		Prompt:      prompt,
		ResponseCh:  respCh,
	}
}

// ErrorEvent 错误事件
type ErrorEvent struct {
	*BaseEvent
	Error   error
	Context string
}

func NewErrorEvent(err error, context string) *ErrorEvent {
	return &ErrorEvent{
		BaseEvent: NewBaseEvent(EventTypeError),
		Error:     err,
		Context:   context,
	}
}

// EventHandler 事件处理器接口
type EventHandler interface {
	// HandleEvent 处理事件
	HandleEvent(event Event) error
	// CanHandle 判断是否能处理该类型的事件
	CanHandle(eventType EventType) bool
}

// EventBus 事件总线，负责事件的分发
type EventBus struct {
	handlers map[EventType][]EventHandler
	eventCh  chan Event
	stopCh   chan struct{}
}

func NewEventBus(bufferSize int) *EventBus {
	return &EventBus{
		handlers: make(map[EventType][]EventHandler),
		eventCh:  make(chan Event, bufferSize),
		stopCh:   make(chan struct{}),
	}
}

// Register 注册事件处理器
func (eb *EventBus) Register(eventType EventType, handler EventHandler) {
	eb.handlers[eventType] = append(eb.handlers[eventType], handler)
}

// Publish 发布事件（非阻塞）
func (eb *EventBus) Publish(event Event) {
	select {
	case eb.eventCh <- event:
	case <-eb.stopCh:
		// Event bus已停止
	default:
		// 缓冲区满，丢弃事件或记录警告
	}
}

// Start 启动事件总线
func (eb *EventBus) Start() {
	go eb.processEvents()
}

// Stop 停止事件总线
func (eb *EventBus) Stop() {
	close(eb.stopCh)
}

// processEvents 处理事件循环
func (eb *EventBus) processEvents() {
	for {
		select {
		case event := <-eb.eventCh:
			eb.dispatchEvent(event)
		case <-eb.stopCh:
			return
		}
	}
}

// dispatchEvent 分发事件给相应的处理器
func (eb *EventBus) dispatchEvent(event Event) {
	handlers := eb.handlers[event.Type()]
	for _, handler := range handlers {
		if handler.CanHandle(event.Type()) {
			// 在goroutine中执行，避免阻塞
			go func(h EventHandler, e Event) {
				_ = h.HandleEvent(e)
			}(handler, event)
		}
	}
}
