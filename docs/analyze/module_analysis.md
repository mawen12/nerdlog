# Nerdlog 项目功能模块分析

## 📋 项目概述

**Nerdlog** 是一个快速、远程优先的多主机TUI日志查看工具，具有时间线直方图，无需中央服务器。

### 核心设计理念

- **无服务器架构**: 直接SSH连接到远程主机，无需中央日志服务器
- **高效查询**: 在远程节点进行日志分析，只下载过滤后的数据和直方图数据
- **带宽优化**: 使用gzip压缩传输数据
- **灵活连接**: 支持任意shell命令建立连接（可与Teleport集成）

---

## 🏗️ 功能模块架构

### 模块关系图

```
┌─────────────────────────────────────────────────────────────┐
│                        Nerdlog                               │
│                     (cmd/nerdlog)                            │
└────────────┬─────────────────────────────┬──────────────────┘
             │                             │
      ┌──────▼──────┐            ┌────────▼────────┐
      │   UI Layer  │            │   Core Layer    │
      │  (TUI应用)  │            │  (业务逻辑)     │
      └──────┬──────┘            └────────┬────────┘
             │                            │
    ┌────────▼──────────┐       ┌────────▼──────────┐
    │   MainView        │       │   LStreams        │
    │ (主页面UI组件)    │       │   Manager         │
    │                   │       │ (连接管理)        │
    ├─QueryEditView    │       │                   │
    ├─MessageView      │       │                   │
    ├─Histogram        │       │                   │
    ├─History (命令/查询) │     │                   │
    └───────┬───────────┘       └────────┬──────────┘
            │                           │
            │                    ┌──────▼──────┐
            │                    │  Transport  │
            │                    │  (连接层)   │
            │                    ├─SSH/Local  │
            │                    ├─Custom Cmd │
            │                    └─────┬──────┘
            │                          │
            └──────────┬───────────────┘
                       │
            ┌──────────▼──────────┐
            │  Remote LogStreams  │
            │  (远程日志源)       │
            │ /var/log, journalctl│
            └─────────────────────┘
```

---

## 📦 详细模块说明

### 1️⃣ **UI Layer** - 用户界面层 ⭐⭐⭐⭐⭐

**位置**: `cmd/nerdlog/`

**优先级**: 🔴 **CRITICAL** (关键)

**职责**:

- 提供交互式TUI界面
- 处理用户输入和事件
- 显示日志查询结果
- 管理页面导航和状态

**主要组件**:

| 组件               | 文件                | 功能                         | 代码行数 |
| ------------------ | ------------------- | ---------------------------- | -------- |
| **MainView**       | main_view.go        | 主页面容器，整合所有UI子组件 | ~1000    |
| **QueryEditView**  | query_edit_view.go  | 日志流和查询条件编辑         | ~300     |
| **MessageView**    | message_view.go     | 日志详情显示                 | ~200     |
| **Histogram**      | histogram.go        | 时间线直方图                 | ~400     |
| **RowDetailsView** | row_details_view.go | 行详情展示                   | ~150     |
| **UI Utilities**   | ui/                 | 下拉菜单、表格扩展           | ~200     |

**关键功能**:

- ✅ 日志表格展示与滚动
- ✅ AWK过滤模式编辑
- ✅ 时间范围选择
- ✅ SELECT字段表达式编辑
- ✅ 时间线直方图可视化
- ✅ 日志消息详情查看
- ✅ 复制到剪贴板
- ✅ 快捷键和菜单导航

**依赖关系**:

```
MainView
├─ QueryEditView ◄── 查询条件
├─ MessageView ◄── 日志详情
├─ Histogram ◄── 时间统计
├─ History ◄── 历史记录
└─ Options ◄── 配置选项
    └─ Transport (通过Core)
```

**主要API**:

```go
type MainView struct {
    logsTable       // 日志表
    queryInput      // AWK模式输入
    histg           // 直方图
    messageView     // 消息详情
    timeLabel       // 时间范围标签
}

// 核心方法
func (mv *MainView) doQuery(params)  // 执行查询
func (mv *MainView) updateTableHeader(msgs) // 更新表头
func (mv *MainView) setQuery(pattern) // 设置查询模式
func (mv *MainView) setTimeRange(from, to) // 设置时间范围
```

