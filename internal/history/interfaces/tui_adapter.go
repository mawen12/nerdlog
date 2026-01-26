package interfaces

import (
	"github.com/dimonomid/nerdlog/internal/history/application"
)

// BrowserLikeHistoryAdapter 浏览器式历史 TUI 适配器
// 用于 TUI 查询编辑视图中的导航
type BrowserLikeHistoryAdapter struct {
	service *application.BrowserLikeHistoryService
}

// NewBrowserLikeHistoryAdapter 创建新适配器
func NewBrowserLikeHistoryAdapter(service *application.BrowserLikeHistoryService) *BrowserLikeHistoryAdapter {
	return &BrowserLikeHistoryAdapter{
		service: service,
	}
}

// HandleAddQuery 处理添加查询到历史
func (a *BrowserLikeHistoryAdapter) HandleAddQuery(query string) error {
	req := &application.AddHistoryRequest{
		Content: query,
	}
	return a.service.Add(req)
}

// HandlePrevQuery 处理查询历史中的上一项
func (a *BrowserLikeHistoryAdapter) HandlePrevQuery() *application.HistoryItemDTO {
	return a.service.GetPrevious()
}

// HandleNextQuery 处理查询历史中的下一项
func (a *BrowserLikeHistoryAdapter) HandleNextQuery() *application.HistoryItemDTO {
	return a.service.GetNext()
}

// HandleResetQuery 重置导航到当前查询
func (a *BrowserLikeHistoryAdapter) HandleResetQuery() {
	a.service.Reset()
}

// GetCurrentQuery 获取当前查询
func (a *BrowserLikeHistoryAdapter) GetCurrentQuery() *application.HistoryItemDTO {
	return a.service.GetCurrent()
}

// GetQueryCount 获取查询历史总数
func (a *BrowserLikeHistoryAdapter) GetQueryCount() int {
	return a.service.Count()
}

// ClearQueryHistory 清空查询历史
func (a *BrowserLikeHistoryAdapter) ClearQueryHistory() {
	a.service.Clear()
}

// GetAllQueries 获取所有查询历史
func (a *BrowserLikeHistoryAdapter) GetAllQueries() ([]string, error) {
	resp, err := a.service.ListAll()
	if err != nil {
		return nil, err
	}

	if resp == nil || len(resp.Items) == 0 {
		return []string{}, nil
	}

	result := make([]string, len(resp.Items))
	for i, item := range resp.Items {
		result[i] = item.Content
	}
	return result, nil
}

// FormatQueryForDisplay 格式化查询用于 TUI 显示
func (a *BrowserLikeHistoryAdapter) FormatQueryForDisplay(item *application.HistoryItemDTO) string {
	if item == nil {
		return ""
	}
	return item.Content
}
