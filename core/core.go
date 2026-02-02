package core

import "time"

const (
	// MaxNumLinesDefault is a default for QueryLogsParams.MaxNumLines below.
	// 默认读取日志的最大行数
	MaxNumLinesDefault = 250
)

// 查询日志的参数，有四要素：时间，查询语句，是否查询之前，是否刷新索引
type QueryLogsParams struct {
	// maxNumLines is how many log lines the nerdlog_agent.sh will return at
	// most.
	MaxNumLines int

	From time.Time
	To   time.Time

	Query string

	// If LoadEarlier is true, it means we're only loading the logs _before_ the ones
	// we already had.
	LoadEarlier bool

	// If DontAddHistoryItem is true, the browser-like history will not be
	// populated with a new item (it should be used exactly when we're navigating
	// this browser-like history back and forth)
	DontAddHistoryItem bool

	// If RefreshIndex is true, we'll drop the index file for all logstreams, and
	// rebuild it from scratch (no-op for journalctl logstreams, because there's
	// no nerdlog-maintained index for journalctl).
	RefreshIndex bool
}

// LogResp is a log response from a single logstream
// 单个主机消息和元信息
type LogResp struct {
	// MinuteStats is a map from the unix timestamp (in seconds) to the stats for
	// the minute starting at this timestamp.
	MinuteStats map[int64]MinuteStatsItem

	Logs []LogMsg

	// NumMsgsTotal is the total number of messages in the time range (and
	// included in MinuteStats). This number is usually larger than len(Logs).
	NumMsgsTotal int

	// DebugInfo contains info collected during this particular query.
	DebugInfo LogstreamDebugInfo
}

type LogstreamDebugInfo struct {
	// AgentStdout and AgentStderr contain arbitrary human-readable debugging
	// info printed by the agent script.
	AgentStdout []string
	AgentStderr []string
}

// LogRespTotal is a log response from a LStreamsManager. It's merged from
// multiple LogResp's and it also contains some extra field(s), e.g. LoadedEarlier.
// 所有主机的消息和元信息的汇总
type LogRespTotal struct {
	// If LoadedEarlier is true, it means we've just loaded more logs instead of replacing
	// the logs (the Logs slice still contains everything though).
	LoadedEarlier bool

	// MinuteStats is a map from the unix timestamp (in seconds) to the stats for
	// the minute starting at this timestamp.
	// 查询的所有主机分钟级消息数
	MinuteStats map[int64]MinuteStatsItem

	// 查询的所有主机日志汇总
	Logs []LogMsg

	// NumMsgsTotal is the total number of messages in the time range (and
	// included in MinuteStats). This number is usually larger than len(Logs).
	// 查询的所有消息数
	NumMsgsTotal int

	// 查询过程中的错误汇总
	Errs []error

	// DebugInfo is a map from the logstream name to the corresponding debug info
	// collected during this particular query.
	DebugInfo map[string]LogstreamDebugInfo

	// QueryDur shows how long the query took.
	// 查询耗时
	QueryDur time.Duration
}

type MinuteStatsItem struct {
	NumMsgs int
}

// 代表 Logs Table 的一条记录，其被保存在 row 的第一列中
type LogMsg struct {
	// 时间
	Time time.Time
	//
	DecreasedTimestamp bool

	// 日志文件名
	// LogFilename and LogLinenumber are file ane line number in that file
	LogFilename string
	// 日志行数
	LogLinenumber int

	// CombinedLinenumber is the line number in pseudo-file: all (actually just
	// two) log files concatenated. This is the linenumbers output by the
	// nerdlog_agent.sh for every "msg:" line, and this is the linenumber
	// which should be used for --lines-until param.
	CombinedLinenumber int

	// 消息
	Msg string
	// 内容
	Context map[string]string
	// 日志级别
	Level LogLevel

	OrigLine string
}

type LogLevel string

const LogLevelUnknown LogLevel = ""
const LogLevelDebug LogLevel = "debug"
const LogLevelInfo LogLevel = "info"
const LogLevelWarn LogLevel = "warn"
const LogLevelError LogLevel = "error"