---

### 2️⃣ **Core Layer** - 核心业务逻辑层 ⭐⭐⭐⭐⭐

**位置**: `core/`

**优先级**: 🔴 **CRITICAL** (关键)

**职责**:

- 管理远程日志流连接
- 执行日志查询和过滤
- 时间解析和格式化
- 日志响应的合并和聚合

**主要组件**:

| 组件                 | 文件                 | 功能           | 代码行数 |
| -------------------- | -------------------- | -------------- | -------- |
| **LStreamsManager**  | lstreams_manager.go  | 日志流连接管理 | ~500     |
| **LStreamClient**    | lstream_client.go    | 单个流的客户端 | ~400     |
| **QueryLogsParams**  | core.go              | 查询参数定义   | ~200     |
| **TimeFormat**       | parsing_time.go      | 时间格式解析   | ~300     |
| **LStreamsResolver** | lstreams_resolver.go | 日志流配置解析 | ~250     |
| **AgentScript**      | nerdlog_agent.sh     | 远程执行脚本   | ~2000    |

**关键功能**:

- ✅ SSH/本地连接管理（连接池）
- ✅ 日志查询并发执行
- ✅ AWK模式过滤
- ✅ 时间范围裁剪（使用索引加速）
- ✅ 直方图数据聚合（按分钟统计）
- ✅ 日志消息合并和排序
- ✅ 自定义日志格式支持

**依赖关系**:

```
LStreamsManager
├─ Transport (连接)
├─ LStreamClient
│  ├─ nerdlog_agent.sh (远程脚本)
│  └─ TimeFormat (时间解析)
├─ Config (logstreams.yaml)
└─ LStreamsResolver (配置解析)
```

**关键数据结构**:

```go
// 查询参数
type QueryLogsParams struct {
    MaxNumLines     int       // 最多返回行数
    From, To        time.Time // 时间范围
    Query           string    // AWK过滤表达式
    LoadEarlier     bool      // 是否加载更早的日志
    RefreshIndex    bool      // 是否刷新索引
}

// 查询响应
type LogResp struct {
    MinuteStats map[int64]MinuteStatsItem // 直方图数据
    Logs        []LogMsg                  // 日志消息
    NumMsgsTotal int                      // 总消息数
}

// 日志消息
type LogMsg struct {
    Fields map[string]string // 动态字段
    Time   time.Time        // 时间戳
    // ...
}
```

---

### 3️⃣ **Transport Layer** - 连接传输层 ⭐⭐⭐⭐

**位置**: `core/shell_transport*.go`

**优先级**: 🟠 **HIGH** (高)

**职责**:

- 建立和管理SSH/本地shell连接
- 执行远程命令
- 处理连接错误和重连
- 支持自定义连接命令

**主要组件**:

| 组件                   | 文件                          | 功能          | 代码行数 |
| ---------------------- | ----------------------------- | ------------- | -------- |
| **Transport**          | transport_mode.go             | 传输模式枚举  | ~50      |
| **ShellTransport**     | shell_transport.go            | 通用Shell接口 | ~300     |
| **SSHTransport**       | shell_transport_ssh_lib.go    | SSH实现       | ~400     |
| **LocalTransport**     | shell_transport.go            | 本地执行      | ~150     |
| **CustomCmdTransport** | shell_transport_custom_cmd.go | 自定义命令    | ~200     |

**关键功能**:

- ✅ SSH密钥认证
- ✅ 本地命令执行
- ✅ 自定义连接脚本支持
- ✅ 连接超时管理
- ✅ 错误恢复

**使用场景**:

```
SSH连接       → ssh user@host (标准)
自定义脚本     → teleport ssh (支持Teleport等)
本地执行      → localhost (本机执行)
```

---

### 4️⃣ **Config Layer** - 配置管理层 ⭐⭐⭐⭐

**位置**: `core/config.go`, `cmd/nerdlog/config.go`

**优先级**: 🟠 **HIGH** (高)

**职责**:

- 加载和解析配置文件（logstreams.yaml）
- 命令行参数处理
- 时间格式和选项配置
- 用户选项管理

**主要组件**:

