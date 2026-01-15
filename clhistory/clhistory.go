package clhistory

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/juju/errors"
)

type CLHistory struct {
	// 参数，保存了文件名称
	params CLHistoryParams

	// 历史记录
	items []Item

	// curHistIdx is used when navigating the history using Prev / Next.
	// When navigating isn't in progress (after a new item was added using Add),
	// it's reset to -1.
	// 用于通过Prev / Next 导航历史记录，当导航操作未进行时，即被设置为-1
	curHistIdx int
	// 最后操作的元素
	lastEphemeralItem Item
}

type CLHistoryParams struct {
	// Filename is where to load the history from and write it to.  If it's
	// empty, the history is only kept in RAM and not persisted anywhere.
	Filename string
}

type Item struct {
	Time time.Time

	Str string
}

func New(params CLHistoryParams) (*CLHistory, error) {
	h := &CLHistory{
		params: params,

		curHistIdx: -1,
	}

	if err := h.Load(); err != nil && !os.IsNotExist(errors.Cause(err)) {
		return nil, errors.Trace(err)
	}

	return h, nil
}

// Load loads all history from the file. If Filename in params is empty,
// Load is a no-op.
// Load 从文件中读取所有历史记录，如果文件名称未指定，则加载为空操作
func (h *CLHistory) Load() error {
	// 文件名称未指定，直接退出
	if h.params.Filename == "" {
		return nil
	}

	// 打开文件
	f, err := os.Open(h.params.Filename)
	if err != nil {
		return errors.Trace(err)
	}

	// 创建历史记录解码器
	decoder := NewHistoryDecoder(f)
	// 解码历史记录
	loadedItems, err := decoder.Decode()
	if err != nil {
		return errors.Trace(err)
	}

	h.items = loadedItems

	h.resetHistoryNavigation()

	return nil
}

// Add adds the given string as a new history item to the in-RAM history and,
// if Filename in params was not empty, then also to this file. It also resets
// the history navigation, if any.
func (h *CLHistory) Add(s string) error {
	h.resetHistoryNavigation()

	item := Item{
		Time: time.Now(),
		Str:  s,
	}

	h.items = append(h.items, item)

	if h.params.Filename != "" {
		f, err := os.OpenFile(h.params.Filename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			return errors.Trace(err)
		}

		defer f.Close()

		fmt.Fprint(f, string(marshalItem(item)))
	}

	return nil
}

// Reset resets the history navigation. Typically client code should call it
// when a user edits or aborts/accepts the command line.
func (h *CLHistory) Reset() {
	h.resetHistoryNavigation()
}

// Prev returns what it considers the previous item.
func (h *CLHistory) Prev(s string) (item Item, hasMore bool) {
	if h.curHistIdx == -1 {
		h.startHistoryNavigation(s)
	}

	for {
		h.curHistIdx--
		if h.curHistIdx < 0 {
			h.curHistIdx = 0
		}

		hasMore := h.curHistIdx > 0

		item := h.getItem(h.curHistIdx)
		if item.Str != s || !hasMore {
			return item, hasMore
		}
	}
}

// Next returns what it considers the next item.
func (h *CLHistory) Next(s string) (item Item, hasMore bool) {
	if h.curHistIdx == -1 {
		h.startHistoryNavigation(s)
	}

	for {
		h.curHistIdx++
		if h.curHistIdx > len(h.items) {
			// We do allow it to exceed the data by 1 item, which means just returning
			// lastEphemeralItem; thus we use len(h.items) and not len(h.items)-1.
			h.curHistIdx = len(h.items)
		}

		hasMore := h.curHistIdx < len(h.items)

		item := h.getItem(h.curHistIdx)
		if item.Str != s || !hasMore {
			return item, hasMore
		}
	}
}

func (h *CLHistory) startHistoryNavigation(s string) {
	h.curHistIdx = len(h.items)
	h.lastEphemeralItem = Item{Str: s}
}

// 重置导航进度
func (h *CLHistory) resetHistoryNavigation() {
	h.curHistIdx = -1
	h.lastEphemeralItem = Item{}
}

func (h *CLHistory) getItem(idx int) Item {
	if idx < len(h.items) {
		return h.items[idx]
	}

	if idx == len(h.items) {
		return h.lastEphemeralItem
	}

	panic(fmt.Sprintf("idx=%d, len(items)=%d", idx, len(h.items)))
}

