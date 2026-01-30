package v2

import (
	"context"
	"fmt"
	"time"
)

// Command 命令接口
type Command interface {
	// ID 返回命令唯一标识
	ID() string
	// Type 返回命令类型
	Type() string
	// Execute 执行命令
	Execute(ctx context.Context, executor CommandExecutor) error
	// Validate 验证命令参数
	Validate() error
	// Timeout 返回命令超时时间
	Timeout() time.Duration
}

// CommandExecutor 命令执行器接口，提供命令执行所需的能力
type CommandExecutor interface {
	// ExecuteShellCommand 执行shell命令
	ExecuteShellCommand(cmd string) (string, error)
	// GetConnection 获取当前连接
	GetConnection() interface{}
	// SendData 发送数据
	SendData(data []byte) error
	// ReceiveData 接收数据
	ReceiveData() ([]byte, error)
}

// CommandResult 命令执行结果
type CommandResult struct {
	CommandID string
	Success   bool
	Data      interface{}
	Error     error
	Duration  time.Duration
}

// BaseCommand 命令基础实现
type BaseCommand struct {
	id       string
	cmdType  string
	timeout  time.Duration
	executed bool
}

func NewBaseCommand(id, cmdType string, timeout time.Duration) *BaseCommand {
	if timeout == 0 {
		timeout = 30 * time.Second // 默认超时30秒
	}
	return &BaseCommand{
		id:      id,
		cmdType: cmdType,
		timeout: timeout,
	}
}

func (bc *BaseCommand) ID() string {
	return bc.id
}

func (bc *BaseCommand) Type() string {
	return bc.cmdType
}

func (bc *BaseCommand) Timeout() time.Duration {
	return bc.timeout
}

func (bc *BaseCommand) Validate() error {
	if bc.id == "" {
		return ErrInvalidCommandID
	}
	if bc.cmdType == "" {
		return ErrInvalidCommandType
	}
	return nil
}

func (bc *BaseCommand) Execute(ctx context.Context, executor CommandExecutor) error {
	return ErrCommandNotImplemented
}

// ConnectCommand 连接命令
type ConnectCommand struct {
	*BaseCommand
	RemoteHost string
	Port       int
	Username   string
	SSHKeys    []string
}

func NewConnectCommand(id, remoteHost string, port int, username string, sshKeys []string) *ConnectCommand {
	return &ConnectCommand{
		BaseCommand: NewBaseCommand(id, "connect", 30*time.Second),
		RemoteHost:  remoteHost,
		Port:        port,
		Username:    username,
		SSHKeys:     sshKeys,
	}
}

func (cc *ConnectCommand) Validate() error {
	if err := cc.BaseCommand.Validate(); err != nil {
		return err
	}
	if cc.RemoteHost == "" {
		return fmt.Errorf("remote host is required")
	}
	if cc.Port <= 0 || cc.Port > 65535 {
		return fmt.Errorf("invalid port: %d", cc.Port)
	}
	return nil
}

func (cc *ConnectCommand) Execute(ctx context.Context, executor CommandExecutor) error {
	// 实际的连接逻辑由executor实现
	// 这里只是示例
	return nil
}

// DisconnectCommand 断开连接命令
type DisconnectCommand struct {
	*BaseCommand
	Force bool
}

func NewDisconnectCommand(id string, force bool) *DisconnectCommand {
	return &DisconnectCommand{
		BaseCommand: NewBaseCommand(id, "disconnect", 10*time.Second),
		Force:       force,
	}
}

func (dc *DisconnectCommand) Execute(ctx context.Context, executor CommandExecutor) error {
	// 实际的断开连接逻辑由executor实现
	return nil
}

// QueryLogsCommand 查询日志命令
type QueryLogsCommand struct {
	*BaseCommand
	Query     string
	From      time.Time
	To        time.Time
	Limit     int
	ResponseCh chan<- string
}

func NewQueryLogsCommand(id, query string, from, to time.Time, limit int, respCh chan<- string) *QueryLogsCommand {
	return &QueryLogsCommand{
		BaseCommand: NewBaseCommand(id, "query_logs", 60*time.Second),
		Query:       query,
		From:        from,
		To:          to,
		Limit:       limit,
		ResponseCh:  respCh,
	}
}

func (qlc *QueryLogsCommand) Validate() error {
	if err := qlc.BaseCommand.Validate(); err != nil {
		return err
	}
	if qlc.Query == "" {
		return fmt.Errorf("query is required")
	}
	if qlc.From.After(qlc.To) {
		return fmt.Errorf("from time must be before to time")
	}
	return nil
}

