package core

import "time"

type lstreamCmd struct {
	// respCh must be either nil, or 1-buffered and it'll receive exactly one
	// message.
	respCh chan lstreamCmdRes

	// Exactly one of the fields below must be non-nil.
	// 当logstream 状态变为 ConnectedIdle 后开始执行的命令
	bootstrap *lstreamCmdBootstrap
	// 当logstream 状态为 ConnectedIdle，且距离上次执行/连接超过40s，会发送ping，作为保活手段
	ping      *lstreamCmdPing
	queryLogs *lstreamCmdQueryLogs
}

type lstreamCmdCtx struct {
	// 命令
	cmd lstreamCmd
	// 索引
	idx int
	// 保存了 bootstrap 的上下文信息
	bootstrapCtx *lstreamCmdCtxBootstrap

	pingCtx      *lstreamCmdCtxPing
	queryLogsCtx *lstreamCmdCtxQueryLogs

	// Initially, stdoutDoneIdx and stderrDoneIdx are set to false. Once we
	// receive the "command_done" marker from either stdout or stderr, we set the
	// corresponding bool here to true. Once both are set, we consider the
	// command execution done, and parse the results.
	stdoutDone bool
	stderrDone bool

	// errs contains all errors accumulated during command execution. This
	// includes errors printed by the nerdlog_agent.sh (lines starting from
	// "error:", on either stderr or stdout), as well as any errors generated
	// on the Go side, e.g. failure to parse some other output.
	// 当前命令执行出错的信息汇总
	errs []error

	// 当前命令执行 echo exit_code:xx 的结果
	exitCode string

	//  unhandledStdout and unhandledStderr contain the lines which the Go app did
	// not make sense of. These are usually ignored, but if the the
	// nerdlog_agent.sh returns an error code, and there are no specific errors
	// printed (lines with the "error:" prefix), then we'll print all these
	// as an error message.
	unhandledStdout []string
	unhandledStderr []string
}

type lstreamCmdRes struct {
	hostname string

	err  error
	resp interface{}
}

type lstreamCmdBootstrap struct{}

// bootstrap 命令上下文
type lstreamCmdCtxBootstrap struct {
	// 读取到成功的标识位，当读取到 bootstrap ok 时，更新标志位
	receivedSuccess bool
	// 读取到失败的标识位，当读取到 bootstrap failed
	receivedFailure bool

	// warnJournalctlNoAdminAccess is set to true if journalctl is used and the
	// user doesn't have access to all the system logs. It's a separate bool
	// instead of a generic warning message to make it possible to suppress it
	// with a flag.
	// 告警Journactl没有admin权限的标识位
	warnJournalctlNoAdminAccess bool
}

type lstreamCmdPing struct{}

type lstreamCmdCtxPing struct {
}

type lstreamCmdQueryLogs struct {
	maxNumLines int

	from time.Time
	to   time.Time

	query string

	// If linesUntil is not zero, it'll be passed to nerdlog_agent.sh as --lines-until.
	// Effectively, only logs BEFORE this log line (not including it) will be output.
	linesUntil int

	// timestampUntil is not zero, it'll be passed to nerdlog_agent as
	// --timestamp-until-precise and --timestamp-until-seconds. It serves the
	// same purpose as linesUntil for cases when we don't have line numbers (e.g.
	// when using journalctl).
	timestampUntil *timeAndNumMsgs

	// If refreshIndex is true, we'll drop the index file, and rebuild it from
	// scratch (no-op for journalctl logstreams, because there's no
	// nerdlog-maintained index for journalctl).
	refreshIndex bool
}

type lstreamCmdCtxQueryLogs struct {
	//
	Resp *LogResp
	// 该 log stream 所获取的所有文件
	logfiles []logfileWithStartingLinenumber
	lastTime time.Time
}

type logfileWithStartingLinenumber struct {
	filename       string
	fromLinenumber int
}
