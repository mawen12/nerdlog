package main

import (
	"strings"
	"time"

	"github.com/dimonomid/nerdlog/core"
	"github.com/juju/errors"
)

// 代表了 --time 的解析结果
type FromToRange struct {
	From TimeOrDur
	To   TimeOrDur
}

// 使用指定时区解析 --time 的值
func ParseFromToRange(timezone *time.Location, s string) (FromToRange, error) {
	// 检查是否有 to ，该场景表明为指定时间范围
	flds := strings.Split(s, " to ")
	if len(flds) == 0 {
		// 代表传递空字符串，因为没有to的话，len(flds) = 1
		return FromToRange{}, errors.New("time can't be empty. try -5h")
	}

	var from, to TimeOrDur
	var err error

	// 读取首个字段，可能为 -1h/Mar27 12:00/
	fromStr := flds[0]

	// 解析字符串
	from, err = parseAndInferTimeOrDur(timezone, inputTimeLayout, fromStr)
	if err != nil {
		return FromToRange{}, errors.Annotatef(err, "invalid 'from' duration")
	}

	to = TimeOrDur{}

	// 如果有 to 的话，则解析
	if len(flds) > 1 {
		toStr := flds[1]

		// If there's no date, prepend date
		// 场景为：Mar27 12:00 to 24:00，此时将to进行完善，结果为 Mar27 24:00
		if len(toStr) <= 5 && len(fromStr) > 5 {
			toStr = fromStr[:5] + " " + toStr
		}

		var err error
		// 解析字符串
		to, err = parseAndInferTimeOrDur(timezone, inputTimeLayout, toStr)
		if err != nil {
			return FromToRange{}, errors.Annotatef(err, "invalid 'to' duration")
		}
	}

	return FromToRange{
		From: from,
		To:   to,
	}, nil
}

func (ftr *FromToRange) String() string {
	fromStr := ftr.From.Format(inputTimeLayout)

	if ftr.To.IsZero() {
		return fromStr
	}

	// If both From and To are absolute and have the same day, then omit day for
	// the To.
	format := inputTimeLayout
	_, fm, fd := ftr.From.Time.Date()
	_, tm, td := ftr.To.Time.Date()
	if fm == tm && fd == td {
		format = inputTimeLayoutMMHH
	}

	return fromStr + " to " + ftr.To.Format(format)
}

// 尝试解析 duration/time，如果是 time，则会推断年份
func parseAndInferTimeOrDur(timezone *time.Location, layout, s string) (TimeOrDur, error) {
	// 尝试解析 duration/time
	t, err := ParseTimeOrDur(timezone, layout, s)
	if err != nil {
		return TimeOrDur{}, err
	}

	// 如果是 time
	if t.IsAbsolute() {
		// 根据给定时间的月份和当前对比，来推断年份，那么尽管指定的时候没有写明年份 Mar27 12:00，此时会设置好年份
		t.Time = core.InferYear(time.Now(), t.Time)
	}

	return t, nil
}
