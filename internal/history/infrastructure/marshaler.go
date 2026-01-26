package infrastructure

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/juju/errors"
)

// HistoryItemMarshaler 负责历史项的序列化/反序列化
type HistoryItemMarshaler struct{}

// NewHistoryItemMarshaler 创建新的编码器
func NewHistoryItemMarshaler() *HistoryItemMarshaler {
	return &HistoryItemMarshaler{}
}

// Marshal 将历史项序列化为字节
// 格式：:<timestamp>:<content-len>:<content>\n
// 例如：:1768526631165763989:100:nerdlog --lstreams 'localhost:22:/path'
func (m *HistoryItemMarshaler) Marshal(timestamp time.Time, content string) []byte {
	var buf bytes.Buffer
	buf.WriteString(":")
	buf.WriteString(strconv.FormatInt(timestamp.UnixNano(), 10))
	buf.WriteString(":")
	buf.WriteString(strconv.Itoa(len(content)))
	buf.WriteString(":")
	buf.WriteString(content)
	buf.WriteString("\n")
	return buf.Bytes()
}

// Unmarshal 反序列化单行历史项
func (m *HistoryItemMarshaler) Unmarshal(line string) (time.Time, string, error) {
	// 格式检查
	if len(line) == 0 || line[0] != ':' {
		return time.Time{}, "", fmt.Errorf("invalid history format: missing leading colon")
	}

	// 移除前导冒号和尾部换行符
	line = line[1:]
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}

	// 第一个分隔符分离时间戳
	idx1 := bytes.IndexByte([]byte(line), ':')
	if idx1 == -1 {
		return time.Time{}, "", fmt.Errorf("invalid history format: missing timestamp")
	}

	timestampStr := line[:idx1]
	timestampNano, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return time.Time{}, "", errors.Trace(err)
	}
	timestamp := time.Unix(0, timestampNano)

	// 第二个分隔符分离内容长度
	rest := line[idx1+1:]
	idx2 := bytes.IndexByte([]byte(rest), ':')
	if idx2 == -1 {
		return time.Time{}, "", fmt.Errorf("invalid history format: missing content length")
	}

	// 对于向后兼容性，允许内容长度不匹配
	// 直接使用冒号后的内容作为数据
	content := rest[idx2+1:]

	return timestamp, content, nil
}

// DecodeFromReaderAsItems 从 Reader 读取并解码所有历史项，返回结构体切片
func (m *HistoryItemMarshaler) DecodeFromReaderAsItems(reader io.Reader) ([]struct {
	Timestamp time.Time
	Content   string
}, error) {
	var items []struct {
		Timestamp time.Time
		Content   string
	}
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		timestamp, content, err := m.Unmarshal(line)
		if err != nil {
			// 容错：跳过无效行，继续处理后续行
			continue
		}

		items = append(items, struct {
			Timestamp time.Time
			Content   string
		}{
			Timestamp: timestamp,
			Content:   content,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, errors.Trace(err)
	}

	return items, nil
}
