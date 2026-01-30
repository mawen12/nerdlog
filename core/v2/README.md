# LStreamClient V2 - 基于Actor模型的日志流客户端

## 概述

LStreamClient V2是原始LStreamClient的重构版本，采用现代并发设计模式，包括：

1. **Actor模型** - 并发处理状态、命令和消息，保证线程安全
2. **事件驱动架构** - 处理事件流（如日志流或状态更新）
3. **状态机** - 管理状态变更
4. **命令模式和解释器** - 将命令独立封装，为复杂命令实现解释器

## 架构设计

### 核心组件

```
┌─────────────────────────────────────────────────────────────┐
│                      LStreamClientV2                         │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌───────────────┐    ┌──────────────┐   ┌──────────────┐  │
│  │  ActorSystem  │───>│ LStreamActor │<─>│   EventBus   │  │
│  └───────────────┘    └──────────────┘   └──────────────┘  │
│         │                     │                   │          │
│         │                     │                   │          │
│         v                     v                   v          │
│  ┌───────────────┐    ┌──────────────┐   ┌──────────────┐  │
│  │ MessageQueue  │    │ StateMachine │   │EventHandlers │  │
│  │  (Mailbox)    │    │              │   │              │  │
│  └───────────────┘    └──────────────┘   └──────────────┘  │
│                               │                              │
│                               v                              │
│                       ┌──────────────┐                      │
│                       │ CommandQueue │                      │
│                       │  & Executor  │                      │
│                       └──────────────┘                      │
└─────────────────────────────────────────────────────────────┘
```

### 1. Actor模型 (actor.go)

Actor模型提供了线程安全的并发处理机制：

- **Actor**: 独立的计算单元，通过消息通信
- **ActorRef**: Actor的引用，用于发送消息
- **ActorSystem**: 管理所有Actor的生命周期
- **Message**: Actor之间传递的消息

**特性：**
- 每个Actor有独立的mailbox（消息队列）
- 消息处理是串行的，保证线程安全
- 支持Tell（fire-and-forget）和Ask（request-reply）模式
- 自动管理Actor生命周期（PreStart/PostStop）

```go
// 创建Actor系统
system := NewActorSystem("lstream")

// 创建并启动Actor
actor := NewLStreamActor(client)
ref, _ := system.Spawn("main-actor", actor, 100, eventBus)

// 发送消息
msg := NewCommandMessage(cmd)
ref.Tell(msg)  // 异步发送

// 或等待响应
reply, _ := ref.Ask(msg, 5*time.Second)
```

### 2. 事件驱动架构 (event.go)

事件系统实现了发布-订阅模式：

**事件类型：**
- 连接事件：ConnectRequested, ConnectStarted, ConnectSucceeded, ConnectFailed
- 命令事件：CommandEnqueued, CommandStarted, CommandCompleted, CommandFailed
- 状态事件：StateChanged
- 数据事件：LogLineReceived, DataReceived
- Bootstrap事件：BootstrapStarted, BootstrapCompleted

```go
// 创建事件总线
eventBus := NewEventBus(1000)
eventBus.Start()

// 注册事件处理器
handler := &StateChangeHandler{client: client}
eventBus.Register(EventTypeStateChanged, handler)

// 发布事件
event := NewConnectEvent(EventTypeConnectSucceeded, "host", nil)
eventBus.Publish(event)
```

**特性：**
- 异步事件处理，不阻塞发布者
- 支持多个处理器订阅同一事件
- 自动过滤和路由事件到相应处理器

### 3. 状态机 (state.go)

状态机管理客户端的状态转换：

**状态定义：**
- **Disconnected**: 未连接状态
- **Connecting**: 连接中
- **ConnectedIdle**: 已连接，空闲
- **ConnectedBusy**: 已连接，执行命令中
- **Disconnecting**: 断开连接中

**状态转换规则：**
```
Disconnected --[ConnectRequested]--> Connecting
Connecting --[ConnectSucceeded]--> ConnectedIdle
Connecting --[ConnectFailed]--> Disconnected
ConnectedIdle --[CommandEnqueued]--> ConnectedBusy
ConnectedBusy --[CommandCompleted]--> ConnectedIdle
* --[DisconnectRequested]--> Disconnecting
Disconnecting --[Disconnected]--> Disconnected
```

```go
// 创建状态机
sm := NewStateMachine(stateContext)

// 注册状态
sm.RegisterState(NewDisconnectedState())
sm.RegisterState(NewConnectingState())

// 设置初始状态
sm.SetInitialState("disconnected")

// 处理事件触发状态转换
event := NewBaseEvent(EventTypeConnectRequested)
sm.HandleEvent(event)
```

**特性：**
- 状态进入/退出钩子（Enter/Exit）
- 事件驱动的状态转换
- 自动发布状态变更事件

### 4. 命令模式和解释器 (command.go)

命令模式将操作封装为对象：