| 组件              | 文件                   | 功能           | 代码行数 |
| ----------------- | ---------------------- | -------------- | -------- |
| **Config**        | core/config.go         | 日志流配置定义 | ~200     |
| **Options**       | cmd/nerdlog/options.go | UI选项管理     | ~300     |
| **OptionsShared** | cmd/nerdlog/options.go | 共享选项       | ~100     |

**关键配置项**:

```yaml
# logstreams.yaml 示例
logstreams:
  - name: app-server
    ssh_host: app.example.com:22
    ssh_user: ubuntu
    paths:
      - /var/log/syslog
      - /var/log/app.log

  - name: db-server
    ssh_host: db.example.com:22
    logstream: "journalctl -u postgres"
```

**命令行参数**:

```
--lstreams localhost,app-*,db-*    # 日志流选择（glob模式）
--pattern '/ERROR/'                # AWK过滤模式
--time -1h                         # 时间范围（相对或绝对）
--selquery 'time, message, level'  # SELECT字段表达式
```

---

### 5️⃣ **History Module** - 历史记录模块 ⭐⭐⭐⭐

**位置**: `internal/history/`, `internal/history_v2/` (新)

**优先级**: 🟡 **MEDIUM** (中等)

**职责**:

- 保存命令行输入历史
- 保存查询历史
- 支持历史导航（前进/后退）
- 去重和持久化

**主要组件**:

| 组件                   | 功能       | 特性                        |
| ---------------------- | ---------- | --------------------------- |
| **CommandLineHistory** | 命令行历史 | 文件持久化、去重、导航      |
| **BrowserLikeHistory** | 查询历史   | 浏览器式前进/后退、内存存储 |

**特性对比**:

| 特性     | CommandLineHistory | BrowserLikeHistory       |
| -------- | ------------------ | ------------------------ |
| 存储     | 文件               | 内存                     |
| 导航     | Prev/Next          | Prev/Next + Forward/Back |
| 去重     | ✅ (跳过重复)      | ❌                       |
| 当前输入 | ✅ (临时项)        | ❌                       |
| 截断     | ❌                 | ✅ (中间添加时)          |

**新实现优势** (history_v2):

- 代码行数: 1255 → 504 (-60%)
- 文件数量: 16 → 2 (-87.5%)
- 学习曲线: 8小时 → 30分钟 (-94%)

---

### 6️⃣ **Utils & Supporting** - 工具和支持模块 ⭐⭐⭐

**位置**: `log/`, `version/`, `clipboard/`, `shellescape/`

**优先级**: 🟡 **MEDIUM** (中等)

**职责**:

- 日志输出（命名空间日志）
- 版本管理
- 剪贴板访问
- Shell命令转义

**主要组件**:

| 组件            | 文件               | 功能         |
| --------------- | ------------------ | ------------ |
| **Logger**      | log/log.go         | 结构化日志   |
| **Version**     | version/version.go | 版本信息     |
| **Clipboard**   | clipboard/         | 跨平台剪贴板 |
| **ShellEscape** | shellescape/       | AWK脚本转义  |

---

## 🔗 模块依赖关系详图

```
                           ┌──────────────┐
                           │  main.go     │ (入口)
                           └────┬─────────┘
                                │
                           ┌────▼────────────────────────────┐
                           │     nerdlogApp                   │
                           │  (应用程序协调)                  │
                           └────┬──────────────┬──────────────┘
                                │              │
                    ┌───────────┴──┐   ┌──────┴──────────┐
                    │              │   │                 │
              ┌─────▼──────┐   ┌───▼───▼──────┐  ┌──────▼─────┐
              │ MainView   │   │ LStreams     │  │ Options    │
              │ (UI)       │   │ Manager      │  │ (Config)   │
              └─────┬──────┘   └────┬────────┘  └──────┬──────┘
                    │               │                  │
            ┌───────┼───────────────┼──────────────────┘
            │       │               │
       ┌────▼───┐ ┌─▼────────────┐ │
       │History │ │ Transport    │ │
       │Module  │ │ (SSH/Local)  │ │
       └────┬───┘ └─┬────────────┘ │
            │       │              │
            │       │ ┌────────────▼───────┐
            │       │ │  nerdlog_agent.sh  │
            │       │ │ (Remote Script)    │
            │       └─┤                    │
            │         │ AWK Filter         │
            │         │ Time Format Parse  │
            │         │ Histogram Generate │
            │         └────────────────────┘
            │
       ┌────▼────────────────────┐
       │  ~/.nerdlog_history     │
       │  ~/.nerdlog_query_hist  │
       │  (持久化)                │
       └─────────────────────────┘
```

