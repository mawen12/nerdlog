# Nerdlog 功能依赖关系详细分析

## 📊 功能模块依赖关系矩阵

### 完整依赖关系

```
模块              被依赖    依赖于                 优先级  复杂度
─────────────────────────────────────────────────────────────
UI Layer          App       History,Config,Core   P0     高
├─ MainView       App       ↓                     P0     高
├─ QueryEditView  MainView  ↓                     P0     中
├─ MessageView    MainView  Core                  P1     中
├─ Histogram      MainView  Core                  P0     中
└─ History        UI        Config                P1     低

Core Layer        UI        Transport,Config      P0     高
├─ LStreamsManager App      Transport,Config      P0     高
├─ LStreamClient   Manager   Transport             P0     中
├─ TimeFormat      Client    ↓                     P0     中
├─ QueryParams     Manager   ↓                     P0     低
└─ nerdlog_agent   Client    (Remote)             P0     高

Transport Layer   Core      (OS)                  P0     中
├─ SSHTransport   Manager   crypto                P0     中
├─ LocalTransport Manager   os                    P1     低
└─ CustomCmd      Manager   shell                 P1     中

Config Layer      Core,UI   yaml,file             P1     低
├─ LogStreamCfg   Manager   ↓                     P1     低
└─ Options        UI        ↓                     P1     低

History Module    UI        file,sync              P1     低
├─ CmdLineHist    UI        file                  P1     低
└─ BrowserHist    UI        memory                P1     低

Util Layer        (all)     (std lib)             P2     低
├─ Logger         All       ↓                     P2     低
├─ Clipboard      UI        platform              P2     低
├─ Version        App       ↓                     P3     低
└─ ShellEscape    Core      regex                 P2     低
```

---

## 🎯 按执行路径分析依赖

### 路径1: 用户查询执行路径

```
┌─────────────────────────────────────────────────────────────────┐
│ 用户输入查询条件                        [UI Layer]               │
│ (QueryEditView)                                                 │
└──────────────────────────┬──────────────────────────────────────┘
                           │ 用户提交查询
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│ MainView.doQuery()                      [UI → Core]             │
│ 触发查询回调                                                    │
└──────────────────────────┬──────────────────────────────────────┘
                           │ OnLogQuery()
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│ nerdlogApp.handleQuery()                [App Logic]             │
│ 检查连接和参数有效性                                            │
└──────────────────────────┬──────────────────────────────────────┘
                           │ 调用QueryLogs
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│ LStreamsManager.QueryLogs()             [Core Layer]            │
│ ├─ 检查配置 (Config)                                             │
│ └─ 对每个日志流执行查询                                          │
└──────────────────────────┬──────────────────────────────────────┘
                           │ 对每个流
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│ LStreamClient.QueryLogs()               [Per Stream]            │
│ ├─ 解析时间范围 (TimeFormat)                                     │
│ └─ 构建查询命令和AWK脚本                                         │
└──────────────────────────┬──────────────────────────────────────┘
                           │ 执行远程命令
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│ Transport.Exec()                        [Transport Layer]       │
│ ├─ SSH连接 (SSHTransport)                                        │
│ ├─ 本地执行 (LocalTransport)                                     │
│ └─ 自定义命令 (CustomCmdTransport)                               │
└──────────────────────────┬──────────────────────────────────────┘
                           │ 执行脚本
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│ 远程服务器                              [Remote Host]          │
│ ├─ nerdlog_agent.sh (远程脚本)                                   │
│ │  ├─ 时间过滤 (head/tail)                                      │
│ │  ├─ AWK过滤 (Awk pattern)                                     │
│ │  └─ 直方图生成 (按分钟统计)                                    │
│ └─ 返回: 日志 + 直方图数据                                       │
└──────────────────────────┬──────────────────────────────────────┘
                           │ 响应
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│ LStreamClient 解析响应                  [Core Layer]            │
│ ├─ 解析日志行 (TimeFormat)                                       │
│ └─ 构建LogResp对象                                              │
└──────────────────────────┬──────────────────────────────────────┘
                           │ 合并
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│ LStreamsManager 合并结果                [Core Layer]            │
│ ├─ 合并所有流的日志                                              │
│ ├─ 按时间排序                                                    │
│ └─ 聚合直方图数据                                                │
└──────────────────────────┬──────────────────────────────────────┘
                           │ 返回结果
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│ nerdlogApp 处理结果                     [App Logic]             │
│ └─ 回调 OnLogQuery 返回结果                                      │
└──────────────────────────┬──────────────────────────────────────┘
                           │ 显示结果
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│ MainView 显示结果                       [UI Layer]              │
│ ├─ MainView.logsTable.SetContent()                              │
│ ├─ Histogram.SetData()                                          │
│ └─ UI刷新                                                        │
└─────────────────────────────────────────────────────────────────┘
```

