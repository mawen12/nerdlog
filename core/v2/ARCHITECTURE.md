# LStreamClient V2 架构设计文档

## 1. 设计模式应用

### 1.1 Actor模型

**核心概念：**
- Actor是独立的计算单元，拥有私有状态
- Actor之间只通过消息通信，不共享状态
- 每个Actor有一个mailbox（消息队列），顺序处理消息
- 保证线程安全，无需显式锁

**实现细节：**

```go
// Actor接口定义
type Actor interface {
    Receive(ctx *ActorContext, msg Message) error
    PreStart(ctx *ActorContext) error
    PostStop(ctx *ActorContext) error
}

// ActorSystem管理所有Actor
type ActorSystem struct {
    actors map[string]*runningActor
    // 为每个Actor启动独立goroutine处理消息
}

// 消息处理循环（单线程，保证安全）
func (as *ActorSystem) runActor(ra *runningActor) {
    for {
        select {
        case msg := <-ra.ref.mailbox:
            ra.actor.Receive(ra.ctx, msg)  // 顺序处理
        case <-ra.stopCh:
            return
        }
    }
}
```

**优势：**
1. **线程安全**: 消息顺序处理，无竞态条件
2. **隔离性**: Actor失败不影响其他Actor
3. **简化并发**: 无需手动管理锁和同步

### 1.2 事件驱动架构 (Event-Driven Architecture)

**核心概念：**
- 系统通过事件通信
- 发布-订阅模式解耦组件
- 异步处理，提高响应性

**架构图：**

```
┌─────────────┐         ┌──────────────┐
│  Producer   │────────>│  Event Bus   │
│  (Actor)    │ Publish │              │
└─────────────┘         └──────┬───────┘
                              │
                    ┌─────────┼─────────┐
                    │         │         │
                    v         v         v
              ┌──────────┐ ┌──────────┐ ┌──────────┐
              │Handler 1 │ │Handler 2 │ │Handler 3 │
              └──────────┘ └──────────┘ └──────────┘
```

**事件流：**

1. **连接流程事件**:
   ```
   ConnectRequested → ConnectStarted → ConnectSucceeded/Failed
                                      → BootstrapStarted
                                      → BootstrapCompleted
   ```

2. **命令执行事件**:
   ```
   CommandEnqueued → CommandStarted → CommandCompleted/Failed
   ```

3. **状态变更事件**:
   ```
   StateChanged(from, to) → 通知所有订阅者
   ```

**实现：**

```go
type EventBus struct {
    handlers map[EventType][]EventHandler
    eventCh  chan Event
}

// 异步发布事件
func (eb *EventBus) Publish(event Event) {
    eb.eventCh <- event
}

// 事件处理循环
func (eb *EventBus) processEvents() {
    for event := range eb.eventCh {
        handlers := eb.handlers[event.Type()]
        for _, handler := range handlers {
            // 并发处理，不阻塞其他处理器
            go handler.HandleEvent(event)
        }
    }
}
```

### 1.3 状态机 (State Machine)

**状态图：**

```
                    ┌──────────────┐
                    │              │
                    │ Disconnected │
                    │              │
                    └───────┬──────┘
                            │ ConnectRequested
                            v
                    ┌──────────────┐
         ┌──────────┤              │
         │Failed    │  Connecting  │
         │          │              │
         │          └───────┬──────┘
         │                  │ Succeeded
         │                  v
         │          ┌──────────────┐
         │          │              │◄────┐
         │          │   Connected  │     │ CommandCompleted
         │          │     Idle     │     │
         │          │              │     │
         │          └───────┬──────┘     │
         │                  │ Command    │
         │                  │ Enqueued   │
         │                  v            │
         │          ┌──────────────┐     │
         │          │              │─────┘
         └─────────>│   Connected  │
   DisconnectReq   │     Busy     │
                    │              │
                    └───────┬──────┘
                            │ DisconnectReq
                            v
                    ┌──────────────┐
                    │              │
                    │Disconnecting │
                    │              │
                    └──────────────┘
```

**状态转换表：**

