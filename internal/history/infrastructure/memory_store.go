package infrastructure

import (
	"github.com/dimonomid/nerdlog/internal/history/domain"
)

// InMemoryHistoryStore 基于内存的历史存储实现
// 用于测试或不需要持久化的场景
type InMemoryHistoryStore struct {
	items []*domain.HistoryItem
}

// NewInMemoryHistoryStore 创建内存存储
func NewInMemoryHistoryStore() *InMemoryHistoryStore {
	return &InMemoryHistoryStore{
		items: make([]*domain.HistoryItem, 0),
	}
}

// LoadCommandLineHistory 从内存加载历史
func (s *InMemoryHistoryStore) LoadCommandLineHistory() (*domain.CommandLineHistory, error) {
	history := domain.NewCommandLineHistory()
	for _, item := range s.items {
		_ = history.Add(item)
	}
	return history, nil
}

// SaveHistoryItem 保存项到内存
func (s *InMemoryHistoryStore) SaveHistoryItem(item *domain.HistoryItem) error {
	if item != nil {
		s.items = append(s.items, item)
	}
	return nil
}

// LoadHistoryItems 加载所有项
func (s *InMemoryHistoryStore) LoadHistoryItems() ([]*domain.HistoryItem, error) {
	result := make([]*domain.HistoryItem, len(s.items))
	copy(result, s.items)
	return result, nil
}

// ClearHistory 清空内存
func (s *InMemoryHistoryStore) ClearHistory() error {
	s.items = make([]*domain.HistoryItem, 0)
	return nil
}

// GetItems 获取内部项列表（用于测试）
func (s *InMemoryHistoryStore) GetItems() []*domain.HistoryItem {
	result := make([]*domain.HistoryItem, len(s.items))
	copy(result, s.items)
	return result
}
