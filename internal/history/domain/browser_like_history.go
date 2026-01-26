package domain

// BrowserLikeHistory 代表浏览器式历史记录的聚合根
// 支持前进/后退导航，在回退位置新增项时会清除该位置之后的历史
// 仅在内存中维护，不持久化
type BrowserLikeHistory struct {
	items  []*HistoryItem
	curIdx int
}

// NewBrowserLikeHistory 创建新的浏览器式历史
func NewBrowserLikeHistory() *BrowserLikeHistory {
	return &BrowserLikeHistory{
		items:  make([]*HistoryItem, 0),
		curIdx: -1,
	}
}

// Add 添加新项到历史
// 如果当前位置不在最后，则清除当前位置之后的所有项
func (h *BrowserLikeHistory) Add(item *HistoryItem) error {
	if item == nil {
		return ErrNilHistoryItem
	}

	// 如果在历史中回退后又添加新项，则截断该位置之后的所有项
	if len(h.items) > 0 && h.curIdx < len(h.items)-1 {
		h.items = h.items[:h.curIdx+1]
	}

	h.items = append(h.items, item)
	h.curIdx = len(h.items) - 1
	return nil
}

// Prev 返回前一项，若已在开头则返回 nil
func (h *BrowserLikeHistory) Prev() *HistoryItem {
	if h.curIdx <= 0 {
		return nil
	}
	h.curIdx--
	return h.items[h.curIdx]
}

// Next 返回后一项，若已在末尾则返回 nil
func (h *BrowserLikeHistory) Next() *HistoryItem {
	if h.curIdx >= len(h.items)-1 {
		return nil
	}
	h.curIdx++
	return h.items[h.curIdx]
}

// Current 返回当前项
func (h *BrowserLikeHistory) Current() *HistoryItem {
	if h.curIdx < 0 || h.curIdx >= len(h.items) {
		return nil
	}
	return h.items[h.curIdx]
}

// All 返回所有历史项（只读）
func (h *BrowserLikeHistory) All() []*HistoryItem {
	result := make([]*HistoryItem, len(h.items))
	copy(result, h.items)
	return result
}

// Count 返回历史项总数
func (h *BrowserLikeHistory) Count() int {
	return len(h.items)
}

// Reset 重置导航位置到最后
func (h *BrowserLikeHistory) Reset() {
	if len(h.items) > 0 {
		h.curIdx = len(h.items) - 1
	} else {
		h.curIdx = -1
	}
}

// Clear 清空所有历史
func (h *BrowserLikeHistory) Clear() {
	h.items = make([]*HistoryItem, 0)
	h.curIdx = -1
}