// :1650712458000000000:12:0:foo bar baz
func marshalItem(item Item) []byte {
	b := bytes.Buffer{}
	b.WriteRune(':')
	b.WriteString(strconv.FormatInt(item.Time.UnixNano(), 10))
	b.WriteRune(':')
	b.WriteString(strconv.Itoa(len(item.Str)))
	b.WriteString(":0:") // For now, no extra info
	b.WriteString(item.Str)
	b.WriteRune('\n')

	return b.Bytes()
}

type HistoryDecoder struct {
	r  io.Reader
	br *bufio.Reader
}

func NewHistoryDecoder(r io.Reader) *HistoryDecoder {
	return &HistoryDecoder{
		r:  r,
		br: bufio.NewReader(r),
	}
}

func (hd *HistoryDecoder) Decode() ([]Item, error) {
	var items []Item

	for i := 0; ; i++ {
		item, err := hd.readNextItem()
		if err != nil {
			if errors.Cause(err) == io.EOF {
				break
			}

			return nil, errors.Annotatef(err, "%dth item", i)
		}

		items = append(items, item)
	}

	return items, nil
}

func (hd *HistoryDecoder) readNextItem() (Item, error) {
	//br := bufio.NewReader(r)
	//br.ReadSlice(':')

	if err := hd.consumeByte(':'); err != nil {
		if errors.Cause(err) == io.EOF {
			return Item{}, io.EOF
		}

		return Item{}, errors.Annotatef(err, "reading initial colon")
	}

	chunk, err := hd.br.ReadBytes(':')
	if err != nil {
		if errors.Cause(err) == io.EOF {
			err = io.ErrUnexpectedEOF
		}

		return Item{}, errors.Annotatef(err, "reading timestamp")
	}

	nanos, err := strconv.ParseInt(string(chunk[:len(chunk)-1]), 10, 64)
	if err != nil {
		return Item{}, errors.Annotatef(err, "parsing timestamp")
	}

	chunk, err = hd.br.ReadBytes(':')
	if err != nil {
		if errors.Cause(err) == io.EOF {
			err = io.ErrUnexpectedEOF
		}

		return Item{}, errors.Annotatef(err, "reading timestamp")
	}

	lenData, err := strconv.Atoi(string(chunk[:len(chunk)-1]))
	if err != nil {
		return Item{}, errors.Annotatef(err, "parsing data length")
	}

	chunk, err = hd.br.ReadBytes(':')
	if err != nil {
		if errors.Cause(err) == io.EOF {
			err = io.ErrUnexpectedEOF
		}

		return Item{}, errors.Annotatef(err, "reading timestamp")
	}

	lenExtra, err := strconv.Atoi(string(chunk[:len(chunk)-1]))
	if err != nil {
		return Item{}, errors.Annotatef(err, "parsing extra length")
	}

	if lenExtra > 0 {
		dataIgnored := make([]byte, lenExtra)
		_, err := io.ReadFull(hd.br, dataIgnored)
		if err != nil {
			if errors.Cause(err) == io.EOF {
				err = io.ErrUnexpectedEOF
			}

			return Item{}, errors.Annotatef(err, "reading extra data")
		}
	}

	var data []byte

	if lenData > 0 {
		data = make([]byte, lenData)
		_, err := io.ReadFull(hd.br, data)
		if err != nil {
			if errors.Cause(err) == io.EOF {
				err = io.ErrUnexpectedEOF
			}

			return Item{}, errors.Annotatef(err, "reading data")
		}
	}

	if err := hd.consumeByte('\n'); err != nil {
		if errors.Cause(err) == io.EOF {
			err = io.ErrUnexpectedEOF
		}

		return Item{}, errors.Annotatef(err, "reading final newline")
	}

	return Item{
		Time: time.Unix(0, nanos),
		Str:  string(data),
	}, nil
}

func (hd *HistoryDecoder) consumeByte(want byte) error {
	b := make([]byte, 1)
	_, err := io.ReadFull(hd.br, b)
	if err != nil {
		return errors.Trace(err)
	}

	if b[0] != want {
		return errors.Errorf("expected to read %v, but read %v", want, b[0])
	}

	return nil
}