| 当前状态 | 事件 | 下一状态 | 动作 |
|---------|------|---------|------|
| Disconnected | ConnectRequested | Connecting | 开始连接 |
| Connecting | ConnectSucceeded | ConnectedIdle | 执行Bootstrap |
| Connecting | ConnectFailed | Disconnected | 清理资源 |
| ConnectedIdle | CommandEnqueued | ConnectedBusy | 开始执行命令 |
| ConnectedBusy | CommandCompleted | ConnectedIdle | 检查命令队列 |
| * | DisconnectRequested | Disconnecting | 清理连接 |

**实现：**

```go
type StateMachine struct {
    currentState State
    states       map[string]State
}

// 状态接口
type State interface {
    Name() string
    Enter(ctx *StateContext) error   // 进入状态时的动作
    Exit(ctx *StateContext) error    // 退出状态时的动作
    Handle(ctx *StateContext, event Event) (State, error)  // 处理事件
}

// 状态转换
func (sm *StateMachine) TransitionTo(newState State) error {
    oldState := sm.currentState
    oldState.Exit(sm.context)       // 退出旧状态
    newState.Enter(sm.context)      // 进入新状态
    sm.currentState = newState
    
    // 发布状态变更事件
    event := NewStateChangeEvent(oldState, newState, "transition")
    sm.context.eventBus.Publish(event)
    
    return nil
}
```

### 1.4 命令模式 (Command Pattern)

**类图：**

```
┌─────────────────┐
│    Command      │
│   (interface)   │
├─────────────────┤
│ + ID()          │
│ + Type()        │
│ + Execute()     │
│ + Validate()    │
│ + Timeout()     │
└────────┬────────┘
         │
         │ implements
         │
    ┌────┴─────────────────────────┐
    │                               │
┌───▼────────────┐      ┌──────────▼────────┐
│ConnectCommand  │      │QueryLogsCommand   │
├────────────────┤      ├───────────────────┤
│+ RemoteHost    │      │+ Query            │
│+ Port          │      │+ From/To          │
│+ Username      │      │+ Limit            │
│+ SSHKeys       │      │+ ResponseCh       │
└────────────────┘      └───────────────────┘
```

**命令执行流程：**

```
1. 创建命令
   cmd := NewQueryLogsCommand(id, query, from, to, limit, respCh)

2. 验证命令
   if err := cmd.Validate(); err != nil { ... }

3. 封装为消息
   msg := NewCommandMessage(cmd)

4. 发送给Actor
   actor.Tell(msg)

5. Actor接收并处理
   actor.Receive(ctx, msg) {
       cmd := msg.Command
       cmd.Execute(context, executor)
   }

6. 发布结果事件
   event := NewCommandEvent(EventTypeCommandCompleted, cmd.ID(), cmd.Type())
   eventBus.Publish(event)
```

**命令解释器：**

```go
type DSLInterpreter struct {
    commands map[string]func(args []string) (Command, error)
}

// 支持DSL解析
// "connect host:22 --user=admin --key=/path/to/key"
//    ↓ Parse
// ConnectCommand{Host: "host", Port: 22, User: "admin", Key: "/path/to/key"}

func (di *DSLInterpreter) Parse(input string) (Command, error) {
    // 词法分析 → 语法分析 → 生成命令对象
}
```

## 2. 组件交互

### 2.1 完整流程示例：查询日志

```
用户调用                Actor处理              状态机              事件总线
   │                      │                     │                     │
   │──QueryLogs()──────>  │                     │                     │
   │                      │                     │                     │
   │                      │─Enqueue Command──>  │                     │
   │                      │                     │                     │
   │                      │─────Publish────────────>CommandEnqueued   │
   │                      │                     │                     │
   │                      │                     │<─HandleEvent────────│
   │                      │                     │                     │
   │                      │                     │──Transition──>      │
   │                      │                     │  Idle→Busy          │
   │                      │                     │                     │
   │                      │<─StateChanged───────────Publish───────────│
   │                      │                     │                     │
   │<─Update(StateChange)─│                     │                     │
   │                      │                     │                     │
   │                      │──Execute Command──> │                     │
   │                      │  (goroutine)        │                     │
   │                      │                     │                     │
   │                      │─────Publish────────────>LogLineReceived   │
   │                      │     (多次)          │                     │
   │                      │                     │                     │
   │<─Log Lines─────────────────────────────────────Handle───────────│
   │  (via responseCh)    │                     │                     │
   │                      │                     │                     │
   │                      │─────Publish────────────>CommandCompleted  │
   │                      │                     │                     │
   │                      │                     │<─HandleEvent────────│
   │                      │                     │                     │
   │                      │                     │──Transition──>      │
   │                      │                     │  Busy→Idle          │
   │                      │                     │                     │
   │<─Update(Complete)────────────────────────────────Publish────────│
   │                      │                     │                     │
```

