package v2

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Message Actor消息接口
type Message interface {
	// Type 返回消息类型
	Type() string
	// Payload 返回消息负载
	Payload() interface{}
	// Sender 返回发送者
	Sender() *ActorRef
	// ReplyTo 返回回复目标
	ReplyTo() chan<- Message
}

// BaseMessage 消息基础实现
type BaseMessage struct {
	msgType  string
	payload  interface{}
	sender   *ActorRef
	replyTo  chan<- Message
}

func NewMessage(msgType string, payload interface{}) *BaseMessage {
	return &BaseMessage{
		msgType: msgType,
		payload: payload,
	}
}

func (bm *BaseMessage) Type() string {
	return bm.msgType
}

func (bm *BaseMessage) Payload() interface{} {
	return bm.payload
}

func (bm *BaseMessage) Sender() *ActorRef {
	return bm.sender
}

func (bm *BaseMessage) ReplyTo() chan<- Message {
	return bm.replyTo
}

func (bm *BaseMessage) SetSender(sender *ActorRef) {
	bm.sender = sender
}

func (bm *BaseMessage) SetReplyTo(replyTo chan<- Message) {
	bm.replyTo = replyTo
}

// ActorRef Actor引用，用于向Actor发送消息
type ActorRef struct {
	id      string
	mailbox chan Message
}

func NewActorRef(id string, mailboxSize int) *ActorRef {
	return &ActorRef{
		id:      id,
		mailbox: make(chan Message, mailboxSize),
	}
}

func (ar *ActorRef) ID() string {
	return ar.id
}

// Tell 发送消息（fire-and-forget，不等待响应）
func (ar *ActorRef) Tell(msg Message) error {
	select {
	case ar.mailbox <- msg:
		return nil
	default:
		return ErrActorMailboxFull
	}
}

// TellWithTimeout 带超时的发送消息
func (ar *ActorRef) TellWithTimeout(msg Message, timeout time.Duration) error {
	select {
	case ar.mailbox <- msg:
		return nil
	case <-time.After(timeout):
		return ErrMessageTimeout
	}
}

// Ask 发送消息并等待响应（request-reply模式）
func (ar *ActorRef) Ask(msg Message, timeout time.Duration) (Message, error) {
	replyCh := make(chan Message, 1)
	msg.SetReplyTo(replyCh)

	if err := ar.TellWithTimeout(msg, timeout); err != nil {
		return nil, err
	}

	select {
	case reply := <-replyCh:
		return reply, nil
	case <-time.After(timeout):
		return nil, ErrMessageTimeout
	}
}

// Actor Actor接口
type Actor interface {
	// Receive 处理接收到的消息
	Receive(ctx *ActorContext, msg Message) error
	// PreStart 在Actor启动前调用
	PreStart(ctx *ActorContext) error
	// PostStop 在Actor停止后调用
	PostStop(ctx *ActorContext) error
}

// ActorContext Actor上下文
type ActorContext struct {
	// 自身引用
	self *ActorRef

	// 父Actor引用
	parent *ActorRef

	// 子Actor
	children map[string]*ActorRef

	// Actor系统
	system *ActorSystem

	// 状态机
	stateMachine *StateMachine

	// 事件总线
	eventBus *EventBus

	// 命令队列
	commandQueue *CommandQueue

	// 用户自定义数据
	data map[string]interface{}

	// 互斥锁
	mu sync.RWMutex
}

func NewActorContext(self *ActorRef, system *ActorSystem, eventBus *EventBus) *ActorContext {
	return &ActorContext{
		self:         self,
		system:       system,
		eventBus:     eventBus,
		commandQueue: NewCommandQueue(100),
		children:     make(map[string]*ActorRef),
		data:         make(map[string]interface{}),
	}
}

func (ac *ActorContext) Self() *ActorRef {
	return ac.self
}

func (ac *ActorContext) Parent() *ActorRef {
	return ac.parent
}

func (ac *ActorContext) SetParent(parent *ActorRef) {
	ac.parent = parent
}

func (ac *ActorContext) AddChild(child *ActorRef) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.children[child.ID()] = child
}

func (ac *ActorContext) RemoveChild(childID string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	delete(ac.children, childID)
}

func (ac *ActorContext) GetChild(childID string) (*ActorRef, bool) {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	child, ok := ac.children[childID]
	return child, ok
}

func (ac *ActorContext) SetData(key string, value interface{}) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.data[key] = value
}

func (ac *ActorContext) GetData(key string) (interface{}, bool) {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	val, ok := ac.data[key]
	return val, ok
}

func (ac *ActorContext) SetStateMachine(sm *StateMachine) {
	ac.stateMachine = sm
}

func (ac *ActorContext) GetStateMachine() *StateMachine {
	return ac.stateMachine
}

func (ac *ActorContext) EnqueueCommand(cmd Command) error {
	return ac.commandQueue.Enqueue(cmd)
}

func (ac *ActorContext) DequeueCommand() (Command, error) {
	return ac.commandQueue.Dequeue()
}

