package application

import (
	"fmt"
)

// 应用层错误定义

// ErrEmptyContent 内容为空
var ErrEmptyContent = fmt.Errorf("history content cannot be empty")

// ErrHistoryNotLoaded 历史未加载
var ErrHistoryNotLoaded = fmt.Errorf("history not loaded")

// ErrSaveHistoryFailed 保存失败
type ErrSaveHistoryFailed struct {
	Reason error
}

func (e *ErrSaveHistoryFailed) Error() string {
	return fmt.Sprintf("failed to save history: %v", e.Reason)
}

// ErrLoadHistoryFailed 加载失败
type ErrLoadHistoryFailed struct {
	Reason error
}

func (e *ErrLoadHistoryFailed) Error() string {
	return fmt.Sprintf("failed to load history: %v", e.Reason)
}