### 2.2 连接流程详细图

```
Client                LStreamActor           Transport           StateMachine        EventBus
  │                        │                      │                    │                 │
  │──Connect()──────────>  │                      │                    │                 │
  │                        │                      │                    │                 │
  │                        │────Publish─────────────────────────────────>ConnectRequested
  │                        │                      │                    │                 │
  │                        │                      │                    │<─Handle─────────│
  │                        │                      │                    │                 │
  │                        │                      │                    │──To Connecting  │
  │                        │                      │                    │                 │
  │                        │──doConnect()──────>  │                    │                 │
  │                        │                      │                    │                 │
  │                        │                      │──SSH Handshake──>  │                 │
  │                        │                      │                    │                 │
  │                        │<─ConnUpdate────────  │                    │                 │
  │                        │  (progress)          │                    │                 │
  │                        │                      │                    │                 │
  │<─Update(ConnDetails)───│                      │                    │                 │
  │                        │                      │                    │                 │
  │                        │<─Connected───────────│                    │                 │
  │                        │                      │                    │                 │
  │                        │────Publish─────────────────────────────────>ConnectSucceeded
  │                        │                      │                    │                 │
  │                        │                      │                    │<─Handle─────────│
  │                        │                      │                    │                 │
  │                        │                      │                    │──To ConnIdle────│
  │                        │                      │                    │                 │
  │                        │──Bootstrap()──────>  │                    │                 │
  │                        │                      │                    │                 │
  │                        │────Publish─────────────────────────────────>BootstrapStarted
  │                        │                      │                    │                 │
  │                        │──Execute Agent───────>                    │                 │
  │                        │                      │                    │                 │
  │                        │<─Timezone, Examples──│                    │                 │
  │                        │                      │                    │                 │
  │                        │────Publish─────────────────────────────────>BootstrapCompleted
  │                        │                      │                    │                 │
  │<─Update(Bootstrap)─────│                      │                    │                 │
  │                        │                      │                    │                 │
```

## 3. 线程安全保证

### 3.1 无锁设计

**Actor模型保证：**
- 每个Actor单线程处理消息
- 消息队列天然串行化访问
- 无需显式锁

**代码示例：**

```go
// ❌ V1版本需要显式锁
func (lsc *LStreamClient) changeState(newState LStreamClientState) {
    lsc.mu.Lock()
    defer lsc.mu.Unlock()
    lsc.state = newState
    // ...
}

// ✅ V2版本通过消息传递，无需锁
func (lsa *LStreamActor) Receive(ctx *ActorContext, msg Message) error {
    // 单线程处理，天然安全
    switch msg.Type() {
    case MsgTypeCommand:
        // 处理命令
    }
}
```

### 3.2 共享状态管理

**原则：**
1. 状态封装在Actor内部
2. 通过消息读写状态
3. 只读数据可以共享

**实现：**

```go
// 状态存储在ActorContext中
type ActorContext struct {
    data map[string]interface{}  // 私有数据
    mu   sync.RWMutex            // 仅在必要时使用
}

// 读写通过方法封装
func (ac *ActorContext) SetData(key string, value interface{}) {
    ac.mu.Lock()
    defer ac.mu.Unlock()
    ac.data[key] = value
}
```

## 4. 性能优化

### 4.1 异步处理

**消息发送：**
```go
// Tell: 非阻塞发送
func (ar *ActorRef) Tell(msg Message) error {
    select {
    case ar.mailbox <- msg:
        return nil
    default:
        return ErrActorMailboxFull  // 立即返回
    }
}
```

