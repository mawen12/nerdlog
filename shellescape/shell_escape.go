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
	parserQuoteStateNone parserQuoteState = iota
	parserQuoteStateSingle
	parserQuoteStateDouble
	parserQuoteStateDoubleEscaped
)

func Parse(shellCmd string) ([]string, error) {
	var parts []string

	partBuilder := strings.Builder{}

	inPart := false
	quoteState := parserQuoteStateNone

	finalizePart := func() {
		parts = append(parts, partBuilder.String())
		partBuilder.Reset()
	}

	for _, r := range shellCmd {
		// Sanity check, TODO: perhaps remove it
		if !inPart && quoteState != parserQuoteStateNone {
			panic("should never be here")
		}

		isSpace := unicode.IsSpace(r)

		if !inPart {
			if !isSpace {
				inPart = true
			} else {
				// We're in the whitespace, nothing else to do here.
				continue
			}
		}

		switch quoteState {
		case parserQuoteStateNone:
			switch r {
			case '\'':
				quoteState = parserQuoteStateSingle
			case '"':
				quoteState = parserQuoteStateDouble
			default:
				if !isSpace {
					partBuilder.WriteRune(r)
				} else {
					finalizePart()
					inPart = false
				}
			}

		case parserQuoteStateSingle:
			switch r {
			case '\'':
				quoteState = parserQuoteStateNone
			default:
				partBuilder.WriteRune(r)
			}

		case parserQuoteStateDouble:
			switch r {
			case '"':
				quoteState = parserQuoteStateNone
			case '\\':
				quoteState = parserQuoteStateDoubleEscaped
			default:
				partBuilder.WriteRune(r)
			}

		case parserQuoteStateDoubleEscaped:
			switch r {
			case '\\':
				partBuilder.WriteRune(r)
			case '"':
				partBuilder.WriteRune(r)
			default:
				partBuilder.WriteRune('\\')
				partBuilder.WriteRune(r)
			}

			quoteState = parserQuoteStateDouble
		}
	}

	if inPart {
		if quoteState != parserQuoteStateNone {
			return nil, errors.Errorf("unfinished quote")
		}

		finalizePart()
	}

	return parts, nil
}
