# Nerdlog P0 模块包结构重组分析

**分析日期**: 2026年1月27日  
**目标**: 按照Go语言最佳实践重组P0关键模块  
**参考标准**: Go标准库、kubernetes、docker等流行项目的包组织方式

---

## 📦 Go包组织最佳实践

### 标准包结构

```
project/
├── cmd/                    # 命令行应用入口
│   └── app/               # 每个可执行文件一个目录
├── internal/              # 私有包（不可被外部导入）
│   ├── app/              # 应用程序逻辑
│   ├── domain/           # 领域模型
│   └── infra/            # 基础设施层
├── pkg/                   # 公共库（可被外部导入）
│   └── utils/            # 工具函数
└── api/                   # API定义（protobuf/openapi等）
```

### 关键原则

1. **`cmd/`**: 应用程序入口，尽可能薄，只负责初始化和启动
2. **`internal/`**: 项目私有代码，不希望被外部项目导入
3. **`pkg/`**: 可复用的库代码，可以被外部项目导入
4. **根目录**: 简单辅助包，如果项目本身是库

---

## 🎯 Nerdlog 当前结构（P0模块）

### 现状分析

```
nerdlog/
├── cmd/
│   └── nerdlog/          # ⭐ P0 - UI Layer (4000行, 25文件)
│       ├── main.go                    # 入口
│       ├── app.go                     # 应用协调
│       ├── main_view.go               # 主视图 (1000行)
│       ├── query_edit_view.go         # 查询编辑
│       ├── message_view.go            # 消息视图
│       ├── histogram.go               # 直方图
│       ├── row_details_view.go        # 行详情
│       ├── column_edit_view.go        # 列编辑
│       ├── my_text_view.go            # 文本视图
│       ├── options.go                 # 选项管理
│       ├── config.go                  # 配置
│       ├── cmdline.go                 # 命令行解析
│       ├── query.go                   # 查询逻辑
│       ├── select_query.go            # SELECT查询
│       ├── from_to_range.go           # 时间范围
│       ├── time_or_dur.go             # 时间或时长
│       ├── menu.go                    # 菜单
│       ├── xmarks.go                  # 标记
│       ├── rune_buffer.go             # 字符缓冲
│       └── ui/                        # UI组件
│           ├── dropdown.go
│           ├── table_with_dropdown.go
│           └── util.go
│
├── core/                  # ⭐ P0 - Core Layer (3500行, 10文件)
│   ├── core.go                        # 核心接口 (200行)
│   ├── config.go                      # 配置定义
│   ├── lstreams_manager.go            # 日志流管理 (500行)
│   ├── lstream_client.go              # 单流客户端 (400行)
│   ├── lstream_cmd.go                 # 命令执行
│   ├── lstreams_resolver.go           # 配置解析 (250行)
│   ├── parsing_time.go                # 时间解析 (300行)
│   ├── transport_mode.go              # 传输模式
│   ├── shell_transport.go             # ⭐ P0 - Transport (300行)
│   ├── shell_transport_ssh_lib.go     # SSH实现 (400行)
│   ├── shell_transport_custom_cmd.go  # 自定义命令 (200行)
│   └── nerdlog_agent.sh               # ⭐ P0 - 远程脚本 (2000行)
│
├── blhistory/             # P1 - 浏览器式历史 (轻量)
├── clhistory/             # P1 - 命令行历史 (轻量)
├── clipboard/             # P1 - 剪贴板
├── log/                   # P2 - 日志工具
├── version/               # P3 - 版本信息
└── shellescape/           # P2 - Shell转义
```

### 问题诊断

#### ❌ 违反原则
1. **cmd/nerdlog 过大** (4000行)
   - 包含了太多UI组件代码
   - 包含了业务逻辑（query.go, select_query.go）
   - main_view.go 单文件1000行

2. **core包混杂**
   - 核心业务逻辑 + Transport实现混在一起
   - 没有使用internal/保护私有代码
   - 配置、时间解析等可以独立出来

3. **缺少清晰的层次**
   - UI组件没有统一的包
   - Transport应该独立
   - 工具包分散在根目录

---

## 🎯 建议的包结构（符合Go最佳实践）