**事件处理：**
```go
// 并发处理多个事件处理器
func (eb *EventBus) dispatchEvent(event Event) {
    handlers := eb.handlers[event.Type()]
    for _, handler := range handlers {
        go handler.HandleEvent(event)  // 并发执行
    }
}
```

### 4.2 批处理

**命令队列：**
```go
type CommandQueue struct {
    commands []Command
    maxSize  int
}

// 批量出队
func (cq *CommandQueue) DequeueBatch(n int) []Command {
    if n > len(cq.commands) {
        n = len(cq.commands)
    }
    batch := cq.commands[:n]
    cq.commands = cq.commands[n:]
    return batch
}
```

### 4.3 缓冲区大小调优

```go
params := LStreamClientParamsV2{
    MailboxSize:      100,   // Actor消息队列
    EventBufferSize:  1000,  // 事件总线缓冲
    CommandQueueSize: 100,   // 命令队列大小
}
```

**建议：**
- 高吞吐：增大缓冲区（1000+）
- 低延迟：减小缓冲区（10-100）
- 根据实际负载调整

## 5. 错误处理

### 5.1 Actor监督（未来扩展）

```go
// 监督策略
type SupervisionStrategy interface {
    HandleFailure(actor Actor, err error) SupervisionDecision
}

type SupervisionDecision int

const (
    Resume    SupervisionDecision = iota  // 继续
    Restart                                // 重启Actor
    Stop                                   // 停止Actor
    Escalate                              // 上报给父Actor
)
```

### 5.2 错误事件

```go
// 发布错误事件
event := NewErrorEvent(err, "context")
eventBus.Publish(event)

// 错误处理器
type ErrorHandler struct{}

func (eh *ErrorHandler) HandleEvent(event Event) error {
    errorEvent := event.(*ErrorEvent)
    log.Printf("Error in %s: %v", errorEvent.Context, errorEvent.Error)
    // 记录日志、发送告警等
    return nil
}
```

## 6. 测试策略

### 6.1 单元测试

**Actor测试：**
```go
func TestActor(t *testing.T) {
    system := NewActorSystem("test")
    actor := NewTestActor()
    ref, _ := system.Spawn("test-actor", actor, 10, nil)
    
    msg := NewMessage("test", "data")
    ref.Tell(msg)
    
    // 验证结果
}
```

**状态机测试：**
```go
func TestStateMachine(t *testing.T) {
    sm := NewStateMachine(ctx)
    sm.SetInitialState("disconnected")
    
    event := NewBaseEvent(EventTypeConnectRequested)
    sm.HandleEvent(event)
    
    assert.Equal(t, "connecting", sm.CurrentState().Name())
}
```

### 6.2 集成测试

**完整流程测试：**
```go
func TestConnectAndQuery(t *testing.T) {
    client, _ := NewLStreamClientV2(params)
    
    // 连接
    client.Connect()
    waitForState(t, client, "connected_idle")
    
    // 查询
    responseCh := make(chan string)
    client.QueryLogs("query", from, to, 10, responseCh)
    
    // 验证结果
    lines := collectLines(responseCh)
    assert.NotEmpty(t, lines)
}
```

## 7. 监控和调试

### 7.1 日志

```go
// 结构化日志
logger.Printf("[%s] State: %s -> %s",
    actor.ID(),
    oldState.Name(),
    newState.Name())

logger.Printf("[%s] Command %s completed in %v",
    actor.ID(),
    cmd.ID(),
    duration)
```

### 7.2 指标（未来扩展）

```go
type Metrics struct {
    MessagesProcessed  counter
    CommandsExecuted   counter
    StateTransitions   counter
    EventsPublished    counter
    AverageLatency     histogram
}
```

## 8. 总结

V2架构通过以下设计模式实现了：

1. **线程安全**: Actor模型 + 消息传递
2. **解耦**: 事件驱动架构
3. **清晰的状态管理**: 状态机模式
4. **可扩展的命令系统**: 命令模式 + 解释器模式

相比V1的主要改进：
- ✅ 消除大部分显式锁
- ✅ 更好的模块化和可测试性
- ✅ 更容易添加新功能
- ✅ 更强的错误隔离
- ✅ 更好的并发性能
