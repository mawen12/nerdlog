package application

import (
	"time"

	"github.com/dimonomid/nerdlog/internal/history/domain"
)

// HistoryItemDTO 历史项的数据传输对象
type HistoryItemDTO struct {
	Timestamp time.Time `json:"timestamp"`
	Content   string    `json:"content"`
}

// NewHistoryItemDTO 从领域对象转换
func NewHistoryItemDTO(item *domain.HistoryItem) *HistoryItemDTO {
	if item == nil {
		return nil
	}
	return &HistoryItemDTO{
		Timestamp: item.Timestamp(),
		Content:   item.Content(),
	}
}

// ToHistoryItem 转换为领域对象
func (dto *HistoryItemDTO) ToHistoryItem() *domain.HistoryItem {
	if dto == nil {
		return nil
	}
	return domain.NewHistoryItemWithTime(dto.Timestamp, dto.Content)
}

// AddHistoryRequest 添加历史项的请求
type AddHistoryRequest struct {
	Content string
}

// Validate 验证请求
func (req *AddHistoryRequest) Validate() error {
	if req.Content == "" {
		return ErrEmptyContent
	}
	return nil
}

// ListHistoryResponse 列出历史项的响应
type ListHistoryResponse struct {
	Items []*HistoryItemDTO
	Total int
}

// QueryHistoryResponse 查询历史项的响应
type QueryHistoryResponse struct {
	Item    *HistoryItemDTO
	HasMore bool
}
