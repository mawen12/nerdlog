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

// :1768526631165763989:136:0:nerdlog --lstreams 'localhost:22:/home/mawen/logs/monitor.log' --time -1h --pattern /port/ --selquery 'time STICKY, message, lstream, *'
// 其中记录了用于键入的命令行查询参数，组成如下：
// - --lstreams 查询目标
// - --time 查询的时间/时间范围
// - --pattern awk pattern
// - --selquery 用于查询表的核心条件
// 上述四要素定义了一个合法 nerdlog query 的基础

// 此处遵行历史记录的特性：历史是只读，不可修改的，因此需要支持一下功能：
// - Add 用于添加记录
// - Next 查询下一条历史记录，默认开始导航的是最近一条数据
// - Prev 查询上一条历史记录

// 由于记录的格式和结构无法直接对应，对于读取的记录手动解析映射，此处使用了 HistoryDecoder 来协助。
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

// 命令历史参数，核心指向了文件名，如果未指定文件名，其将保留在内存中，不会持久化
type CLHistoryParams struct {
	// Filename is where to load the history from and write it to.  If it's
	// empty, the history is only kept in RAM and not persisted anywhere.
	Filename string
}

// 代表了单条历史记录，组成为：time + str
// 存储在文件上的标准格式为：:<timestamp>:<len-data>:<len-extra><data-extra>:<data>\n
// 示例数据：:1768526631165763989:136:0:nerdlog --lstreams 'localhost:22:/home/mawen/logs/monitor.log' --time -1h --pattern /port/ --selquery 'time STICKY, message, lstream, *'
// 其中 data 部分，即 nerdlog --lstreams 'localhost:22:/home/mawen/logs/monitor.log' --time -1h --pattern /port/ --selquery 'time STICKY, message, lstream, *'
// 刚好是启动 nerdlog 时的标准命令，其底层对应为：QueryFull
type Item struct {
	Time time.Time

	Str string
}

func New(params CLHistoryParams) (*CLHistory, error) {
	// 设置参数，重置历史记录指针
	h := &CLHistory{
		params: params,

		curHistIdx: -1,
	}

	// 加载历史记录，对于文件不存在的情况，进行忽略；对于文件无法被正确读取的请求/或者因为权限无法读取等情况，中断处理
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

	// 打开文件，失败时直接退出，可能有：文件不存在，无读取权限
	f, err := os.Open(h.params.Filename)
	if err != nil {
		return errors.Trace(err)
	}

	// 创建历史记录解码器，以便一次性加载到内存中
	decoder := NewHistoryDecoder(f)
	// 解码历史记录
	loadedItems, err := decoder.Decode()
	if err != nil {
		return errors.Trace(err)
	}

	// 重置历史导航
	h.items = loadedItems

	// 初始化导航进度，此时没有触发导航，因此其指向的位置为-1
	h.resetHistoryNavigation()

	return nil
}

// Add adds the given string as a new history item to the in-RAM history and,
// if Filename in params was not empty, then also to this file. It also resets
// the history navigation, if any.
// 将给定的字符串添加到历史记录中，
func (h *CLHistory) Add(s string) error {
	// 重置历史导航，因为历史记录发生了变更
	h.resetHistoryNavigation()

	// 设置时间，转换为历史记录
	item := Item{
		Time: time.Now(),
		Str:  s,
	}

	// 追加元素
	h.items = append(h.items, item)

	// 当文件不为空时，同步持久化
	if h.params.Filename != "" {
		// 以 WRONLY|CREATE|APPEND 模式，且当文件不存在时，以0644权限创建文件
		f, err := os.OpenFile(h.params.Filename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			return errors.Trace(err)
		}

		defer f.Close()

		// 将元素转换为string，并写入文件
		fmt.Fprint(f, string(marshalItem(item)))
	}

	return nil
}

// Reset resets the history navigation. Typically client code should call it
// when a user edits or aborts/accepts the command line.
// 重置历史记录导航，通常在用户编辑、中止/接收命令行时
func (h *CLHistory) Reset() {
	h.resetHistoryNavigation()
}

// Prev returns what it considers the previous item.
// 返回其认为的前一个项目，需要注意的时，其对于参数的特殊处理，即跳过重复数据
func (h *CLHistory) Prev(s string) (item Item, hasMore bool) {
	// 当处于未导航时，则触发开始历史导航操作，即从末尾开始往前走
	if h.curHistIdx == -1 {
		h.startHistoryNavigation(s)
	}

	for {
		// 减去一位，首次触发时，索引会指向最后一个元素
		h.curHistIdx--
		// 如果小于0,则修正为0
		if h.curHistIdx < 0 {
			h.curHistIdx = 0
		}

		// 之前是否具有更多的元素
		hasMore := h.curHistIdx > 0

		// 获取该索引位置的元素
		item := h.getItem(h.curHistIdx)
		// 当元素不一致时（跳过重复数据），或者已经是第一行是，则返回
		if item.Str != s || !hasMore {
			return item, hasMore
		}
	}
}

