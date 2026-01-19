package blhistory

import (
	"time"
)

// BLHistory is a browser-like history: we can add more items to the history;
// we can go back and forth; when we're a few items back and we add a new item,
// a new item is added at this place in the history and all the previously existing
// newer items are dropped; there is no persistence.
//
// A cache can be added to every history item too, but that's a TODO.
type BLHistory struct {
	items []Item

	curIdx int
}

type Item struct {
	Time time.Time

	Str string
}

func New() *BLHistory {
	h := &BLHistory{}

	return h
}

func (h *BLHistory) Add(s string) {
	item := Item{
		Time: time.Now(),
		Str:  s,
	}

	// 当用户在历史中回退后 (curIdx < len(items)-1) 再添加新项时
	// - 截断当前位置之后的所有历史记录
	// - 这模拟浏览器行为：从历史页面中点击新链接时，前进按钮的历史会被清除
	// - 假设历史为：[A, B, C, D]，当前索引为1 (B), 添加新项E后，历史变为 [A, B, E],截断 2 之后的历史
	if len(h.items) > 0 && h.curIdx < len(h.items)-1 {
		h.items = h.items[:h.curIdx+1]
	}

	h.items = append(h.items, item)
	h.curIdx = len(h.items) - 1
}

func (h *BLHistory) Prev() *Item {
	if h.curIdx == 0 {
		return nil
	}

	h.curIdx--

	item := h.items[h.curIdx]
	return &item
}

func (h *BLHistory) Next() *Item {
	if h.curIdx >= len(h.items)-1 {
		return nil
	}

	h.curIdx++

	item := h.items[h.curIdx]
	return &item
}
