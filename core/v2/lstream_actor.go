package v2

import (
	"context"
	"fmt"
	"time"
)

// LStreamActor 日志流Actor，实现Actor接口
type LStreamActor struct {
	*BaseActor
	client *LStreamClientV2
}

func NewLStreamActor(client *LStreamClientV2) *LStreamActor {
	return &LStreamActor{
		BaseActor: NewBaseActor("lstream-actor"),
		client:    client,
	}
}

// PreStart Actor启动前的初始化
func (lsa *LStreamActor) PreStart(ctx *ActorContext) error {
	lsa.client.logger.Printf("LStreamActor PreStart: %s", ctx.Self().ID())
	return nil
}

// PostStop Actor停止后的清理
func (lsa *LStreamActor) PostStop(ctx *ActorContext) error {
	lsa.client.logger.Printf("LStreamActor PostStop: %s", ctx.Self().ID())
	return nil
}

// Receive 处理接收到的消息
func (lsa *LStreamActor) Receive(actorCtx *ActorContext, msg Message) error {
	switch msg.Type() {
	case MsgTypeCommand:
		return lsa.handleCommandMessage(actorCtx, msg)

	case MsgTypeEvent:
		return lsa.handleEventMessage(actorCtx, msg)

	case MsgTypeStateTransition:
		return lsa.handleStateTransitionMessage(actorCtx, msg)

	case MsgTypeShutdown:
		return lsa.handleShutdown(actorCtx, msg)

	default:
		lsa.client.logger.Printf("Unhandled message type: %s", msg.Type())
		return nil
	}
}

// handleCommandMessage 处理命令消息
func (lsa *LStreamActor) handleCommandMessage(actorCtx *ActorContext, msg Message) error {
	cmdMsg, ok := msg.(*CommandMessage)
	if !ok {
		return ErrInvalidMessage
	}

	cmd := cmdMsg.Command
	lsa.client.logger.Printf("Processing command: %s (type: %s)", cmd.ID(), cmd.Type())

	// 将命令加入队列
	if err := actorCtx.EnqueueCommand(cmd); err != nil {
		lsa.client.logger.Printf("Failed to enqueue command: %v", err)
		return err
	}

	// 根据命令类型执行不同的操作
	switch cmd.Type() {
	case "connect":
		return lsa.handleConnectCommand(actorCtx, cmd)

	case "disconnect":
		return lsa.handleDisconnectCommand(actorCtx, cmd)

	case "query_logs":
		return lsa.handleQueryLogsCommand(actorCtx, cmd)

	case "bootstrap":
		return lsa.handleBootstrapCommand(actorCtx, cmd)

	default:
		lsa.client.logger.Printf("Unknown command type: %s", cmd.Type())
		return fmt.Errorf("unknown command type: %s", cmd.Type())
	}
}

// handleConnectCommand 处理连接命令
func (lsa *LStreamActor) handleConnectCommand(actorCtx *ActorContext, cmd Command) error {
	connectCmd, ok := cmd.(*ConnectCommand)
	if !ok {
		return fmt.Errorf("invalid connect command")
	}

	lsa.client.logger.Printf("Connecting to %s:%d", connectCmd.RemoteHost, connectCmd.Port)

	// 创建命令执行器
	executor := NewLStreamCommandExecutor(lsa.client)

	// 在goroutine中执行命令
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cmd.Timeout())
		defer cancel()

		// 执行连接
		err := cmd.Execute(ctx, executor)

		// 发布事件
		var event Event
		if err != nil {
			lsa.client.logger.Printf("Connection failed: %v", err)
			event = NewConnectEvent(EventTypeConnectFailed, connectCmd.RemoteHost, err)

			// 触发状态机事件
			if lsa.client.stateMachine != nil {
				_ = lsa.client.stateMachine.HandleEvent(event)
			}
		} else {
			lsa.client.logger.Printf("Connection succeeded to %s", connectCmd.RemoteHost)
			event = NewConnectEvent(EventTypeConnectSucceeded, connectCmd.RemoteHost, nil)

			// 触发状态机事件
			if lsa.client.stateMachine != nil {
				_ = lsa.client.stateMachine.HandleEvent(event)
			}

			// 连接成功后执行bootstrap
			lsa.executeBootstrap(actorCtx)
		}

		// 发布事件
		lsa.client.eventBus.Publish(event)
	}()

	return nil
}

// handleDisconnectCommand 处理断开连接命令
func (lsa *LStreamActor) handleDisconnectCommand(actorCtx *ActorContext, cmd Command) error {
	lsa.client.logger.Printf("Disconnecting...")

	executor := NewLStreamCommandExecutor(lsa.client)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cmd.Timeout())
		defer cancel()

		err := cmd.Execute(ctx, executor)

		// 发布断开连接事件
		lsa.client.eventBus.Publish(NewBaseEvent(EventTypeDisconnected))

		// 触发状态机事件
		if lsa.client.stateMachine != nil {
			_ = lsa.client.stateMachine.HandleEvent(NewBaseEvent(EventTypeDisconnected))
		}

		if err != nil {
			lsa.client.logger.Printf("Disconnect error: %v", err)
		} else {
			lsa.client.logger.Printf("Disconnected successfully")
		}
	}()

	return nil
}