func (qlc *QueryLogsCommand) Execute(ctx context.Context, executor CommandExecutor) error {
	// 实际的查询逻辑由executor实现
	return nil
}

// BootstrapCommand Bootstrap命令
type BootstrapCommand struct {
	*BaseCommand
	ScriptPath string
	Args       []string
}

func NewBootstrapCommand(id, scriptPath string, args []string) *BootstrapCommand {
	return &BootstrapCommand{
		BaseCommand: NewBaseCommand(id, "bootstrap", 30*time.Second),
		ScriptPath:  scriptPath,
		Args:        args,
	}
}

func (bc *BootstrapCommand) Validate() error {
	if err := bc.BaseCommand.Validate(); err != nil {
		return err
	}
	if bc.ScriptPath == "" {
		return fmt.Errorf("script path is required")
	}
	return nil
}

func (bc *BootstrapCommand) Execute(ctx context.Context, executor CommandExecutor) error {
	// 实际的bootstrap逻辑由executor实现
	return nil
}

// CommandInterpreter 命令解释器接口
type CommandInterpreter interface {
	// Parse 解析命令字符串为Command对象
	Parse(input string) (Command, error)
	// Interpret 解释并执行命令
	Interpret(input string, executor CommandExecutor) error
}

// DSLInterpreter DSL解释器，用于解析和执行复杂的查询语言
type DSLInterpreter struct {
	commands map[string]func(args []string) (Command, error)
}

func NewDSLInterpreter() *DSLInterpreter {
	di := &DSLInterpreter{
		commands: make(map[string]func(args []string) (Command, error)),
	}
	di.registerDefaultCommands()
	return di
}

func (di *DSLInterpreter) registerDefaultCommands() {
	// 注册默认命令
	di.commands["connect"] = di.parseConnectCommand
	di.commands["disconnect"] = di.parseDisconnectCommand
	di.commands["query"] = di.parseQueryCommand
	di.commands["bootstrap"] = di.parseBootstrapCommand
}

func (di *DSLInterpreter) Parse(input string) (Command, error) {
	// 简单的解析实现，实际应该使用更复杂的解析器
	// 这里只是示例
	return nil, ErrCommandNotImplemented
}

func (di *DSLInterpreter) Interpret(input string, executor CommandExecutor) error {
	cmd, err := di.Parse(input)
	if err != nil {
		return err
	}

	if err := cmd.Validate(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cmd.Timeout())
	defer cancel()

	return cmd.Execute(ctx, executor)
}

func (di *DSLInterpreter) parseConnectCommand(args []string) (Command, error) {
	// 解析连接命令参数
	return nil, ErrCommandNotImplemented
}

func (di *DSLInterpreter) parseDisconnectCommand(args []string) (Command, error) {
	// 解析断开连接命令参数
	return nil, ErrCommandNotImplemented
}

func (di *DSLInterpreter) parseQueryCommand(args []string) (Command, error) {
	// 解析查询命令参数
	return nil, ErrCommandNotImplemented
}

func (di *DSLInterpreter) parseBootstrapCommand(args []string) (Command, error) {
	// 解析bootstrap命令参数
	return nil, ErrCommandNotImplemented
}

// CommandQueue 命令队列
type CommandQueue struct {
	commands []Command
	maxSize  int
}

func NewCommandQueue(maxSize int) *CommandQueue {
	if maxSize <= 0 {
		maxSize = 100 // 默认最大队列长度
	}
	return &CommandQueue{
		commands: make([]Command, 0, maxSize),
		maxSize:  maxSize,
	}
}

func (cq *CommandQueue) Enqueue(cmd Command) error {
	if len(cq.commands) >= cq.maxSize {
		return ErrCommandQueueFull
	}
	cq.commands = append(cq.commands, cmd)
	return nil
}

func (cq *CommandQueue) Dequeue() (Command, error) {
	if len(cq.commands) == 0 {
		return nil, ErrCommandQueueEmpty
	}
	cmd := cq.commands[0]
	cq.commands = cq.commands[1:]
	return cmd, nil
}

func (cq *CommandQueue) Peek() (Command, error) {
	if len(cq.commands) == 0 {
		return nil, ErrCommandQueueEmpty
	}
	return cq.commands[0], nil
}

func (cq *CommandQueue) Size() int {
	return len(cq.commands)
}

func (cq *CommandQueue) IsEmpty() bool {
	return len(cq.commands) == 0
}

func (cq *CommandQueue) Clear() {
	cq.commands = cq.commands[:0]
}