---

## ⚡ 功能优先级矩阵

### 按优先级分类

#### 🔴 **P0 - CRITICAL (关键)**

需要正常运行的核心功能

| 功能         | 模块      | 原因         | 依赖           |
| ------------ | --------- | ------------ | -------------- |
| 日志表格显示 | UI        | 核心用户交互 | Core/Transport |
| 日志查询执行 | Core      | 应用核心功能 | Transport      |
| 远程连接管理 | Transport | 无法不要     | Core           |
| 时间范围过滤 | Core      | 核心查询功能 | -              |
| AWK模式过滤  | Core      | 核心查询功能 | -              |
| 直方图绘制   | UI        | 可视化分析   | Core           |
| 查询编辑     | UI        | 用户交互     | -              |
| 时间解析     | Core      | 日志处理基础 | -              |

#### 🟠 **P1 - HIGH (高)**

重要但非关键的功能

| 功能         | 模块      | 原因         | 依赖 |
| ------------ | --------- | ------------ | ---- |
| 命令行历史   | History   | 提升UX       | UI   |
| 查询历史     | History   | 提升UX       | UI   |
| 浏览器式导航 | UI        | 提升UX       | -    |
| 日志详情查看 | UI        | 提升用户体验 | Core |
| 剪贴板复制   | Clipboard | 便捷操作     | -    |
| 自定义连接   | Transport | 灵活部署     | -    |
| 配置文件支持 | Config    | 简化使用     | -    |
| 调试信息面板 | UI        | 故障排查     | Core |

#### 🟡 **P2 - MEDIUM (中等)**

有用但可选的功能

| 功能             | 模块 | 原因         | 依赖 |
| ---------------- | ---- | ------------ | ---- |
| SELECT字段表达式 | UI   | 灵活显示字段 | Core |
| 列编辑视图       | UI   | 高级功能     | -    |
| 日志索引（加速） | Core | 性能优化     | -    |
| 多时间格式支持   | Core | 兼容性       | -    |
| 菜单导航         | UI   | 易用性       | -    |
| 分页加载         | Core | 性能优化     | -    |

#### 🟢 **P3 - LOW (低)**

锦上添花的功能

| 功能     | 模块      | 原因 | 依赖 |
| -------- | --------- | ---- | ---- |
| 版本信息 | Version   | 可选 | -    |
| 颜色主题 | UI        | 可选 | -    |
| 帮助信息 | UI        | 可选 | -    |
| 性能基准 | Benchmark | 可选 | -    |

---

## 📊 功能依赖优先级表

```
      优先级
         │
       P0│  ┌──────────────────────────┐
         │  │ 查询、表显、直方图、连接  │ Critical Path
         │  │ (无这些应用不能用)       │
       P1│  ├──────────────────────────┤
         │  │ 历史、导航、详情、连接   │ 重要特性
         │  │ (没这些体验很差)        │
       P2│  ├──────────────────────────┤
         │  │ SELECT表达式、索引、格式 │ 高级特性
         │  │ (优化和灵活性)          │
       P3│  ├──────────────────────────┤
         │  │ 颜色、帮助、版本等       │ 美化
         │  │ (可选项)                │
         └──┴──────────────────────────┘
```

---

## 🎯 模块之间的控制流

### 典型查询流程

```
1. 用户输入 (UI)
   └─ QueryInput: "/ERROR/"
      └─ TimeInput: "1h"
         └─ QueryEditView: 编辑查询条件

2. 提交查询 (UI → Core)
   └─ MainView.doQuery()
      └─ LStreamsManager.QueryLogs()

3. 远程执行 (Core → Transport → Remote)
   └─ LStreamClient.QueryLogs()
      ├─ Transport.Exec(nerdlog_agent.sh)
      │  └─ AWK过滤、时间裁剪、直方图生成
      └─ 解析响应并返回

4. 显示结果 (Core → UI)
   └─ MainView.updateTableHeader()
      ├─ MainView.logsTable.SetContent()
      ├─ Histogram.SetData()
      └─ UI刷新
```