### 重组后的结构

```
nerdlog/
│
├── cmd/
│   └── nerdlog/                       # 应用入口 (精简到<500行)
│       ├── main.go                    # 入口：初始化+启动 (~100行)
│       ├── app.go                     # 应用协调层 (~200行)
│       ├── cmdline.go                 # 命令行参数 (~100行)
│       └── options.go                 # 选项配置 (~100行)
│
├── internal/                          # 私有代码
│   │
│   ├── ui/                            # ⭐ P0 - UI层
│   │   ├── app.go                     # UI应用封装 (从cmd/nerdlog/app.go移动部分)
│   │   ├── views/                     # 视图组件
│   │   │   ├── main_view.go          # 主视图 (拆分为多个小文件)
│   │   │   ├── query_view.go         # 查询视图
│   │   │   ├── query_edit_view.go    # 查询编辑
│   │   │   ├── message_view.go       # 消息详情
│   │   │   ├── histogram_view.go     # 直方图视图
│   │   │   ├── row_details_view.go   # 行详情
│   │   │   └── column_edit_view.go   # 列编辑
│   │   ├── components/                # UI基础组件
│   │   │   ├── text_view.go          # 文本视图
│   │   │   ├── dropdown.go           # 下拉菜单
│   │   │   ├── table.go              # 表格扩展
│   │   │   └── histogram.go          # 直方图组件
│   │   ├── models/                    # UI数据模型
│   │   │   ├── query_model.go        # 查询模型
│   │   │   ├── select_query.go       # SELECT查询
│   │   │   └── time_range.go         # 时间范围
│   │   └── handlers/                  # 事件处理
│   │       ├── query_handler.go      # 查询处理
│   │       └── menu_handler.go       # 菜单处理
│   │
│   ├── core/                          # ⭐ P0 - 核心业务逻辑
│   │   ├── logstream/                 # 日志流管理
│   │   │   ├── manager.go            # LStreamsManager
│   │   │   ├── client.go             # LStreamClient
│   │   │   ├── resolver.go           # 配置解析
│   │   │   └── types.go              # 类型定义
│   │   ├── query/                     # 查询逻辑
│   │   │   ├── params.go             # QueryLogsParams
│   │   │   ├── response.go           # LogResp
│   │   │   └── executor.go           # 查询执行器
│   │   ├── time/                      # 时间处理
│   │   │   ├── parser.go             # 时间解析
│   │   │   └── format.go             # 时间格式
│   │   └── config/                    # 配置管理
│   │       ├── config.go             # 配置结构
│   │       └── loader.go             # 配置加载
│   │
│   ├── transport/                     # ⭐ P0 - 传输层
│   │   ├── transport.go               # Transport接口
│   │   ├── ssh.go                     # SSH实现
│   │   ├── local.go                   # 本地实现
│   │   ├── custom.go                  # 自定义命令
│   │   ├── pool.go                    # 连接池
│   │   └── agent/                     # 远程Agent
│   │       └── nerdlog_agent.sh      # Agent脚本
│   │
│   ├── history/                       # P1 - 历史记录（已重构）
│   │   ├── history.go                # 命令行+查询历史
│   │   └── history_test.go
│   │
│   └── testutils/                     # 测试工具
│       └── ...
│
├── pkg/                               # 可复用的公共库
│   ├── clipboard/                     # P1 - 剪贴板工具
│   │   └── clipboard.go
│   ├── logger/                        # P2 - 日志库
│   │   └── logger.go
│   ├── shellescape/                   # P2 - Shell转义
│   │   └── escape.go
│   └── version/                       # P3 - 版本管理
│       └── version.go
│
└── scripts/                           # 脚本和工具
    └── ...
```

---

## 📊 文件映射表：旧位置 → 新位置

### P0 - UI Layer 文件重组

