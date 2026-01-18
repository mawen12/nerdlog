package main

import (
	"time"

	"github.com/juju/errors"
)

type TimeOrDur struct {
	Time time.Time
	Dur  time.Duration
}

func (t TimeOrDur) IsZero() bool {
	return t.Time.IsZero() && t.Dur == 0
}

func (t TimeOrDur) In(loc *time.Location) TimeOrDur {
	if t.Time.IsZero() {
		return t
	}

	t.Time = t.Time.In(loc)
	return t
}

// 当 Time 被设置后，其含义为固定的某个时间，便是绝对时间
func (t TimeOrDur) IsAbsolute() bool {
	return !t.Time.IsZero()
}

// AbsoluteTime returns the exact point in time, either relative to the
// provided relativeTo, or if it represents an absolute point in time already,
// then just returns it (and then relativeTo is ignored).
//
// If relativeTo is zero, AbsoluteTime panics.
//
// AbsoluteTime can never return a zero time.
func (t TimeOrDur) AbsoluteTime(relativeTo time.Time) time.Time {
	if relativeTo.IsZero() {
		panic("relativeTo can't be zero")
	}

	if !t.Time.IsZero() {
		return t.Time
	}

	return relativeTo.Add(t.Dur)
}

func (t TimeOrDur) String() string {
	if !t.Time.IsZero() {
		return t.Time.String()
	}

	return formatDuration(t.Dur)
}

func (t TimeOrDur) Format(layout string) string {
	if !t.Time.IsZero() {
		return t.Time.Format(layout)
	}

	return formatDuration(t.Dur)
}

// ParseTimeOrDur tries to parse a string as either time or duration.
// If parsing as a duration succeeds, then layout is ignored; otherwise it's
// used to parse it as time.
// 支持将 s 解析为 Time/Duration
func ParseTimeOrDur(timezone *time.Location, layout, s string) (TimeOrDur, error) {
	// Try to parse as a duration first
	// 首先尝试解析 duration，如 -1h，出现错误则忽略，解析 time
	dur, err := time.ParseDuration(s)
	if err == nil {
		return TimeOrDur{
			Dur: dur,
		}, nil
	}

	// Now try to parse as a time
	// 按指定格式解析，此时出错的话，便抛出错误
	t, err := time.ParseInLocation(layout, s, timezone)
	if err != nil {
		return TimeOrDur{}, errors.Trace(err)
	}

	return TimeOrDur{
		Time: t,
	}, nil
}
