package shellescape

import (
	"strings"
	"unicode"

	"github.com/juju/errors"
)

// 规范化，并拼接为string
func Escape(parts []string) string {
	// 保存转义后或无需转义
	eParts := make([]string, 0, len(parts))

	for _, part := range parts {
		// 检查当前部分是否为合法字符串，规则为：字母 数字 - _ . /
		needEscape := false
		// 依次检查
		for _, r := range part {
			if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-' && r != '_' && r != '.' && r != '/' {
				needEscape = true
				break
			}
		}

		// 长度为0,也需要转换
		if len(part) == 0 {
			needEscape = true
		}

		if needEscape {
			// 两侧加上 ''，然后将 ' 进行转义为 '\"'\"'
			part = "'" + strings.Replace(part, "'", "'\"'\"'", -1) + "'"
		}
		// 写入处理后的数组中
		eParts = append(eParts, part)
	}

	// 使用空格拼接
	return strings.Join(eParts, " ")
}

type parserQuoteState int

const (
	parserQuoteStateNone          parserQuoteState = iota
	parserQuoteStateSingle                         // 对应单引号 '\''
	parserQuoteStateDouble                         // 对应双引号 '"'
	parserQuoteStateDoubleEscaped                  // 对应反斜杠 '\\'
)

// 通用的解析，解析一个字符串，并将特殊字符进行转义，然后按照空格，单引号进行拆分
// 将 shell 转换为 nerdlog --lstreams 'localhost:22:journalctl' --time -1h --pattern /Error/ --selquery 'time STICKY, message, lstream, *'
func Parse(shellCmd string) ([]string, error) {
	var parts []string

	// 用于将解析的部分组装为字符串
	partBuilder := strings.Builder{}

	// 用于表示是否处在连续的字符串中，即能否决定后续解析，不能后续解析的场景：
	// - 遇到空格，将跳过进行后续处理
	inPart := false
	quoteState := parserQuoteStateNone

	finalizePart := func() {
		parts = append(parts, partBuilder.String())
		partBuilder.Reset()
	}

	// 依次迭代每个字节
	for _, r := range shellCmd {
		// Sanity check, TODO: perhaps remove it
		//
		if !inPart && quoteState != parserQuoteStateNone {
			panic("should never be here")
		}

		// 检查当前字节是否为空格
		isSpace := unicode.IsSpace(r)

		if !inPart { // 代表刚开始，不在字符串中
			if !isSpace { // 非空格，代表为合法字符串，即开始尝试解析
				inPart = true
			} else { // 遇到空格，跳过
				// We're in the whitespace, nothing else to do here.
				continue
			}
		}

		switch quoteState {
		// 初始解析为： NONE
		case parserQuoteStateNone:
			switch r {
			// 单引号
			case '\'':
				quoteState = parserQuoteStateSingle // => 当前结束，下次转到 case:parserQuoteStateSingle
			// 双引号
			case '"':
				quoteState = parserQuoteStateDouble // => 当前结束，下次转到 case:parserQuoteStateDouble

			default:
				if !isSpace { // 非空格部分，写入part中
					partBuilder.WriteRune(r)
				} else { // 该part完成解析，写入parts,然后重置
					finalizePart()
					inPart = false
				}
			}

		// 单引号，单引号内所有内容将被解析为part
		case parserQuoteStateSingle:
			switch r {
			// 此为单引号，代表该quote结束
			case '\'':
				quoteState = parserQuoteStateNone // => 退出，下次转到 case:parserQuoteStateNone
			// 写入part
			default:
				partBuilder.WriteRune(r)
			}

		// 双引号，双引号内所有内容将被解析为part，其中对于\进行特殊处理
		case parserQuoteStateDouble:
			switch r {
			// 此为双引号，代表该quote结束
			case '"':
				quoteState = parserQuoteStateNone // => 当前结束，下次转到 case:parserQuoteStateNone
			// 此为反斜杠
			case '\\':
				quoteState = parserQuoteStateDoubleEscaped // => 当前结束，下次转到 case:parserQuoteStateDoubleEscaped
			// 写入part
			default:
				partBuilder.WriteRune(r)
			}

		// 反斜杠，遇到\时，直接写入；遇到"时，直接写入，其他会在前面追加\
		case parserQuoteStateDoubleEscaped:
			switch r {
			// 遇到反斜杠，写入part
			case '\\':
				partBuilder.WriteRune(r)
			// 遇到双引号，写入part
			case '"':
				partBuilder.WriteRune(r)
			// 反之前缀加上\,写入part
			default:
				partBuilder.WriteRune('\\')
				partBuilder.WriteRune(r)
			}

			quoteState = parserQuoteStateDouble // => 当前结束，下次转到 case:parserQuoteStateDouble
		}
	}

	if inPart { // 尚未结束，比如 "foo  bar  "， "foo bar"，bar的解析直到for循环结束时，其inPart仍然为true，且quoteState=None
		if quoteState != parserQuoteStateNone { // 解析到最后，不为 parserQuoteStateNone，则该字符串格式错误
			return nil, errors.Errorf("unfinished quote")
		}

		finalizePart() // 写入 parts
	}

	return parts, nil
}