// BaseActor 提供Actor的基础实现
type BaseActor struct {
	name string
}

func NewBaseActor(name string) *BaseActor {
	return &BaseActor{name: name}
}

func (ba *BaseActor) Receive(ctx *ActorContext, msg Message) error {
	// 默认实现：记录未处理的消息
	fmt.Printf("Actor %s received unhandled message: %s\n", ba.name, msg.Type())
	return nil
}

func (ba *BaseActor) PreStart(ctx *ActorContext) error {
	// 默认实现：什么都不做
	return nil
}

func (ba *BaseActor) PostStop(ctx *ActorContext) error {
	// 默认实现：什么都不做
	return nil
}

// ActorSystem Actor系统
type ActorSystem struct {
	name   string
	actors map[string]*runningActor
	mu     sync.RWMutex
	stopCh chan struct{}
	wg     sync.WaitGroup
}

type runningActor struct {
	ref     *ActorRef
	actor   Actor
	ctx     *ActorContext
	stopCh  chan struct{}
	stopped bool
}

func NewActorSystem(name string) *ActorSystem {
	return &ActorSystem{
		name:   name,
		actors: make(map[string]*runningActor),
		stopCh: make(chan struct{}),
	}
}

// Spawn 创建并启动一个新的Actor
func (as *ActorSystem) Spawn(id string, actor Actor, mailboxSize int, eventBus *EventBus) (*ActorRef, error) {
	as.mu.Lock()
	defer as.mu.Unlock()

	if _, exists := as.actors[id]; exists {
		return nil, fmt.Errorf("actor with id %s already exists", id)
	}

	ref := NewActorRef(id, mailboxSize)
	ctx := NewActorContext(ref, as, eventBus)

	ra := &runningActor{
		ref:    ref,
		actor:  actor,
		ctx:    ctx,
		stopCh: make(chan struct{}),
	}

	as.actors[id] = ra

	// 调用PreStart
	if err := actor.PreStart(ctx); err != nil {
		delete(as.actors, id)
		return nil, fmt.Errorf("actor PreStart failed: %w", err)
	}

	// 启动Actor消息处理循环
	as.wg.Add(1)
	go as.runActor(ra)

	return ref, nil
}

// runActor Actor消息处理循环
func (as *ActorSystem) runActor(ra *runningActor) {
	defer as.wg.Done()
	defer ra.actor.PostStop(ra.ctx)

	for {
		select {
		case msg := <-ra.ref.mailbox:
			// 处理消息
			if err := ra.actor.Receive(ra.ctx, msg); err != nil {
				fmt.Printf("Actor %s error processing message: %v\n", ra.ref.ID(), err)
			}

		case <-ra.stopCh:
			// Actor停止
			return

		case <-as.stopCh:
			// 整个系统停止
			return
		}
	}
}

// Stop 停止指定的Actor
func (as *ActorSystem) Stop(id string) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	ra, ok := as.actors[id]
	if !ok {
		return fmt.Errorf("actor %s not found", id)
	}

	if !ra.stopped {
		close(ra.stopCh)
		ra.stopped = true
	}

	delete(as.actors, id)
	return nil
}

// GetActor 获取Actor引用
func (as *ActorSystem) GetActor(id string) (*ActorRef, bool) {
	as.mu.RLock()
	defer as.mu.RUnlock()

	ra, ok := as.actors[id]
	if !ok {
		return nil, false
	}
	return ra.ref, true
}

// Shutdown 关闭Actor系统
func (as *ActorSystem) Shutdown(timeout time.Duration) error {
	close(as.stopCh)

	// 等待所有Actor停止
	done := make(chan struct{})
	go func() {
		as.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("actor system shutdown timeout")
	}
}

// 消息类型常量
const (
	MsgTypeCommand        = "command"
	MsgTypeEvent          = "event"
	MsgTypeStateTransition = "state_transition"
	MsgTypeConnect        = "connect"
	MsgTypeDisconnect     = "disconnect"
	MsgTypeQueryLogs      = "query_logs"
	MsgTypeShutdown       = "shutdown"
)

// CommandMessage 命令消息
type CommandMessage struct {
	*BaseMessage
	Command Command
}

func NewCommandMessage(cmd Command) *CommandMessage {
	return &CommandMessage{
		BaseMessage: NewMessage(MsgTypeCommand, cmd),
		Command:     cmd,
	}
}

// EventMessage 事件消息
type EventMessage struct {
	*BaseMessage
	Event Event
}

func NewEventMessage(event Event) *EventMessage {
	return &EventMessage{
		BaseMessage: NewMessage(MsgTypeEvent, event),
		Event:       event,
	}
}

// StateTransitionMessage 状态转换消息
type StateTransitionMessage struct {
	*BaseMessage
	FromState State
	ToState   State
}

func NewStateTransitionMessage(from, to State) *StateTransitionMessage {
	return &StateTransitionMessage{
		BaseMessage: NewMessage(MsgTypeStateTransition, nil),
		FromState:   from,
		ToState:     to,
	}
}
