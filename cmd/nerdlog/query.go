package main

import (
	"github.com/dimonomid/nerdlog/shellescape"
	"github.com/juju/errors"
)

// QueryFull contains everything that defines a query: the logstreams filter, time range,
// and the query to filter logs.
// 代表一个查询，组成有：logstreams, time, query, selQuery
// 示例记录为：nerdlog --lstreams 'localhost:22:journalctl' --time -1h --pattern /Error/ --selquery 'time STICKY, message, lstream, *'
type QueryFull struct {
	// 由逗号分隔的多个logstream，每个logstream格式为：<user>@<host>:<port>:</var/log/messages|journalctl|...>
	LStreams string
	// 时间/时间范围，用于限制日志的获取范围，支持格式为：-1h（表示最近1小时），Mar27 12:00（表示特定时间）
	Time string
	// awk 查询模式，用于过滤日志内容，以便满足指定条件，例如：/Error/
	Query string
	// selQuery，即查询日志，例如：time STICKY, message, lstream, *
	SelectQuery SelectQuery
}

// 执行前缀
var execName = "nerdlog"

// numShellParts defines how many shell parts should be in the
// shell-command-marshalled form. It looks like this:
//
//	nerdlog --lstreams <value> --time <value> --pattern <value>
//
// Therefore, there are 7 parts.
// 定义了一个 shell-command 中有几个部分，如下，至少7个部分，格式如下：
// nerdlog --lstreams <value> --time <value> --pattern <value>
// TODO Fix 出现了描述不一致的场景，描述中只有7处，实际有9处，默认有 --selquery，修正：--selquery 
var numShellParts = 1 + 3*2

// 将 QueryFull 转换为原始的shell命令
func (qf *QueryFull) MarshalShellCmd() string {
	// 将 QueryFull 转换为 []string
	parts := qf.MarshalShellCmdParts()
	// 非法字符转义，并拼接为字符串，即shell命令，例如：nerdlog --lstreams 'localhost:22:journalctl' --time -1h --pattern /Error/ --selquery 'time STICKY, message, lstream, *'
	return shellescape.Escape(parts)
}

// 将 string 读取并更新
func (qf *QueryFull) UnmarshalShellCmd(cmd string) error {
	// 将 cmd 解析为合法的字符串数组
	parts, err := shellescape.Parse(cmd)
	if err != nil {
		return errors.Trace(err)
	}

	// 使用 parts 更新到 queryFull
	if err := qf.UnmarshalShellCmdParts(parts); err != nil {
		return errors.Trace(err)
	}

	return nil
}

// 将 QueryFull 转换为 []string
func (qf *QueryFull) MarshalShellCmdParts() []string {
	// 设置7个大小的slice
	parts := make([]string, 0, numShellParts)

	// [0] = nerlog
	parts = append(parts, execName)
	// [1] = --lstreams [2] = logStream
	parts = append(parts, "--lstreams", qf.LStreams)
	// [3] = --time [4] = time
	parts = append(parts, "--time", qf.Time)
	// [5] = --patten [6] = query
	parts = append(parts, "--pattern", qf.Query)
	// [7] = --selquery --selquery [8] = Select Query
	parts = append(parts, "--selquery", string(qf.SelectQuery))

	return parts
}

// UnmarshalShellCmdParts unmarshals shell command parts to the receiver
// QueryFull.  Note that no checks are performed as to whether LStreams,
// Time or Query are actually valid strings.
// 按照 parts 更新 QueryFull
func (qf *QueryFull) UnmarshalShellCmdParts(parts []string) error {
	// 检查长度，其中至少7处，否则视为内容非法
	if len(parts) < numShellParts {
		return errors.Errorf(
			"not enough parts; should be at least %d, got %d", numShellParts, len(parts),
		)
	}

	// 检查首个，必须为 nerdlog
	if parts[0] != execName {
		return errors.Errorf("command should begin from %q, but it's %q", execName, parts[0])
	}

	// 排除第一个
	parts = parts[1:]

	var lstreamsSet, timeSet, querySet, selectQuerySet bool

	// 以每两个的方式迭代，即 [0] =key [1] = value
	for ; len(parts) >= 2; parts = parts[2:] {
		switch parts[0] {
		// 处理 --lstreams
		case "--lstreams":
			qf.LStreams = parts[1]
			lstreamsSet = true
		// 处理 --time
		case "--time":
			qf.Time = parts[1]
			timeSet = true
		// 处理 --patten
		case "--pattern":
			qf.Query = parts[1]
			querySet = true
		// 处理 --selquery
		case "--selquery":
			qf.SelectQuery = SelectQuery(parts[1])
			selectQuerySet = true
		}
	}

	if !lstreamsSet {
		return errors.Errorf("--lstreams is missing")
	}

	if !timeSet {
		return errors.Errorf("--time is missing")
	}

	if !querySet {
		return errors.Errorf("--pattern is missing")
	}

	// SelectQuery 本身是可选的
	if !selectQuerySet {
		// NOTE: we can't return an error here since selquery was not there from the beginning
		qf.SelectQuery = DefaultSelectQuery
	}

	return nil
}