**命令类型：**
- **ConnectCommand**: 连接到远程主机
- **DisconnectCommand**: 断开连接
- **QueryLogsCommand**: 查询日志
- **BootstrapCommand**: 执行bootstrap脚本

```go
// 创建命令
cmd := NewConnectCommand("cmd-1", "host", 22, "user", sshKeys)

// 验证命令
if err := cmd.Validate(); err != nil {
    return err
}

// 执行命令
ctx := context.Background()
executor := NewLStreamCommandExecutor(client)
err := cmd.Execute(ctx, executor)
```

**DSL解释器：**
支持解析复杂的查询语言（可扩展）

```go
interpreter := NewDSLInterpreter()
cmd, err := interpreter.Parse("connect host:port --user=username")
```

## 使用示例

### 基本使用

```go
// 1. 创建客户端参数
params := v2.LStreamClientParamsV2{
    LogStream: core.LogStream{
        Name:     "server1",
        Hostname: "192.168.1.100",
        Username: "admin",
    },
    SSHKeys:          []string{"/home/user/.ssh/id_rsa"},
    ClientID:         "client-001",
    UpdatesCh:        updatesCh,
    Logger:           logger,
    MailboxSize:      100,
    EventBufferSize:  1000,
}

// 2. 创建客户端
client, err := v2.NewLStreamClientV2(params)
if err != nil {
    log.Fatal(err)
}
defer client.Close()

// 3. 处理更新
go func() {
    for update := range updatesCh {
        if update.StateChange != nil {
            fmt.Printf("State: %s -> %s\n",
                update.StateChange.OldState,
                update.StateChange.NewState)
        }
    }
}()

// 4. 连接
if err := client.Connect(); err != nil {
    log.Fatal(err)
}

// 5. 查询日志
responseCh := make(chan string, 100)
err = client.QueryLogs(
    "error",
    time.Now().Add(-1*time.Hour),
    time.Now(),
    100,
    responseCh,
)

// 6. 处理结果
for line := range responseCh {
    fmt.Println(line)
}

// 7. 断开连接
client.Disconnect()
```

### 自定义事件处理器

```go
type CustomEventHandler struct {
    // 自定义字段
}

func (ceh *CustomEventHandler) CanHandle(eventType v2.EventType) bool {
    return eventType == v2.EventTypeLogLineReceived
}

func (ceh *CustomEventHandler) HandleEvent(event v2.Event) error {
    logEvent := event.(*v2.LogLineEvent)
    // 处理日志行
    fmt.Printf("Log: %s\n", logEvent.Line)
    return nil
}

// 注册处理器
client.eventBus.Register(v2.EventTypeLogLineReceived, &CustomEventHandler{})
```

### 自定义命令

```go
type CustomCommand struct {
    *v2.BaseCommand
    CustomParam string
}

func (cc *CustomCommand) Execute(ctx context.Context, executor v2.CommandExecutor) error {
    // 实现自定义逻辑
    return nil
}

// 使用自定义命令
cmd := &CustomCommand{
    BaseCommand: v2.NewBaseCommand("custom-1", "custom", 30*time.Second),
    CustomParam: "value",
}

msg := v2.NewCommandMessage(cmd)
client.mainActor.Tell(msg)
```

## 优势

### 相比V1版本的改进

1. **线程安全**: Actor模型天然保证消息处理的线程安全，无需显式锁
2. **可扩展性**: 易于添加新的事件处理器、命令类型和状态
3. **可测试性**: 每个组件都是独立的，便于单元测试
4. **错误隔离**: Actor失败不会影响整个系统
5. **清晰的关注点分离**: 
   - Actor处理消息和协调
   - 状态机管理状态
   - 事件总线处理事件分发
   - 命令封装业务逻辑

### 性能特征

- **并发性**: Actor可以并行处理不同的消息流
- **响应性**: 异步消息处理，不阻塞调用者
- **弹性**: 支持Actor监督和重启策略
- **可伸缩性**: 可以轻松添加更多Actor来分担负载

## 测试

运行测试：

```bash
cd core/v2
go test -v
go test -bench=. -benchmem
```

## 文件结构

```
core/v2/
├── actor.go              # Actor模型实现
├── command.go            # 命令模式和解释器
├── errors.go             # 错误定义
├── event.go              # 事件系统
├── state.go              # 状态机
├── lstream_client_v2.go  # V2客户端主实现
├── lstream_actor.go      # 日志流Actor实现
├── example_test.go       # 示例和测试
└── README.md             # 本文档
```

## 后续改进

1. **监督策略**: 实现Actor监督树，自动重启失败的Actor
2. **持久化**: 添加事件溯源，持久化状态变更
3. **分布式**: 支持远程Actor通信
4. **更多命令**: 实现更多复杂的日志查询命令
5. **性能优化**: 优化消息队列和事件处理性能
6. **监控**: 添加指标收集和监控

## 许可证

与主项目保持一致
