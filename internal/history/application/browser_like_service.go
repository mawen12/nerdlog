package application

import (
	"github.com/dimonomid/nerdlog/internal/history/domain"
)

// BrowserLikeHistoryService 浏览器式历史用例服务
// 用于临时导航，不持久化
type BrowserLikeHistoryService struct {
	history *domain.BrowserLikeHistory
}

// NewBrowserLikeHistoryService 创建新服务
func NewBrowserLikeHistoryService() *BrowserLikeHistoryService {
	return &BrowserLikeHistoryService{
		history: domain.NewBrowserLikeHistory(),
	}
}

// Add 添加新项
func (s *BrowserLikeHistoryService) Add(req *AddHistoryRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}

	item := domain.NewHistoryItem(req.Content)
	return s.history.Add(item)
}

// GetPrevious 获取上一项
func (s *BrowserLikeHistoryService) GetPrevious() *HistoryItemDTO {
	item := s.history.Prev()
	return NewHistoryItemDTO(item)
}

// GetNext 获取下一项
func (s *BrowserLikeHistoryService) GetNext() *HistoryItemDTO {
	item := s.history.Next()
	return NewHistoryItemDTO(item)
}

// GetCurrent 获取当前项
func (s *BrowserLikeHistoryService) GetCurrent() *HistoryItemDTO {
	item := s.history.Current()
	return NewHistoryItemDTO(item)
}

// ListAll 列出所有项
func (s *BrowserLikeHistoryService) ListAll() (*ListHistoryResponse, error) {
	items := s.history.All()
	dtos := make([]*HistoryItemDTO, len(items))
	for i, item := range items {
		dtos[i] = NewHistoryItemDTO(item)
	}

	return &ListHistoryResponse{
		Items: dtos,
		Total: len(dtos),
	}, nil
}

// Reset 重置导航
func (s *BrowserLikeHistoryService) Reset() {
	s.history.Reset()
}

// Clear 清空历史
func (s *BrowserLikeHistoryService) Clear() {
	s.history.Clear()
}

// Count 获取总数
func (s *BrowserLikeHistoryService) Count() int {
	return s.history.Count()
}
