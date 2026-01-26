package interfaces

import (
	"github.com/dimonomid/nerdlog/internal/history/application"
)

// HistoryModule 历史记录模块的外观（Facade）
// 向上层 (cmd/nerdlog) 提供简化的接口
type HistoryModule struct {
	cmdLineAdapter *CommandLineHistoryAdapter
	queryAdapter   *BrowserLikeHistoryAdapter
}

// NewHistoryModule 创建历史记录模块
// 使用文件作为命令行历史的持久化存储
func NewHistoryModule(cmdLineHistoryFile string) (*HistoryModule, error) {
	factory := NewHistoryFactoryWithFile(cmdLineHistoryFile)

	cmdLineAdapter, err := factory.CreateCommandLineHistoryAdapter()
	if err != nil {
		return nil, err
	}

	queryAdapter := factory.CreateBrowserLikeHistoryAdapter()

	return &HistoryModule{
		cmdLineAdapter: cmdLineAdapter,
		queryAdapter:   queryAdapter,
	}, nil
}

// NewHistoryModuleInMemory 创建内存版本（仅用于测试）
func NewHistoryModuleInMemory() *HistoryModule {
	factory := NewHistoryFactoryInMemory()
	cmdLineAdapter, _ := factory.CreateCommandLineHistoryAdapter()
	queryAdapter := factory.CreateBrowserLikeHistoryAdapter()

	return &HistoryModule{
		cmdLineAdapter: cmdLineAdapter,
		queryAdapter:   queryAdapter,
	}
}

// ========== 命令行历史 API ==========

// AddCmdLineHistory 添加命令行到历史
func (m *HistoryModule) AddCmdLineHistory(cmdLine string) error {
	return m.cmdLineAdapter.HandleAddCommand(cmdLine)
}

// GetPrevCmdLine 获取上一条命令行
func (m *HistoryModule) GetPrevCmdLine(currentInput string) *application.HistoryItemDTO {
	return m.cmdLineAdapter.HandlePrevCommand(currentInput)
}

// GetNextCmdLine 获取下一条命令行
func (m *HistoryModule) GetNextCmdLine(currentInput string) *application.HistoryItemDTO {
	return m.cmdLineAdapter.HandleNextCommand(currentInput)
}

// ResetCmdLineNavigation 重置命令行导航（用户编辑时调用）
func (m *HistoryModule) ResetCmdLineNavigation() error {
	return m.cmdLineAdapter.HandleResetNavigation()
}

// ListCmdLineHistory 列出所有命令行历史
func (m *HistoryModule) ListCmdLineHistory() (*application.ListHistoryResponse, error) {
	return m.cmdLineAdapter.HandleListCommand()
}

// ClearCmdLineHistory 清空命令行历史
func (m *HistoryModule) ClearCmdLineHistory() error {
	return m.cmdLineAdapter.HandleClearCommand()
}

// CmdLineCount 获取命令行历史条数
func (m *HistoryModule) CmdLineCount() int {
	return m.cmdLineAdapter.service.Count()
}

// ========== 查询历史 API (浏览器式，会话内) ==========

// AddQueryHistory 添加查询到历史
func (m *HistoryModule) AddQueryHistory(query string) error {
	return m.queryAdapter.HandleAddQuery(query)
}

// GetPrevQuery 获取上一个查询
func (m *HistoryModule) GetPrevQuery() *application.HistoryItemDTO {
	return m.queryAdapter.HandlePrevQuery()
}

// GetNextQuery 获取下一个查询
func (m *HistoryModule) GetNextQuery() *application.HistoryItemDTO {
	return m.queryAdapter.HandleNextQuery()
}

// GetCurrentQuery 获取当前查询
func (m *HistoryModule) GetCurrentQuery() *application.HistoryItemDTO {
	return m.queryAdapter.GetCurrentQuery()
}

// ResetQueryNavigation 重置查询导航
func (m *HistoryModule) ResetQueryNavigation() {
	m.queryAdapter.HandleResetQuery()
}

// ListQueryHistory 列出所有查询历史
func (m *HistoryModule) ListQueryHistory() ([]string, error) {
	return m.queryAdapter.GetAllQueries()
}

// ClearQueryHistory 清空查询历史
func (m *HistoryModule) ClearQueryHistory() {
	m.queryAdapter.ClearQueryHistory()
}

// QueryCount 获取查询历史条数
func (m *HistoryModule) QueryCount() int {
	return m.queryAdapter.GetQueryCount()
}

// ========== 格式化输出 ==========

// FormatCmdLineItem 格式化命令行项用于显示
func (m *HistoryModule) FormatCmdLineItem(item *application.HistoryItemDTO) string {
	return m.cmdLineAdapter.FormatHistoryItem(item)
}

// FormatQueryItem 格式化查询项用于显示
func (m *HistoryModule) FormatQueryItem(item *application.HistoryItemDTO) string {
	return m.queryAdapter.FormatQueryForDisplay(item)
}

// ========== 内部访问（高级用法）==========

// GetCmdLineService 获取底层服务（用于自定义操作）
func (m *HistoryModule) GetCmdLineService() *application.CommandLineHistoryService {
	return m.cmdLineAdapter.service
}

// GetQueryService 获取查询服务
func (m *HistoryModule) GetQueryService() *application.BrowserLikeHistoryService {
	return m.queryAdapter.service
}
