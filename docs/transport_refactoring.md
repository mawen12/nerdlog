# Transport Layer 重构总结

## 概述

本次重构将原 `core/` 目录下的 transport 相关代码重构到新的 `internal/transport/` 包中，参考 Docker 的代码风格和架构模式。

## 重构目标

### 设计原则（参考 Docker）

1. **清晰的接口定义** - Transport 和 Connection 接口明确分离
2. **Context 支持** - 所有操作支持取消和超时
3. **结构化错误处理** - 使用 errors 包进行错误包装和注解
4. **工厂模式** - 通过 New() 函数统一创建 transport 实例
5. **配置验证** - Config 结构体在使用前进行验证
6. **资源管理** - 显式的 Close() 方法管理资源

### 架构改进

- **分层清晰** - 接口定义与实现分离
- **易于测试** - 提供 mock 实现和测试用例
- **向后兼容** - 通过 adapter 层保持与旧代码的兼容性
- **可扩展** - 易于添加新的 transport 类型（如 teleport）

## 文件结构对比

### 重构前 (core/)

```
core/
├── shell_transport.go               # 接口定义
├── transport_mode.go                # 模式定义
├── shell_transport_ssh_lib.go       # SSH 实现 (446行)
├── shell_transport_custom_cmd.go    # 自定义命令 (250行)
└── (无本地 transport 实现)
```

### 重构后 (internal/transport/)

```
internal/transport/
├── transport.go       # 核心接口和工厂 (350行)
├── local.go          # 本地 shell 实现 (170行)
├── ssh.go            # SSH 库实现 (430行)
├── custom.go         # 自定义命令实现 (320行)
├── adapter.go        # 向后兼容适配器 (80行)
├── README.md         # 完整文档
└── transport_test.go # 单元测试 (300行)
```

## 主要变化

### 1. 接口设计

#### 旧接口

```go
type ShellTransport interface {
    Connect(resCh chan<- ShellConnUpdate)
}

type ShellConn interface {
    Stdin() io.Writer
    Stdout() io.Reader
    Stderr() io.Reader
    Close()
}
```

问题：
- 没有 context 支持
- Connect 是异步的，需要轮询 channel
- Close 没有返回错误
- 没有 Transport 资源管理

#### 新接口

```go
type Transport interface {
    Connect(ctx context.Context, updateCh chan<- Update) (Connection, error)
    Close() error
}

type Connection interface {
    Stdin() io.Writer
    Stdout() io.Reader
    Stderr() io.Reader
    Close() error
}
```

改进：
- ✅ Context 支持取消和超时
- ✅ Connect 同步返回，错误处理清晰
- ✅ Close 返回错误
- ✅ Transport 可管理全局资源（如 jumphost 连接池）

### 2. 配置方式

#### 旧方式

```go
// SSH 模式
transport := core.NewShellTransportSSHLib(core.ShellTransportSSHLibParams{
    SSHKeys: []string{"~/.ssh/id_rsa"},
    ConnDetails: ConfigLogStreamShellTransportSSHLib{
        Host: ConfigHost{Addr: "host:22", User: "user"},
    },
    Logger: logger,
})

// 自定义命令模式
transport := core.NewShellTransportCustomCmd(core.ShellTransportCustomCmdParams{
    ShellCommand: "ssh user@host",
    EnvOverride: map[string]string{"NLHOST": "host"},
    Logger: logger,
})
```

问题：
- 每种模式有不同的构造函数
- 参数结构不统一
- 无配置验证

#### 新方式

```go
config := &transport.Config{
    Mode: transport.ModeSSHLib,
    Host: &transport.HostConfig{
        Addr: "host:22",
        User: "user",
    },
    SSHKeys: []string{"~/.ssh/id_rsa"},
    Logger: logger,
}

trans, err := transport.New(config)  // 统一工厂函数
if err != nil {
    // 配置验证失败
}
```

改进：
- ✅ 统一的 Config 结构体
- ✅ 单一工厂函数 New()
- ✅ 自动配置验证
- ✅ 类型安全的 Mode 枚举

### 3. 连接建立

#### 旧方式

```go
resCh := make(chan core.ShellConnUpdate)
transport.Connect(resCh)

// 需要循环等待结果
for update := range resCh {
    if update.DebugInfo != nil {
        log.Info(update.DebugInfo.Message)
    }
    if update.DataRequest != nil {
        // 处理密码请求
        update.DataRequest.ResponseCh <- password
    }
    if update.Result != nil {
        if update.Result.Err != nil {
            return update.Result.Err
        }
        conn = update.Result.Conn
        break
    }
}
```

问题：
- 需要手动循环处理更新
- 无超时控制
- 无法取消连接

#### 新方式

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

updateCh := make(chan transport.Update, 10)
go func() {
    for update := range updateCh {
        if update.DebugInfo != nil {
            log.Info(update.DebugInfo.Message)
        }
        if update.DataRequest != nil {
            update.DataRequest.ResponseCh <- password
        }
    }
}()

conn, err := trans.Connect(ctx, updateCh)
if err != nil {
    return err
}
defer conn.Close()
```

改进：
- ✅ 使用 context 控制超时和取消
- ✅ 直接返回连接，错误处理清晰
- ✅ 更新处理在独立 goroutine
- ✅ defer 确保资源释放

### 4. 错误处理

#### 旧方式

```go
if err != nil {
    res.Err = errors.Annotatef(err, "getting stdin pipe")
    return res
}
```

#### 新方式

```go
if err != nil {
    return nil, FormatError(err, "creating stdin pipe")
}
```

改进：
- ✅ 统一的错误格式化
- ✅ 一致的错误前缀 "transport:"
- ✅ 更好的错误上下文

### 5. 资源管理

#### 新增功能

```go
// Transport 级别资源管理
trans, err := transport.New(config)
defer trans.Close()  // 清理 jumphost 连接池等