// Next returns what it considers the next item.
func (h *CLHistory) Next(s string) (item Item, hasMore bool) {
	// 当处于未导航时，则触发开始历史导航操作，即从末尾开始往前走
	if h.curHistIdx == -1 {
		h.startHistoryNavigation(s)
	}

	for {
		// 加上一位，首次触发时，索引会指向最后一个元素
		h.curHistIdx++
		// 当超过了元素数量，则进行修正，此时返回的元素是 lastEphemeralItem
		if h.curHistIdx > len(h.items) {
			// We do allow it to exceed the data by 1 item, which means just returning
			// lastEphemeralItem; thus we use len(h.items) and not len(h.items)-1.
			h.curHistIdx = len(h.items)
		}

		// 之后是否具有更多的元素
		hasMore := h.curHistIdx < len(h.items)

		// 获取该索引位置的元素
		item := h.getItem(h.curHistIdx)
		// 当元素不一致时（跳过重复数据），或者已经是最后一行是，则返回
		if item.Str != s || !hasMore {
			return item, hasMore
		}
	}
}

// 开始历史导航,导航的指针指向元素记录长度，这对数组来说是一个越界的值，内容则为指定的字符串
func (h *CLHistory) startHistoryNavigation(s string) {
	h.curHistIdx = len(h.items)
	h.lastEphemeralItem = Item{Str: s}
}

// 重置导航进度
func (h *CLHistory) resetHistoryNavigation() {
	h.curHistIdx = -1
	h.lastEphemeralItem = Item{}
}

// 获取指定索引的元素
func (h *CLHistory) getItem(idx int) Item {
	// 如何是合法索引，则直接返回
	if idx < len(h.items) {
		return h.items[idx]
	}

	// 末尾的索引，则返回临时元素，适用于 Next 场景
	if idx == len(h.items) {
		return h.lastEphemeralItem
	}

	// 否则触发 panic，报告访问的索引和元素的实际数量
	panic(fmt.Sprintf("idx=%d, len(items)=%d", idx, len(h.items)))
}

// :1650712458000000000:12:0:foo bar baz
// 将元素编码为字节数组
func marshalItem(item Item) []byte {
	// 初始化一个字节缓冲区
	b := bytes.Buffer{}
	// 写入:
	b.WriteRune(':')
	// 以十进制方式，将时间的毫秒表示为字符串
	b.WriteString(strconv.FormatInt(item.Time.UnixNano(), 10))
	// 写入:
	b.WriteRune(':')
	// 写入 str 的长度
	b.WriteString(strconv.Itoa(len(item.Str)))
	// 写入 :0:，0 代表 length of extra，即没有额外信息
	b.WriteString(":0:") // For now, no extra info
	// 写入 str 的内容
	b.WriteString(item.Str)
	// 写入换行符
	b.WriteRune('\n')

	// 返回字节数组
	return b.Bytes()
}

// 历史记录解码器，直接 io.Reader，并且底层使用缓冲区来高效读取
type HistoryDecoder struct {
	// 代表了要读取的文件 Reader
	r io.Reader
	// 代表读取时使用的缓冲区，缓冲区大小默认为：4096
	br *bufio.Reader
}

func NewHistoryDecoder(r io.Reader) *HistoryDecoder {
	return &HistoryDecoder{
		r:  r,
		br: bufio.NewReader(r),
	}
}

// 从 Reader 中解码历史记录集
func (hd *HistoryDecoder) Decode() ([]Item, error) {
	var items []Item

	for i := 0; ; i++ {
		// 读取一条记录
		item, err := hd.readNextItem()
		if err != nil {
			// 当读取到末尾时，跳出循环
			if errors.Cause(err) == io.EOF {
				break
			}

			// 对于其他错误，抛出异常，提示读取到第几条数据错误
			// 该方法可以追加错误上下文，例如 err := fmt.Errorf("reading timestamp")
			// 最终错误信息为：10th item: reading timestamp
			return nil, errors.Annotatef(err, "%dth item", i)
		}

		// 写入 slice
		items = append(items, item)
	}

	return items, nil
}

