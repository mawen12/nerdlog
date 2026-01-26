package domain

import (
	"time"
)

// HistoryItem 代表单条历史记录的值对象
// 它是不可变的，一旦创建就不能修改
type HistoryItem struct {
	timestamp time.Time
	content   string
}

// NewHistoryItem 创建新的历史记录项
func NewHistoryItem(content string) *HistoryItem {
	if content == "" {
		return nil
	}
	return &HistoryItem{
		timestamp: time.Now(),
		content:   content,
	}
}

// NewHistoryItemWithTime 为测试或恢复用途创建带有指定时间的项
func NewHistoryItemWithTime(timestamp time.Time, content string) *HistoryItem {
	if content == "" {
		return nil
	}
	return &HistoryItem{
		timestamp: timestamp,
		content:   content,
	}
}

// Timestamp 返回项的创建时间
func (h *HistoryItem) Timestamp() time.Time {
	return h.timestamp
}

// Content 返回历史内容
func (h *HistoryItem) Content() string {
	return h.content
}

// Equals 比较两个历史项是否相同（基于内容）
func (h *HistoryItem) Equals(other *HistoryItem) bool {
	if h == nil || other == nil {
		return h == other
	}
	return h.content == other.content
}

// String 返回项的字符串表示
func (h *HistoryItem) String() string {
	return h.content
}