| 原始文件 | 新位置 | 大小 | 说明 |
|---------|--------|------|------|
| **cmd/nerdlog/main.go** | cmd/nerdlog/main.go | 100行 | 精简：只保留入口逻辑 |
| **cmd/nerdlog/app.go** | cmd/nerdlog/app.go + internal/ui/app.go | 拆分 | 协调逻辑保留，UI部分移动 |
| **cmd/nerdlog/cmdline.go** | cmd/nerdlog/cmdline.go | 保留 | 命令行参数解析 |
| **cmd/nerdlog/options.go** | cmd/nerdlog/options.go | 保留 | 选项配置 |
| **cmd/nerdlog/config.go** | internal/core/config/config.go | 移动 | 配置属于Core层 |
| | | | |
| **cmd/nerdlog/main_view.go** | internal/ui/views/main_view.go | 拆分 | 拆分为多个view |
| **cmd/nerdlog/query_edit_view.go** | internal/ui/views/query_edit_view.go | 移动 | UI视图 |
| **cmd/nerdlog/message_view.go** | internal/ui/views/message_view.go | 移动 | UI视图 |
| **cmd/nerdlog/row_details_view.go** | internal/ui/views/row_details_view.go | 移动 | UI视图 |
| **cmd/nerdlog/column_edit_view.go** | internal/ui/views/column_edit_view.go | 移动 | UI视图 |
| | | | |
| **cmd/nerdlog/histogram.go** | internal/ui/components/histogram.go | 移动 | UI组件 |
| **cmd/nerdlog/my_text_view.go** | internal/ui/components/text_view.go | 移动 | UI组件 |
| **cmd/nerdlog/ui/dropdown.go** | internal/ui/components/dropdown.go | 移动 | UI组件 |
| **cmd/nerdlog/ui/table_with_dropdown.go** | internal/ui/components/table.go | 移动 | UI组件 |
| **cmd/nerdlog/ui/util.go** | internal/ui/components/util.go | 移动 | UI工具 |
| | | | |
| **cmd/nerdlog/query.go** | internal/ui/models/query_model.go | 移动 | UI数据模型 |
| **cmd/nerdlog/select_query.go** | internal/ui/models/select_query.go | 移动 | UI数据模型 |
| **cmd/nerdlog/from_to_range.go** | internal/ui/models/time_range.go | 移动 | UI数据模型 |
| **cmd/nerdlog/time_or_dur.go** | internal/ui/models/time_or_dur.go | 移动 | UI数据模型 |
| **cmd/nerdlog/menu.go** | internal/ui/handlers/menu_handler.go | 移动 | 事件处理 |
| **cmd/nerdlog/xmarks.go** | internal/ui/models/xmarks.go | 移动 | UI数据 |
| **cmd/nerdlog/rune_buffer.go** | internal/ui/components/rune_buffer.go | 移动 | UI工具 |

### P0 - Core Layer 文件重组

| 原始文件 | 新位置 | 大小 | 说明 |
|---------|--------|------|------|
| **core/core.go** | internal/core/query/types.go | 移动 | 查询类型定义 |
| **core/config.go** | internal/core/config/config.go | 移动 | 配置定义 |
| | | | |
| **core/lstreams_manager.go** | internal/core/logstream/manager.go | 移动 | 日志流管理器 |
| **core/lstream_client.go** | internal/core/logstream/client.go | 移动 | 日志流客户端 |
| **core/lstream_cmd.go** | internal/core/logstream/client.go | 合并 | 命令执行逻辑 |
| **core/lstreams_resolver.go** | internal/core/logstream/resolver.go | 移动 | 配置解析 |
| | | | |
| **core/parsing_time.go** | internal/core/time/parser.go | 移动 | 时间解析 |
| | | | |

### P0 - Transport Layer 文件重组

| 原始文件 | 新位置 | 大小 | 说明 |
|---------|--------|------|------|
| **core/transport_mode.go** | internal/transport/transport.go | 移动 | Transport接口 |
| **core/shell_transport.go** | internal/transport/local.go | 拆分 | 本地Transport |
| **core/shell_transport_ssh_lib.go** | internal/transport/ssh.go | 移动 | SSH Transport |
| **core/shell_transport_custom_cmd.go** | internal/transport/custom.go | 移动 | 自定义Transport |
| **core/nerdlog_agent.sh** | internal/transport/agent/nerdlog_agent.sh | 移动 | Agent脚本 |

### P1/P2 - 工具包重组