// 依次读取单个记录
// 单条记录合法格式：
// - :<timestamp>:<len-data>:<len-extra>:<data>\n
// - :<timestamp>:<len-data>:<len-extra><data-extra>:<data>\n
// - EOF
func (hd *HistoryDecoder) readNextItem() (Item, error) {
	//br := bufio.NewReader(r)
	//br.ReadSlice(':')

	// :1768526631165763989:136:0:nerdlog --lstreams 'localhost:22:/home/mawen/logs/monitor.log' --time -1h --pattern /port/ --selquery 'time STICKY, message, lstream, *'
	// 开始读取首个字节，预期为:，因为记录以:开头
	if err := hd.consumeByte(':'); err != nil {
		// 当发生读到末尾的时候，直接返回，不再读取，因为这是新的一行，所以该错误是正常的
		if errors.Cause(err) == io.EOF {
			return Item{}, io.EOF
		}

		// 包装错误，返回阅读初始冒号的错误
		return Item{}, errors.Annotatef(err, "reading initial colon")
	}

	// 继续往后，直到读取到下一个:,以合法数据为例，读取到的值为：1768526631165763989:
	chunk, err := hd.br.ReadBytes(':')
	if err != nil {
		// 此时发现已经没有了，代表该条数据格式存在问题
		if errors.Cause(err) == io.EOF {
			err = io.ErrUnexpectedEOF
		}

		// 包装错误，返回读取 timestamp 错误
		return Item{}, errors.Annotatef(err, "reading timestamp")
	}

	// 去除最后一位:，然后按十进制解析成一个64为有符号整数，之所以使用base=10,
	// 是因为记录的时候采用了10进制，读取的时候也需要采用对应的进制
	nanos, err := strconv.ParseInt(string(chunk[:len(chunk)-1]), 10, 64)
	if err != nil {
		// 格式错误
		return Item{}, errors.Annotatef(err, "parsing timestamp")
	}

	// 继续往后，直到读取到下一个:，以合法数据为例，读取到的值为：136:
	chunk, err = hd.br.ReadBytes(':')
	if err != nil {
		// 此时发现已经没有了，代表该条数据格式存在问题
		if errors.Cause(err) == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		// 包装错误，返回读取 timestamp 错误
		// TODO Fix，此处读取的是 data length，而非 timestamp
		return Item{}, errors.Annotatef(err, "reading timestamp")
	}

	// 去除最后一位:,然后将字符串解析为10进制整数，返回int范围内的整数，（32位或64位，取决于平台）
	lenData, err := strconv.Atoi(string(chunk[:len(chunk)-1]))
	if err != nil {
		// 格式错误
		return Item{}, errors.Annotatef(err, "parsing data length")
	}

	// 继续往后，直到读取到下一个:,以合法数据为例，读取到的值为：0:
	chunk, err = hd.br.ReadBytes(':')
	if err != nil {
		// 此时发现已经没有了，代表该条数据格式存在问题
		if errors.Cause(err) == io.EOF {
			err = io.ErrUnexpectedEOF
		}

		// 包装错误，返回读取 timestamp 错误
		// TODO Fix，此处读取的是 extra，而非 timestamp
		return Item{}, errors.Annotatef(err, "reading timestamp")
	}

	// 去除最后一位:,然后将字符串解析为10进制整数，返回int范围内的整数，（32位或64位，取决于平台）
	lenExtra, err := strconv.Atoi(string(chunk[:len(chunk)-1]))
	if err != nil {
		// 格式错误
		return Item{}, errors.Annotatef(err, "parsing extra length")
	}

	// 当额外长度超过0
	if lenExtra > 0 {
		// 构造指定长度的字节数组，需要注意的是，这部分读取的数据没有被实际使用，即是被忽略的
		dataIgnored := make([]byte, lenExtra)
		// 往后读取lenExtra长度的字节数组
		_, err := io.ReadFull(hd.br, dataIgnored)
		if err != nil {
			// 此时发现已经没有了，代表该条数据格式存在问题
			if errors.Cause(err) == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			// 包装错误，返回读取 extra data 错误
			return Item{}, errors.Annotatef(err, "reading extra data")
		}
	}

	// 该内容为保存历史记录操作内容的数组
	var data []byte

	if lenData > 0 {
		// 构造指定长度的slice
		data = make([]byte, lenData)
		// 往后读取lenData长度的字节数组
		_, err := io.ReadFull(hd.br, data)
		if err != nil {
			// 此时发现已经没有了，代表该条数据格式存在问题
			if errors.Cause(err) == io.EOF {
				err = io.ErrUnexpectedEOF
			}

			// 包装错误，返回读取 data 错误
			return Item{}, errors.Annotatef(err, "reading data")
		}
	}

	// 开始读取下个字节，预期为\n，这是因为上一条记录已经读取结束了
	if err := hd.consumeByte('\n'); err != nil {
		// 此时发现已经没有了，代表该条数据格式存在问题，因为即使只有一条数据，其末尾也会有\n，
		// 这样下一条数据插入的时候，就自然会在另外一行
		if errors.Cause(err) == io.EOF {
			err = io.ErrUnexpectedEOF
		}

		// 包装错误，返回读取 final newline 错误
		return Item{}, errors.Annotatef(err, "reading final newline")
	}

	// 至此，一条数据读取成功
	return Item{
		// 将毫秒转换为 time
		Time: time.Unix(0, nanos),
		// 将数组转换为 string
		Str: string(data),
	}, nil
}

// 从缓冲区中读取指定 byte，
func (hd *HistoryDecoder) consumeByte(want byte) error {
	// 构造byte slice,分配长度为1,即逐个字节读取
	b := make([]byte, 1)
	// 从缓冲区中读取下一个byte到 b 中，此时br内部的offset也会同步往后移动一位
	_, err := io.ReadFull(hd.br, b)
	// 读取失败时，直接返回，比如读取到末尾
	if err != nil {
		return errors.Trace(err)
	}

	// 如果读取到的字节和目标不一致，那么报错
	if b[0] != want {
		return errors.Errorf("expected to read %v, but read %v", want, b[0])
	}

	return nil
}
