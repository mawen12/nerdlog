package v2

import "errors"

var (
	// 状态机相关错误
	ErrStateNotFound  = errors.New("state not found")
	ErrNoCurrentState = errors.New("no current state")

	// 命令相关错误
	ErrInvalidCommandID     = errors.New("invalid command ID")
	ErrInvalidCommandType   = errors.New("invalid command type")
	ErrCommandNotImplemented = errors.New("command not implemented")
	ErrCommandQueueFull     = errors.New("command queue is full")
	ErrCommandQueueEmpty    = errors.New("command queue is empty")
	ErrCommandTimeout       = errors.New("command execution timeout")

	// Actor相关错误
	ErrActorStopped      = errors.New("actor is stopped")
	ErrActorMailboxFull  = errors.New("actor mailbox is full")
	ErrInvalidMessage    = errors.New("invalid message")
	ErrMessageTimeout    = errors.New("message send timeout")

	// 连接相关错误
	ErrNotConnected     = errors.New("not connected")
	ErrAlreadyConnected = errors.New("already connected")
	ErrConnectionFailed = errors.New("connection failed")
)