**关键依赖**:

- UI → Core (必须)
- Core → Transport (必须)
- Transport → 远程Host (必须)
- 所有层 → Config (配置)
- 所有层 → Logger (日志)

---

### 路径2: 历史导航路径

```
┌─────────────────────────┐
│ 用户按键: <Alt+Left>    │
│ 或点击Back按钮          │
└────────────┬────────────┘
             │
             ▼
┌─────────────────────────┐
│ MainView.eventHandler() │ [UI]
└────────────┬────────────┘
             │
             ▼
┌─────────────────────────────┐
│ QueryHistory.Prev()         │ [History]
└────────────┬────────────────┘
             │
             ▼
┌────────────────────────────────┐
│ 返回上一个查询条件              │
│ (Query, TimeRange, Pattern)    │
└────────────┬───────────────────┘
             │
             ▼
┌────────────────────────────────┐
│ MainView.applyQueryEditData()   │ [UI]
└────────────┬───────────────────┘
             │
             ▼
┌────────────────────────────────┐
│ MainView.doQuery()             │ [UI → Core]
│ (DontAddHistoryItem = true)    │ 不再添加到历史
└────────────┬───────────────────┘
             │
             ▼
[同 路径1: 用户查询执行]
```

**关键点**:

- History 是独立的，不依赖 Core
- History 只提供前一个查询状态
- Core 执行真实查询
- 导航不添加到历史中 (避免循环)

---

### 路径3: 配置初始化路径

```
┌──────────────────────────────────┐
│ main() 启动                       │
└────────────┬─────────────────────┘
             │
             ▼
┌──────────────────────────────────────┐
│ 读取命令行参数                        │ [App]
│ --lstreams, --pattern, --time 等      │
└────────────┬─────────────────────────┘
             │
             ▼
┌───────────────────────────────────────────────┐
│ 读取配置文件 ~/.config/nerdlog/logstreams.yaml│ [Config]
│ 支持glob模式匹配                              │
└────────────┬────────────────────────────────┘
             │
             ▼
┌──────────────────────────────────┐
│ LStreamsResolver.Resolve()        │ [Core]
│ 解析logstream配置                 │
└────────────┬─────────────────────┘
             │
             ▼
┌──────────────────────────────────┐
│ LStreamsManager 初始化            │ [Core]
│ ├─ 创建Transport连接              │
│ └─ 为每个流建立连接               │
└────────────┬─────────────────────┘
             │
             ▼
┌──────────────────────────────────┐
│ MainView 初始化                   │ [UI]
│ ├─ 创建History模块                │
│ ├─ 加载历史文件                    │
│ └─ 显示初始UI                      │
└──────────────────────────────────┘
```

**关键依赖**:

- Config → LStreamsResolver
- LStreamsResolver → LStreamsManager
- LStreamsManager → Transport
- MainView → History + Config

---

## 🔄 关键依赖关系分析

### 1. **UI ↔ Core 交互**

```
UI Layer                          Core Layer
────────────────────────────────────────────

MainView                        nerdlogApp
  │                               │
  ├─ OnLogQuery ──────────────►  │
  │  (查询条件)                  │
  │                              ▼
  │                        LStreamsManager
  │                              │
  │◄─ 回调返回结果  ──────────  │
  │   (日志 + 直方图)             │
  │
  ├─ onConnect ───────────────►  │
  │  (连接更改)                  │
  │                              ▼
  │                        Transport
  │                              │
  │◄─ 回调连接状态  ────────────│


关键特性:
✓ 异步通信 (callback)
✓ 不阻塞UI
✓ 支持并发查询
✓ 自动重连
```

### 2. **Core 内部依赖**

```
LStreamsManager
    │
    ├─► LStreamClient
    │   ├─► Transport
    │   ├─► TimeFormat
    │   └─► nerdlog_agent.sh
    │
    ├─► Config
    │   └─► logstreams.yaml
    │
    └─► Logger
        └─► 日志输出

强度:
P0: LStreamsManager → Transport (必需)
P0: LStreamClient → TimeFormat (必需)
P1: 所有 → Config (必需)
P1: 所有 → Logger (可选)
```

### 3. **Transport 连接池**

```
Transport
  ├─ SSH 连接池
  │  ├─ 复用连接
  │  └─ 并发限制
  │
  ├─ Local 连接
  │  └─ 直接执行
  │
  └─ Custom Cmd
     └─ 自定义脚本

特点:
✓ 连接复用
✓ 自动重连
✓ 超时管理
✓ 并发控制
```

### 4. **History 独立性**

```
History Module
  ├─ 不依赖 Core
  ├─ 不依赖 Transport
  └─ 只依赖 file I/O

优点:
✓ 可独立测试
✓ 易于实现和维护
✓ 零运行时开销
```

