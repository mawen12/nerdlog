package interfaces

import (
	"github.com/dimonomid/nerdlog/internal/history/application"
)

// CommandLineHistoryAdapter CLI 命令行历史适配器
// 将 CLI 交互转换为用例服务调用
type CommandLineHistoryAdapter struct {
	service *application.CommandLineHistoryService
}

// NewCommandLineHistoryAdapter 创建新的适配器
func NewCommandLineHistoryAdapter(service *application.CommandLineHistoryService) *CommandLineHistoryAdapter {
	return &CommandLineHistoryAdapter{
		service: service,
	}
}

// HandleAddCommand 处理添加历史命令
// 入参为用户输入的命令行字符串
func (a *CommandLineHistoryAdapter) HandleAddCommand(cmdLine string) error {
	req := &application.AddHistoryRequest{
		Content: cmdLine,
	}
	return a.service.AddHistory(req)
}

// HandlePrevCommand 处理上一条历史命令
// currentInput 为当前在编辑的内容
func (a *CommandLineHistoryAdapter) HandlePrevCommand(currentInput string) *application.HistoryItemDTO {
	resp := a.service.GetPrevious(currentInput)
	if resp == nil {
		return nil
	}
	return resp.Item
}

// HandleNextCommand 处理下一条历史命令
func (a *CommandLineHistoryAdapter) HandleNextCommand(currentInput string) *application.HistoryItemDTO {
	resp := a.service.GetNext(currentInput)
	if resp == nil {
		return nil
	}
	return resp.Item
}

// HandleResetNavigation 处理重置导航（用户编辑时）
func (a *CommandLineHistoryAdapter) HandleResetNavigation() error {
	return a.service.ResetNavigation()
}

// HandleListCommand 处理列表历史命令
func (a *CommandLineHistoryAdapter) HandleListCommand() (*application.ListHistoryResponse, error) {
	return a.service.ListHistory()
}

// HandleClearCommand 处理清空历史命令
func (a *CommandLineHistoryAdapter) HandleClearCommand() error {
	return a.service.ClearHistory()
}

// FormatHistoryItem 格式化历史项用于显示
func (a *CommandLineHistoryAdapter) FormatHistoryItem(item *application.HistoryItemDTO) string {
	if item == nil {
		return ""
	}
	return item.Content
}

// FormatHistoryList 格式化历史列表用于显示
func (a *CommandLineHistoryAdapter) FormatHistoryList(resp *application.ListHistoryResponse) []string {
	if resp == nil || len(resp.Items) == 0 {
		return []string{}
	}

	result := make([]string, len(resp.Items))
	for i, item := range resp.Items {
		result[i] = item.Content
	}
	return result
}
