package domain

import "fmt"

// 定义领域错误

// ErrNilHistoryItem 当尝试添加 nil 项时返回
var ErrNilHistoryItem = fmt.Errorf("history item cannot be nil")

// ErrInvalidIndex 索引超出范围
type ErrInvalidIndex struct {
	Index int
	Count int
}

func (e *ErrInvalidIndex) Error() string {
	return fmt.Sprintf("invalid history index: %d, count: %d", e.Index, e.Count)
}

// ErrHistoryEmpty 历史为空
var ErrHistoryEmpty = fmt.Errorf("history is empty")