---

## 📈 依赖强度分级

### 强依赖 (Strong Dependency)

```
UI ──必须─► Core
 ↓           ↓
History   Transport
 ↓           ↓
File I/O   SSH/Local

这些依赖不能删除或替换
```

### 中依赖 (Medium Dependency)

```
Core ──应有── Config
All ──应有── Logger
Transport ──应有── CLI参数

这些可以替换但很困难
```

### 弱依赖 (Weak Dependency)

```
UI ──可选── Clipboard
Core ──可选── 特定日志格式
History ──可选── 加密存储

这些可以删除或替换
```

---

## 🎯 优先级分布

### 按优先级画依赖树

```
            ┌─────────┐
            │   App   │ (P0)
            └────┬────┘
                 │
         ┌───────┴───────┐
         │               │
    ┌────▼────┐      ┌───▼──────┐
    │ MainView │      │LStreams  │
    │  (P0)   │      │Manager   │
    │         │      │ (P0)     │
    └────┬────┘      └───┬──────┘
         │                │
    ┌────▼────┐       ┌───▼──────┐
    │ History │       │Transport │
    │ (P1)    │       │ (P0)     │
    │         │       │          │
    └─────────┘       └──────────┘

P0 路径: App → LStreamsManager → Transport
        App → MainView (UI核心)

P1 路径: UI → History
        Config → 所有层

P2 路径: Logger → 所有层
        Clipboard → UI
        ShellEscape → Core
```

### 关键路径分析

```
关键路径 (Critical Path):
1. MainView 显示
2. 用户输入查询
3. Core 执行查询
4. Transport 连接并执行
5. 返回结果并显示

任何一个环节故障都会导致应用无法使用
→ P0 优先级

非关键路径:
- History 保存/加载
- 剪贴板复制
- 调试信息显示
- 颜色主题

这些故障不影响核心功能
→ P1-P3 优先级
```

---

## 🔍 循环依赖检查

### 检查结果

```
✅ 无循环依赖发现

依赖方向统一:
UI ──► Core ──► Transport ──► Remote
 │        │
 └────────┴──────► Config/Logger/Utils

树形结构，无环。
```

---

## 💡 依赖优化建议

### 1. **减少 UI ↔ Core 耦合**

现状:

```go
MainView {
    OnLogQuery  // UI调用Core
    OnLStreamsChange  // UI调用Core
    Options  // UI共享配置
}
```

建议:

```
创建 AppCoordinator 中间层
  ├─ 接收UI事件
  ├─ 调用Core
  └─ 返回结果给UI

优点: 解耦UI和Core
```

### 2. **Config 作为单例**

现状:

```
Config 在 UI 和 Core 中分别创建
```

建议:

```
创建单一 ConfigManager
  ├─ 集中管理配置
  ├─ 支持热重载
  └─ 通知所有使用者
```

### 3. **Transport 连接管理**

现状:

```
每个 LStreamClient 管理自己的连接
```

建议:

```
创建 TransportPool
  ├─ 全局连接池
  ├─ 自动复用和清理
  └─ 统一管理连接生命周期

优点: 更好的资源利用
```

### 4. **History 作为插件**

现状:

```
History 直接在 MainView 中使用
```

建议:

```
保持不变 (History 已经很独立)

但可以:
  ├─ 支持多个 History 实现
  ├─ 支持自定义存储后端
  └─ 使用接口而不是具体类
```

---

## 📊 总体依赖统计

### 依赖关系统计表

```
模块          被依赖次数  依赖数  耦合度
─────────────────────────────────────
App               -        3     低
UI (MainView)     1        3     中
Core              2        2     中
Transport         1        1     低
Config            3        0     低
History           1        1     低
Logger            5        0     低
Clipboard         1        0     低
Version           1        0     低
```

### 平均依赖距离

```
UI → Core: 1 步
Core → Transport: 1 步
UI → Transport: 2 步
全局平均: 1.5 步

越低越好，表示结构越清晰。
```

---

## ✅ 结论

### 强项

✅ **清晰的单向依赖** - 无循环依赖
✅ **良好的分层** - UI/Core/Transport 清晰分离
✅ **模块独立性** - History、Logger 等可独立测试
✅ **合理的优先级** - P0-P3 优先级明确

### 改进空间

🔄 **可以进一步降耦合** - 通过中间层解耦UI和Core
🔄 **Config 管理** - 支持运行时修改和热重载
🔄 **Transport 连接池** - 改进连接复用策略
🔄 **依赖注入** - 更明确的依赖声明

### 总体评估

**依赖关系复杂度**: 中等 (可维护)
**适合优化**: Transport 和 Config 层
**建议行动**: 见上面的优化建议