### 导航流程

```
1. 用户按键 (UI)
   └─ <Alt+Left> / 后退按钮

2. 历史导航 (UI → History)
   └─ QueryHistory.Prev()
      └─ 返回上一个查询

3. 自动重查 (History → Core)
   └─ MainView.doQuery()
      └─ 用历史查询条件重新查询

4. 显示结果 (Core → UI)
   └─ 更新表格和直方图
```

---

## 🏆 关键设计模式

### 1. **模块分离**

- **UI层** 和 **Core层** 分离
- UI层：只负责显示和用户交互
- Core层：只负责查询逻辑

### 2. **异步非阻塞**

- 查询通过回调处理
- UI和Core通过channel通信
- 不阻塞用户界面

### 3. **连接复用**

- Transport层维护连接池
- 避免重复建立连接
- 连接保活和重连管理

### 4. **配置驱动**

- logstreams.yaml 配置日志源
- 支持glob模式动态选择
- 可选：内存模式用于测试

### 5. **远程执行**

- 核心逻辑在 `nerdlog_agent.sh` 中
- 在远程主机执行过滤和分析
- 只传输过滤后的结果
- 节省带宽和时间

---

## 📈 代码规模分析

### 各模块代码量

```
模块               文件数  代码行数  优先级
─────────────────────────────────
UI Layer            25     4000    P0 ⭐⭐⭐⭐⭐
Core Layer          10     3500    P0 ⭐⭐⭐⭐⭐
Remote Script        1     2000    P0 ⭐⭐⭐⭐⭐
Transport           4     1050    P0 ⭐⭐⭐⭐
Config              3      600    P1 ⭐⭐⭐⭐
History (旧)       16     1255    P1 ⭐⭐⭐⭐
History (新)        2      504    P1 ⭐⭐⭐⭐
Utils               4      800    P2 ⭐⭐⭐
─────────────────────────────────
总计               65    ~13700
```

### 架构复杂度评估

| 维度       | 评分 | 说明                     |
| ---------- | ---- | ------------------------ |
| 代码规模   | 中等 | ~13K行，相对紧凑         |
| 模块数     | 低   | 7个主要模块              |
| 分层深度   | 中   | 3层（UI/Core/Transport） |
| 并发复杂度 | 中   | 多连接并发查询           |
| 依赖关系   | 低   | 清晰的单向依赖           |
| 测试覆盖   | 中   | 核心功能有单元测试       |
| 整体复杂度 | 中等 | 可维护，有改进空间       |

---

## 🚀 现有改进机会

### 1. **History模块重构** ✅ 已完成

- 从DDD模式 → 轻量级实现
- 代码减少: 1255 → 504 行 (-60%)
- 文件减少: 16 → 2 个 (-87.5%)
- [详见: history_refactor_report.md](./history_refactor_report.md)

### 2. **Transport模块重构** 📋 建议

- 当前: DDD实现，832行，10个文件
- 建议: 轻量级实现，约250行，2-3个文件
- 预计: -70%代码，-80%复杂度
- [详见: transport_ddd_analysis.md](./transport_ddd_analysis.md)

### 3. **UI模块优化** 🔄 进行中

- MainView 过大 (~1000行)
- 可进一步拆分为更小的组件
- 改进事件处理的清晰度

### 4. **配置管理** 💡 改进空间

- 支持更多日志格式
- 热重载配置
- 配置验证和提示

---

## 📝 总结

### 核心强项

✅ 清晰的分层架构（UI / Core / Transport）
✅ 高效的远程日志查询（agent脚本）
✅ 灵活的连接方式（SSH/Local/Custom）
✅ 合理的配置管理（logstreams.yaml）
✅ 完善的测试覆盖（单元和E2E）

### 改进方向

🔄 优化过度设计的模块（Transport, History）
🔄 减少代码重复和抽象层级
🔄 增强文档和注释
🔄 扩展时间格式和日志类型支持

### 维护建议

1. **重点维护**: UI、Core、Transport 这三个P0模块
2. **定期重构**: 每个主版本进行一次复杂度审查
3. **测试优先**: 新功能必须有相应的测试
4. **文档更新**: 保持架构文档与代码同步
