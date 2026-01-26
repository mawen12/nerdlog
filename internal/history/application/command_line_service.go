package application

import (
	"github.com/dimonomid/nerdlog/internal/history/domain"
)

// CommandLineHistoryService 命令行历史用例服务
type CommandLineHistoryService struct {
	store   domain.HistoryStore
	history *domain.CommandLineHistory
	loaded  bool
}

// NewCommandLineHistoryService 创建新的命令行历史服务
func NewCommandLineHistoryService(store domain.HistoryStore) *CommandLineHistoryService {
	return &CommandLineHistoryService{
		store:   store,
		history: domain.NewCommandLineHistory(),
	}
}

// Load 加载历史记录
func (s *CommandLineHistoryService) Load() error {
	if s.loaded {
		return nil
	}

	history, err := s.store.LoadCommandLineHistory()
	if err != nil {
		return &ErrLoadHistoryFailed{Reason: err}
	}

	s.history = history
	s.loaded = true
	return nil
}

// AddHistory 添加历史项
func (s *CommandLineHistoryService) AddHistory(req *AddHistoryRequest) error {
	if !s.loaded {
		return ErrHistoryNotLoaded
	}

	if err := req.Validate(); err != nil {
		return err
	}

	item := domain.NewHistoryItem(req.Content)
	if err := s.history.Add(item); err != nil {
		return err
	}

	if err := s.store.SaveHistoryItem(item); err != nil {
		return &ErrSaveHistoryFailed{Reason: err}
	}

	return nil
}

// GetPrevious 获取上一条历史（跳过重复）
func (s *CommandLineHistoryService) GetPrevious(currentInput string) *QueryHistoryResponse {
	if !s.loaded {
		return nil
	}

	item, hasMore := s.history.Prev(currentInput)
	if item == nil {
		return nil
	}

	return &QueryHistoryResponse{
		Item:    NewHistoryItemDTO(item),
		HasMore: hasMore,
	}
}

// GetNext 获取下一条历史（跳过重复）
func (s *CommandLineHistoryService) GetNext(currentInput string) *QueryHistoryResponse {
	if !s.loaded {
		return nil
	}

	item, hasMore := s.history.Next(currentInput)
	if item == nil {
		return nil
	}

	return &QueryHistoryResponse{
		Item:    NewHistoryItemDTO(item),
		HasMore: hasMore,
	}
}

// ResetNavigation 重置导航状态
func (s *CommandLineHistoryService) ResetNavigation() error {
	if !s.loaded {
		return ErrHistoryNotLoaded
	}

	s.history.ResetNavigation()
	return nil
}

// ListHistory 列出所有历史项
func (s *CommandLineHistoryService) ListHistory() (*ListHistoryResponse, error) {
	if !s.loaded {
		return nil, ErrHistoryNotLoaded
	}

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

// ClearHistory 清空历史
func (s *CommandLineHistoryService) ClearHistory() error {
	if !s.loaded {
		return ErrHistoryNotLoaded
	}

	s.history.Clear()
	return s.store.ClearHistory()
}

// Count 获取历史项总数
func (s *CommandLineHistoryService) Count() int {
	if !s.loaded {
		return 0
	}
	return s.history.Count()
}
