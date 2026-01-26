package domain

// CommandLineHistory 代表命令行历史记录聚合根
// 支持持久化，支持导航时跳过重复项
// 是 CLHistory 的 DDD 版本
type CommandLineHistory struct {
	items             []*HistoryItem
	curHistIdx        int
	lastEphemeralItem *HistoryItem
}

// NewCommandLineHistory 创建新的命令行历史
func NewCommandLineHistory() *CommandLineHistory {
	return &CommandLineHistory{
		items:      make([]*HistoryItem, 0),
		curHistIdx: -1,
	}
}

// Add 添加新项，重置导航状态
func (h *CommandLineHistory) Add(item *HistoryItem) error {
	if item == nil {
		return ErrNilHistoryItem
	}

	h.ResetNavigation()
	h.items = append(h.items, item)
	return nil
}

// Prev 返回前一项，跳过与当前输入相同的项
// 返回值：(item, hasMore)，hasMore 表示是否还有更多历史项
func (h *CommandLineHistory) Prev(currentInput string) (*HistoryItem, bool) {
	if h.curHistIdx == -1 {
		h.startNavigation(currentInput)
	}

	for {
		h.curHistIdx--
		if h.curHistIdx < 0 {
			h.curHistIdx = 0
		}

		hasMore := h.curHistIdx > 0
		item := h.getItem(h.curHistIdx)

		// 跳过与当前输入相同的项，或已到第一项时返回
		if item.Content() != currentInput || !hasMore {
			return item, hasMore
		}
	}
}

// Next 返回后一项，跳过与当前输入相同的项
func (h *CommandLineHistory) Next(currentInput string) (*HistoryItem, bool) {
	if h.curHistIdx == -1 {
		h.startNavigation(currentInput)
	}

	for {
		h.curHistIdx++
		if h.curHistIdx > len(h.items) {
			h.curHistIdx = len(h.items)
		}

		hasMore := h.curHistIdx < len(h.items)
		item := h.getItem(h.curHistIdx)

		if item.Content() != currentInput || !hasMore {
			return item, hasMore
		}
	}
}

// ResetNavigation 重置导航状态（用户编辑或中止/接收命令时调用）
func (h *CommandLineHistory) ResetNavigation() {
	h.curHistIdx = -1
	h.lastEphemeralItem = nil
}

// All 返回所有历史项
func (h *CommandLineHistory) All() []*HistoryItem {
	result := make([]*HistoryItem, len(h.items))
	copy(result, h.items)
	return result
}

// Count 返回历史项总数
func (h *CommandLineHistory) Count() int {
	return len(h.items)
}

// Clear 清空所有历史
func (h *CommandLineHistory) Clear() {
	h.items = make([]*HistoryItem, 0)
	h.curHistIdx = -1
	h.lastEphemeralItem = nil
}

// 私有方法

func (h *CommandLineHistory) startNavigation(currentInput string) {
	h.curHistIdx = len(h.items)
	h.lastEphemeralItem = NewHistoryItem(currentInput)
}

func (h *CommandLineHistory) getItem(idx int) *HistoryItem {
	if idx < 0 {
		return nil
	}
	if idx == len(h.items) {
		// 返回当前临时输入
		return h.lastEphemeralItem
	}
	if idx < len(h.items) {
		return h.items[idx]
	}
	return nil
}
