package main

import (
	"github.com/dimonomid/nerdlog/shellescape"
	"github.com/juju/errors"
)

// QueryFull contains everything that defines a query: the logstreams filter, time range,
// and the query to filter logs.
// 代表一个查询，组成有：logstreams, time, query

type QueryFull struct {
	LStreams string
	Time     string
	Query    string

	SelectQuery SelectQuery
}

var execName = "nerdlog"

// numShellParts defines how many shell parts should be in the
// shell-command-marshalled form. It looks like this:
//
//	nerdlog --lstreams <value> --time <value> --pattern <value>
//
// Therefore, there are 7 parts.
// 定义了一个 shell-command 中有几个部分，如下，一共7个部分，格式如下：
// nerdlog --lstreams <value> --time <value> --pattern <value>
// TODO Fix 出现了描述不一致的场景，描述中只有7处，实际有9处，默认有 --selquery
var numShellParts = 1 + 3*2

// 将 QueryFull 转换为原始的shell命令
func (qf *QueryFull) MarshalShellCmd() string {
	// 将 QueryFull 转换为原始字符串数组
	parts := qf.MarshalShellCmdParts()
	// 非法字符转义，并拼接为字符串，即shell命令
	return shellescape.Escape(parts)
}

func (qf *QueryFull) UnmarshalShellCmd(cmd string) error {
	parts, err := shellescape.Parse(cmd)
	if err != nil {
		return errors.Trace(err)
	}

	if err := qf.UnmarshalShellCmdParts(parts); err != nil {
		return errors.Trace(err)
	}

	return nil
}

// 将 QueryFull 转换为原始字符串数组
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
func (qf *QueryFull) UnmarshalShellCmdParts(parts []string) error {
	if len(parts) < numShellParts {
		return errors.Errorf(
			"not enough parts; should be at least %d, got %d", numShellParts, len(parts),
		)
	}

	if parts[0] != execName {
		return errors.Errorf("command should begin from %q, but it's %q", execName, parts[0])
	}

	parts = parts[1:]

	var lstreamsSet, timeSet, querySet, selectQuerySet bool

	for ; len(parts) >= 2; parts = parts[2:] {
		switch parts[0] {
		case "--lstreams":
			qf.LStreams = parts[1]
			lstreamsSet = true
		case "--time":
			qf.Time = parts[1]
			timeSet = true
		case "--pattern":
			qf.Query = parts[1]
			querySet = true
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

	if !selectQuerySet {
		// NOTE: we can't return an error here since selquery was not there from the beginning
		qf.SelectQuery = DefaultSelectQuery
	}

	return nil
}