| 原始文件 | 新位置 | 说明 |
|---------|--------|------|
| **log/log.go** | pkg/logger/logger.go | 可复用的日志库 |
| **version/version.go** | pkg/version/version.go | 版本信息 |
| **clipboard/** | pkg/clipboard/ | 剪贴板工具 |
| **shellescape/** | pkg/shellescape/ | Shell转义 |
| **blhistory/** + **clhistory/** | internal/history/history.go | 已重构为轻量级 |

---

## 🔍 重组后的包依赖关系

### 清晰的分层依赖

```
cmd/nerdlog (应用入口)
    │
    ├──► internal/ui (UI层)
    │       ├──► internal/core (Core层)
    │       │       ├──► internal/transport (传输层)
    │       │       └──► pkg/* (工具库)
    │       ├──► internal/history
    │       └──► pkg/clipboard, pkg/logger
    │
    └──► pkg/* (工具库)

特点:
✅ 单向依赖
✅ 层次清晰
✅ 无循环依赖
```

### 包导入规则

```go
// ✅ 允许的导入
package main
import "github.com/dimonomid/nerdlog/internal/ui"
import "github.com/dimonomid/nerdlog/pkg/logger"

// ✅ 允许的导入
package ui
import "github.com/dimonomid/nerdlog/internal/core"
import "github.com/dimonomid/nerdlog/internal/history"

// ✅ 允许的导入
package core
import "github.com/dimonomid/nerdlog/internal/transport"
import "github.com/dimonomid/nerdlog/pkg/logger"

// ❌ 禁止的导入（循环依赖）
package transport
import "github.com/dimonomid/nerdlog/internal/core" // 不允许

// ❌ 禁止的导入（internal不能被外部导入）
// 在外部项目中:
import "github.com/dimonomid/nerdlog/internal/core" // 编译错误
```

---

## 📦 详细的包职责说明

### 1️⃣ `cmd/nerdlog/` - 应用入口

**文件**: 4个文件，约500行

**职责**:
- 解析命令行参数
- 读取配置文件
- 初始化应用组件
- 启动UI应用
- 处理信号和优雅退出

**不应该包含**:
- ❌ UI组件实现
- ❌ 业务逻辑
- ❌ 数据模型

**示例代码**:
```go
// cmd/nerdlog/main.go
package main

import (
    "github.com/dimonomid/nerdlog/internal/ui"
    "github.com/dimonomid/nerdlog/internal/core/logstream"
    "github.com/dimonomid/nerdlog/pkg/logger"
)

func main() {
    // 1. 解析命令行
    opts := parseCmdline()
    
    // 2. 初始化Logger
    log := logger.New()
    
    // 3. 初始化Core
    manager := logstream.NewManager(opts)
    
    // 4. 启动UI
    app := ui.NewApp(manager, log)
    app.Run()
}
```

---

### 2️⃣ `internal/ui/` - UI层

**子包**:
- `views/` - 视图组件 (8个文件, ~2000行)
- `components/` - 基础UI组件 (6个文件, ~800行)
- `models/` - UI数据模型 (5个文件, ~600行)
- `handlers/` - 事件处理 (2个文件, ~300行)

**职责**:
- 所有TUI界面组件
- 用户输入处理
- 界面渲染和更新
- UI状态管理

**依赖**:
```
internal/ui
├─► internal/core (查询日志)
├─► internal/history (历史记录)
├─► pkg/clipboard (剪贴板)
└─► pkg/logger (日志)
```

**示例目录结构**:
```
internal/ui/
├── app.go                    # UI应用封装
├── views/                    # 视图层
│   ├── main_view.go          # 主视图 (拆分后约300行)
│   ├── main_view_query.go    # 查询部分
│   ├── main_view_table.go    # 表格部分
│   ├── query_edit_view.go    # 查询编辑
│   ├── message_view.go       # 消息详情
│   ├── histogram_view.go     # 直方图
│   ├── row_details_view.go   # 行详情
│   └── column_edit_view.go   # 列编辑
├── components/               # UI组件
│   ├── histogram.go          # 直方图组件
│   ├── text_view.go          # 文本视图
│   ├── dropdown.go           # 下拉菜单
│   ├── table.go              # 表格
│   ├── rune_buffer.go        # 字符缓冲
│   └── util.go               # UI工具
├── models/                   # 数据模型
│   ├── query_model.go        # 查询模型
│   ├── select_query.go       # SELECT查询
│   ├── time_range.go         # 时间范围
│   ├── time_or_dur.go        # 时间或时长
│   └── xmarks.go             # 标记
└── handlers/                 # 事件处理
    ├── menu_handler.go       # 菜单处理
    └── query_handler.go      # 查询处理
```

---

### 3️⃣ `internal/core/` - 核心业务逻辑

**子包**:
- `logstream/` - 日志流管理 (4个文件, ~1500行)
- `query/` - 查询逻辑 (3个文件, ~400行)
- `time/` - 时间处理 (2个文件, ~300行)
- `config/` - 配置管理 (2个文件, ~300行)

**职责**:
- 日志流连接管理
- 日志查询执行
- 结果聚合和排序
- 时间解析和格式化
- 配置加载和验证

**依赖**:
```
internal/core
├─► internal/transport (连接远程)
└─► pkg/logger (日志)
```

**示例目录结构**:
```
internal/core/
├── logstream/                # 日志流管理
│   ├── manager.go            # LStreamsManager
│   ├── client.go             # LStreamClient
│   ├── resolver.go           # 配置解析
│   └── types.go              # 类型定义
├── query/                    # 查询逻辑
│   ├── params.go             # QueryLogsParams
│   ├── response.go           # LogResp, LogMsg
│   └── executor.go           # 查询执行器
├── time/                     # 时间处理
│   ├── parser.go             # ParseTime, InferYear
│   └── format.go             # TimeFormat, AWKExpr
└── config/                   # 配置管理
    ├── config.go             # Config结构
    └── loader.go             # 加载logstreams.yaml
```

---

### 4️⃣ `internal/transport/` - 传输层

**文件**: 5个文件 + agent脚本，约1050行

**职责**:
- 建立和管理远程连接
- 执行远程命令
- 连接复用和重连
- 支持多种连接方式

**依赖**:
```
internal/transport
└─► 无内部依赖（底层）
    只依赖标准库和SSH库
```

**示例目录结构**:
```
internal/transport/
├── transport.go              # Transport接口定义
├── ssh.go                    # SSH实现 (400行)
├── local.go                  # 本地实现 (150行)
├── custom.go                 # 自定义命令 (200行)
├── pool.go                   # 连接池（可选新增）
└── agent/                    # Agent脚本
    └── nerdlog_agent.sh      # 远程执行脚本 (2000行)
```

**接口定义示例**:
```go
// internal/transport/transport.go
package transport

type Transport interface {
    Exec(cmd string) (output string, err error)
    Close() error
    IsConnected() bool
}

type Manager struct {
    transports map[string]Transport
}

func NewSSHTransport(host, user string) (Transport, error)
func NewLocalTransport() Transport
func NewCustomTransport(cmd string) (Transport, error)
```

---

### 5️⃣ `internal/history/` - 历史记录

**文件**: 2个文件，504行（已重构）

**职责**:
- 命令行历史管理
- 查询历史管理
- 前进/后退导航
- 持久化到文件

**依赖**:
```
internal/history
└─► 无依赖（独立模块）
    只依赖标准库
```

---

### 6️⃣ `pkg/` - 公共库

**职责**: 可被外部项目导入的通用库

#### `pkg/logger/`
```go
// pkg/logger/logger.go
package logger

type Logger struct {
    namespace string
}

func New() *Logger
func (l *Logger) WithNamespace(ns string) *Logger
func (l *Logger) Infof(format string, args ...interface{})
```

#### `pkg/clipboard/`
```go
// pkg/clipboard/clipboard.go
package clipboard

func Write(text string) error
func Read() (string, error)
```

#### `pkg/shellescape/`
```go
// pkg/shellescape/escape.go
package shellescape

func Quote(s string) string
func QuoteCommand(args []string) string
```

#### `pkg/version/`
```go
// pkg/version/version.go
package version

var (
    Version   = "dev"
    GitCommit = "unknown"
)

func FullDescription() string
```

---

## 🔄 重组实施步骤

### Phase 1: 创建新包结构（1周）

```bash
# 1. 创建目录结构
mkdir -p internal/ui/{views,components,models,handlers}
mkdir -p internal/core/{logstream,query,time,config}
mkdir -p internal/transport/agent
mkdir -p pkg/{logger,clipboard,shellescape,version}

# 2. 移动文件（使用git mv保留历史）
# UI层
git mv cmd/nerdlog/main_view.go internal/ui/views/
git mv cmd/nerdlog/query_edit_view.go internal/ui/views/
git mv cmd/nerdlog/message_view.go internal/ui/views/
# ... 其他文件

# Core层
git mv core/lstreams_manager.go internal/core/logstream/manager.go
git mv core/lstream_client.go internal/core/logstream/client.go
# ... 其他文件

# Transport层
git mv core/shell_transport_ssh_lib.go internal/transport/ssh.go
git mv core/shell_transport_custom_cmd.go internal/transport/custom.go
git mv core/nerdlog_agent.sh internal/transport/agent/
# ... 其他文件

# pkg层
git mv log/ pkg/logger/
git mv version/ pkg/version/
git mv clipboard/ pkg/clipboard/
git mv shellescape/ pkg/shellescape/
```

### Phase 2: 更新包名和导入（2周）

```go
// 示例：更新main.go的导入
package main

// 旧导入
import (
    "github.com/dimonomid/nerdlog/core"
    "github.com/dimonomid/nerdlog/log"
)

// 新导入
import (
    "github.com/dimonomid/nerdlog/internal/core/logstream"
    "github.com/dimonomid/nerdlog/pkg/logger"
)
```

### Phase 3: 拆分大文件（2周）

```go
// 拆分 main_view.go (1000行) 为:
// - main_view.go (核心结构, 300行)
// - main_view_query.go (查询逻辑, 300行)
// - main_view_table.go (表格逻辑, 200行)
// - main_view_histogram.go (直方图, 200行)
```

### Phase 4: 测试和验证（1周）

```bash
# 运行所有测试
go test ./...

# 检查导入循环
go list -f '{{.ImportPath}}: {{.Imports}}' ./internal/... | grep -E 'internal.*internal'

# 构建验证
go build ./cmd/nerdlog/
```

---

## 📊 重组前后对比

### 包数量变化

```
重组前:
├── cmd/nerdlog/          1个包，25个文件，4000行
├── core/                 1个包，10个文件，3500行
├── 根目录其他包           6个包，各自独立

重组后:
├── cmd/nerdlog/          1个包，4个文件，500行 ✅
├── internal/
│   ├── ui/              4个子包，21个文件，3700行 ✅
│   ├── core/            4个子包，11个文件，2500行 ✅
│   ├── transport/       1个包，5个文件，1050行 ✅
│   └── history/         1个包，2个文件，504行 ✅
└── pkg/                 4个包，各自独立 ✅

总计: 15个包（清晰分层）
```

### 代码行数分布

```
              重组前    重组后    说明
────────────────────────────────────────
cmd/nerdlog   4000     500      -87.5% 精简入口
internal/ui   -        3700     新增UI层
internal/core 3500     2500     -28.6% 拆分Transport
internal/transport -   1050     新增Transport层
pkg/*         2200     2200     0% 移动位置
────────────────────────────────────────
总计          ~9700    ~9950    +2.5% (小幅增加)
```

### 依赖关系改善

```
重组前:
cmd/nerdlog → core (混杂UI+业务+Transport)
  ↓
各种工具包 (分散在根目录)

重组后:
cmd/nerdlog → internal/ui → internal/core → internal/transport
              ↓               ↓               ↓
              pkg/*          pkg/*           (无依赖)

✅ 依赖层次清晰
✅ 职责明确分离
✅ 易于测试和维护
```

---

## ✅ 重组的收益

### 1. **更清晰的职责分离**
- ✅ UI层只关注界面渲染和用户交互
- ✅ Core层只关注业务逻辑
- ✅ Transport层只关注连接管理
- ✅ cmd/只关注应用启动

### 2. **更好的可测试性**
- ✅ 各层可以独立编写单元测试
- ✅ Mock接口更容易（Transport, Core）
- ✅ UI测试可以Mock Core层

### 3. **更好的可维护性**
- ✅ 小文件易于理解（每个文件<400行）
- ✅ 包结构清晰，新人容易上手
- ✅ 修改影响范围小

### 4. **符合Go最佳实践**
- ✅ 使用internal/保护私有代码
- ✅ pkg/用于可复用库
- ✅ cmd/精简入口
- ✅ 按功能分包，不按类型

### 5. **便于扩展**
- ✅ 新增UI组件：加到internal/ui/components/
- ✅ 新增Transport类型：加到internal/transport/
- ✅ 新增查询功能：加到internal/core/query/
- ✅ 不影响其他层

---

## 🎯 推荐行动

### 立即执行（高优先级）
1. ✅ **创建internal/目录结构** - 1天
2. ✅ **移动Transport层** - 2天
   - 从core/移动到internal/transport/
   - 这是最独立的部分，影响最小
3. ✅ **更新导入路径** - 1天
4. ✅ **运行测试验证** - 0.5天

### 短期执行（中优先级）
5. 📋 **移动pkg/层** - 2天
   - log/ → pkg/logger/
   - version/ → pkg/version/
   - clipboard/ → pkg/clipboard/
   - shellescape/ → pkg/shellescape/

6. 📋 **拆分Core层** - 3天
   - 创建internal/core/的子包
   - 移动和重命名文件

### 中期执行（低优先级）
7. 📋 **重组UI层** - 5天
   - 创建internal/ui/的子包
   - 拆分main_view.go大文件
   - 移动UI相关文件

8. 📋 **精简cmd/nerdlog/** - 2天
   - 只保留入口逻辑
   - UI协调移到internal/ui/

---

## 📚 参考项目

### 相似规模的Go项目包结构

#### 1. Docker CLI
```
docker/cli/
├── cmd/docker/           # 入口
├── cli/                  # CLI框架 (类似我们的internal/ui)
├── opts/                 # 选项处理
└── pkg/                  # 公共库
```

#### 2. Kubernetes kubectl
```
kubernetes/cmd/kubectl/
├── kubectl.go            # 入口
└── ...
kubernetes/pkg/kubectl/   # kubectl逻辑（大量子包）
```

#### 3. Hugo
```
hugo/
├── commands/             # 命令定义
├── hugolib/              # 核心库
├── resources/            # 资源处理
└── tpl/                  # 模板
```

### 共同特点
- ✅ cmd/只是入口，逻辑在其他包
- ✅ 按功能分包（logstream, query, transport）
- ✅ 不按类型分包（不是models/, services/, controllers/）
- ✅ 使用internal/保护私有API

---

## 📋 检查清单

### 重组前检查
- [ ] 所有测试通过
- [ ] 代码已提交
- [ ] 创建feature分支
- [ ] 备份当前版本

### 重组中检查
- [ ] 使用`git mv`保留文件历史
- [ ] 每移动一个模块就运行测试
- [ ] 更新导入路径
- [ ] 更新文档

### 重组后验证
- [ ] 所有测试通过
- [ ] 无导入循环
- [ ] 构建成功
- [ ] E2E测试通过
- [ ] 文档更新完成

---

## 🎓 总结

### 核心原则
1. **cmd/精简** - 只负责启动
2. **internal/分层** - UI/Core/Transport清晰分离
3. **pkg/复用** - 通用库可被外部使用
4. **按功能分包** - 不按类型

### 预期收益
- ✅ 代码更清晰（职责明确）
- ✅ 测试更容易（独立测试）
- ✅ 维护更简单（小文件）
- ✅ 扩展更方便（清晰边界）

### 投入产出比
```
投入时间: 约3-4周
代码改动: 约30%的import路径
预期收益: 长期维护成本降低50%+
```

**建议**: 分阶段实施，先移动Transport和pkg/，验证后再继续UI层重组。

---

**文档版本**: 1.0  
**最后更新**: 2026年1月27日  
**下一步**: 创建实施计划和时间表