// handleQueryLogsCommand 处理查询日志命令
func (lsa *LStreamActor) handleQueryLogsCommand(actorCtx *ActorContext, cmd Command) error {
	queryCmd, ok := cmd.(*QueryLogsCommand)
	if !ok {
		return fmt.Errorf("invalid query logs command")
	}

	lsa.client.logger.Printf("Querying logs: %s", queryCmd.Query)

	executor := NewLStreamCommandExecutor(lsa.client)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cmd.Timeout())
		defer cancel()

		startTime := time.Now()
		err := cmd.Execute(ctx, executor)
		duration := time.Since(startTime)

		// 创建命令事件
		cmdEvent := NewCommandEvent(EventTypeCommandCompleted, cmd.ID(), cmd.Type())
		if err != nil {
			cmdEvent = NewCommandEvent(EventTypeCommandFailed, cmd.ID(), cmd.Type())
			cmdEvent.Error = err
			lsa.client.logger.Printf("Query failed: %v", err)
		} else {
			lsa.client.logger.Printf("Query completed in %v", duration)
		}

		// 设置结果
		cmdEvent.Result = &CommandResult{
			CommandID: cmd.ID(),
			Success:   err == nil,
			Error:     err,
			Duration:  duration,
		}

		// 发布事件
		lsa.client.eventBus.Publish(cmdEvent)

		// 触发状态机事件
		if lsa.client.stateMachine != nil {
			_ = lsa.client.stateMachine.HandleEvent(cmdEvent)
		}
	}()

	return nil
}

// handleBootstrapCommand 处理bootstrap命令
func (lsa *LStreamActor) handleBootstrapCommand(actorCtx *ActorContext, cmd Command) error {
	lsa.client.logger.Printf("Executing bootstrap command")

	executor := NewLStreamCommandExecutor(lsa.client)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cmd.Timeout())
		defer cancel()

		err := cmd.Execute(ctx, executor)

		var event Event
		if err != nil {
			event = NewErrorEvent(err, "bootstrap")
			lsa.client.eventBus.Publish(NewBaseEvent(EventTypeBootstrapFailed))
		} else {
			lsa.client.eventBus.Publish(NewBaseEvent(EventTypeBootstrapCompleted))
		}

		if event != nil {
			lsa.client.eventBus.Publish(event)
		}
	}()

	return nil
}

// executeBootstrap 执行bootstrap流程
func (lsa *LStreamActor) executeBootstrap(actorCtx *ActorContext) {
	lsa.client.logger.Printf("Starting bootstrap process")

	// 发布bootstrap开始事件
	lsa.client.eventBus.Publish(NewBaseEvent(EventTypeBootstrapStarted))

	// 创建bootstrap命令
	bootstrapCmd := NewBootstrapCommand(
		fmt.Sprintf("bootstrap-%d", time.Now().UnixNano()),
		"/tmp/nerdlog_agent.sh",
		[]string{"logstream-info"},
	)

	// 发送bootstrap命令消息
	msg := NewCommandMessage(bootstrapCmd)
	if err := actorCtx.Self().Tell(msg); err != nil {
		lsa.client.logger.Printf("Failed to send bootstrap command: %v", err)
		lsa.client.eventBus.Publish(NewBaseEvent(EventTypeBootstrapFailed))
	}
}

// handleEventMessage 处理事件消息
func (lsa *LStreamActor) handleEventMessage(actorCtx *ActorContext, msg Message) error {
	eventMsg, ok := msg.(*EventMessage)
	if !ok {
		return ErrInvalidMessage
	}

	// 将事件传递给状态机处理
	if lsa.client.stateMachine != nil {
		return lsa.client.stateMachine.HandleEvent(eventMsg.Event)
	}

	return nil
}

// handleStateTransitionMessage 处理状态转换消息
func (lsa *LStreamActor) handleStateTransitionMessage(actorCtx *ActorContext, msg Message) error {
	stMsg, ok := msg.(*StateTransitionMessage)
	if !ok {
		return ErrInvalidMessage
	}

	lsa.client.logger.Printf("State transition: %s -> %s",
		stMsg.FromState.Name(), stMsg.ToState.Name())

	return nil
}

// handleShutdown 处理关闭消息
func (lsa *LStreamActor) handleShutdown(actorCtx *ActorContext, msg Message) error {
	lsa.client.logger.Printf("Shutdown requested")

	// 先断开连接
	disconnectCmd := NewDisconnectCommand(
		fmt.Sprintf("disconnect-shutdown-%d", time.Now().UnixNano()),
		true,
	)

	disconnectMsg := NewCommandMessage(disconnectCmd)
	_ = actorCtx.Self().Tell(disconnectMsg)

	return nil
}

// LStreamCommandExecutor 日志流命令执行器
type LStreamCommandExecutor struct {
	client *LStreamClientV2
}

func NewLStreamCommandExecutor(client *LStreamClientV2) *LStreamCommandExecutor {
	return &LStreamCommandExecutor{
		client: client,
	}
}

func (lsce *LStreamCommandExecutor) ExecuteShellCommand(cmd string) (string, error) {
	// 实际的shell命令执行逻辑
	// 这里需要使用client的transport来执行命令
	lsce.client.logger.Printf("Executing shell command: %s", cmd)
	return "", ErrCommandNotImplemented
}

func (lsce *LStreamCommandExecutor) GetConnection() interface{} {
	lsce.client.mu.RLock()
	defer lsce.client.mu.RUnlock()
	return lsce.client.connection
}

func (lsce *LStreamCommandExecutor) SendData(data []byte) error {
	// 发送数据到远程连接
	return ErrCommandNotImplemented
}

func (lsce *LStreamCommandExecutor) ReceiveData() ([]byte, error) {
	// 从远程连接接收数据
	return nil, ErrCommandNotImplemented
}