// Connection 级别资源管理  
conn, err := trans.Connect(ctx, updateCh)
defer conn.Close()  // 关闭 stdin/stdout/stderr，终止进程
```

改进：
- ✅ 明确的资源所有权
- ✅ Jumphost 连接复用
- ✅ 认证方法缓存
- ✅ 优雅的资源释放

## 代码示例对比

### SSH 连接

#### 旧代码

```go
transport := core.NewShellTransportSSHLib(core.ShellTransportSSHLibParams{
    SSHKeys: []string{"~/.ssh/id_rsa"},
    ConnDetails: core.ConfigLogStreamShellTransportSSHLib{
        Host: core.ConfigHost{
            Addr: "example.com:22",
            User: "user",
        },
    },
    Logger: logger,
})

resCh := make(chan core.ShellConnUpdate)
transport.Connect(resCh)

var conn core.ShellConn
for update := range resCh {
    if update.Result != nil {
        if update.Result.Err != nil {
            return update.Result.Err
        }
        conn = update.Result.Conn
        break
    }
}
```

#### 新代码

```go
config := &transport.Config{
    Mode: transport.ModeSSHLib,
    Host: &transport.HostConfig{
        Addr: "example.com:22",
        User: "user",
    },
    SSHKeys: []string{"~/.ssh/id_rsa"},
    Logger: transport.NewLoggerAdapter(logger),
}

trans, err := transport.New(config)
if err != nil {
    return err
}
defer trans.Close()

ctx := context.Background()
conn, err := trans.Connect(ctx, make(chan transport.Update, 10))
if err != nil {
    return err
}
defer conn.Close()
```

### Jumphost 支持

#### 新代码（旧代码类似但更复杂）

```go
config := &transport.Config{
    Mode: transport.ModeSSHLib,
    Host: &transport.HostConfig{
        Addr: "internal-server:22",
        User: "user",
    },
    Jumphost: &transport.HostConfig{
        Addr: "bastion.example.com:22",
        User: "jumpuser",
    },
    SSHKeys: []string{"~/.ssh/id_rsa"},
    Logger: logger,
}

trans, err := transport.New(config)
// Jumphost 连接自动管理和复用
```

## Docker 风格体现

### 1. 接口优先设计

像 Docker 的 `client.Client` 接口，定义清晰的契约：

```go
type Transport interface {
    Connect(ctx context.Context, updateCh chan<- Update) (Connection, error)
    Close() error
}
```

### 2. Context 无处不在

Docker 所有 API 都接受 context.Context：

```go
func (t *sshTransport) Connect(ctx context.Context, ...) (Connection, error)
```

### 3. 配置验证

Docker 在使用前验证配置：

```go
func (c *Config) Validate() error {
    // 验证逻辑
}
```

### 4. 错误注解

Docker 使用 errors 包添加上下文：

```go
return FormatError(err, "connecting to host")
```

### 5. 资源池化

像 Docker 的连接池，Transport 缓存 jumphost 连接：

```go
type sshTransport struct {
    jumphostCache   map[string]*ssh.Client
    jumphostCacheMu sync.Mutex
}
```

### 6. Logger 注入

Docker 注入 logger 而非使用全局：

```go
type Config struct {
    Logger Logger  // 接口注入
}
```

## 迁移建议

### 短期（兼容性）

1. **保留旧代码** - 暂时保持 `core/shell_transport*.go` 文件
2. **使用适配器** - 在需要的地方使用 `adapter.go` 桥接
3. **新代码使用新包** - 所有新功能使用 `internal/transport`

### 中期（渐进式迁移）

1. **迁移测试** - 首先迁移测试代码
2. **迁移 lstream_client** - 更新 `core/lstream_client.go` 使用新 transport
3. **更新配置解析** - 修改 `core/lstreams_resolver.go`

### 长期（完全切换）

1. **删除旧代码** - 移除 `core/shell_transport*.go`
2. **删除 transport_mode.go** - 模式定义已在新包中
3. **更新文档** - 更新所有引用旧 API 的文档

## 测试

新包包含完整的测试套件：

```bash
cd internal/transport
go test -v          # 运行所有测试
go test -cover      # 查看覆盖率
```

测试包括：
- ✅ 本地 transport 基本功能
- ✅ 配置验证
- ✅ 模式解析
- ✅ Context 取消
- ✅ 更新通道机制
- ✅ Mock 实现示例

## 性能考虑

新实现的性能改进：

1. **连接复用** - Jumphost 连接缓存
2. **认证缓存** - 避免重复密码提示
3. **并发安全** - 使用互斥锁保护共享状态
4. **资源池化** - Transport 可创建多个 Connection

## 下一步

1. **编写集成测试** - 测试实际的 SSH 连接
2. **性能基准测试** - 对比新旧实现性能
3. **更新 core 包** - 逐步迁移 core 包使用新 transport
4. **添加 metrics** - 收集连接成功率、延迟等指标
5. **支持 teleport** - 添加 tsh transport 模式

## 参考资料

- [Docker CLI Transport](https://github.com/docker/cli/tree/master/cli/command/cli)
- [Go Context 最佳实践](https://go.dev/blog/context)
- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)

## 总结

此次重构遵循 Docker 的优秀实践，提供了：

- ✅ 更清晰的接口定义
- ✅ 更好的错误处理
- ✅ Context 支持
- ✅ 统一的配置方式
- ✅ 完善的文档和测试
- ✅ 向后兼容性
- ✅ 易于扩展的架构

新的 transport 包为 nerdlog 提供了坚实的基础，便于未来添加新功能和进行维护。
