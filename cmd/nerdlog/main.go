package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dimonomid/nerdlog/clhistory"
	"github.com/dimonomid/nerdlog/clipboard"
	"github.com/dimonomid/nerdlog/log"
	"github.com/dimonomid/nerdlog/version"
	"github.com/spf13/pflag"
)

// TODO: make multiple of them
const inputTimeLayout = "Jan2 15:04"
const inputTimeLayoutMMHH = "15:04"

func main() {
	// 读取用户目录 ~
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting home dir: %s\n", err)
		os.Exit(1)
	}

	// 默认读取 ~/.ssh/id_ed25519 | id_ecdsa | id_rsa
	defaultSSHKeys := []string{
		filepath.Join(homeDir, ".ssh", "id_ed25519"),
		filepath.Join(homeDir, ".ssh", "id_ecdsa"),
		filepath.Join(homeDir, ".ssh", "id_rsa"),
	}

	var (
		flagVersion = pflag.BoolP("version", "v", false, "Print version info and exit")
		// 查询时间，支持相对时间和指定时间，例如：--time -1h
		flagTime           = pflag.StringP("time", "t", "", "Time range in the same format as accepted by the UI. Examples: '1h', 'Mar27 12:00'")
		flagLStreamsConfig = pflag.String("lstreams-config", filepath.Join(homeDir, ".config", "nerdlog", "logstreams.yaml"), "logstreams config file to use; set to an empty string to disable reading logstreams config")
		// 
		flagCmdHistoryFile = pflag.String("cmdhistory-file", filepath.Join(homeDir, ".nerdlog_history"), "Command-line history file")
		// 其内部存储了如下信息：:1768526631165763989:136:0:nerdlog --lstreams 'localhost:22:/home/mawen/logs/monitor.log' --time -1h --pattern /port/ --selquery 'time STICKY, message, lstream, *'
		// 这是完整的查询
		flagQueryHistoryFile = pflag.String("queryhistory-file", filepath.Join(homeDir, ".nerdlog_query_history"), "Query history file")
		// log streams，指定要读取的目标日志信息，例如：--lstreams 'localhost:22:journalctl'
		flagLStreams = pflag.StringP("lstreams", "h", "", "Logstreams to connect to, as comma-separated glob patterns, e.g. 'foo-*,bar-*'")
		// awk 查询，例如：--pattern /INFO/
		flagQuery = pflag.StringP("pattern", "p", "", "Initial awk pattern to use")
		// select 查询，例如：time STICKY, message, lstream, level_name AS level, *
		flagSelectQuery = pflag.StringP("selquery", "s", "", "SELECT-like query to specify which fields to show, like 'time STICKY, message, lstream, level_name AS level, *'")
		flagLogLevel    = pflag.String("loglevel", "error", "This is NOT about the logs that nerdlog fetches from the remote servers, it's rather about nerdlog's own log. Valid values are: error, warning, info, verbose1, verbose2 or verbose3")
		flagSSHConfig   = pflag.String("ssh-config", filepath.Join(homeDir, ".ssh", "config"), "ssh config file to use; set to an empty string to disable reading ssh config")
		flagSSHKeys     = pflag.StringSlice("ssh-key", defaultSSHKeys, "ssh keys to use; only the first existing file will be used")

		// NOTE: we specifically use StringArray and not StringSlice here, because we
		// don't want it to interpret commas in the values, like "--set foo=123,bar=234", since
		// it messes with more complicated option syntax like 'transport=custom:some "arbitrary command"'
		flagSet = pflag.StringArray("set", []string{}, "Initial option values in the form option=value, in the same way you'd specify them for the :set command. This flag can be given multiple times")

		flagNoJournalctlAccessWarn = pflag.Bool("no-journalctl-access-warning", false, "Suppress the warning when journalctl is being used by the user who can't read all system logs")
	)

	pflag.Parse()

	// 输出版本信息然后退出
	if *flagVersion {
		fmt.Print(version.VersionFullDescr())
		os.Exit(0)
	}

	// 读取查询历史记录，该记录是用于进行查询的整体整合
	queryCLHistory, err := clhistory.New(clhistory.CLHistoryParams{
		Filename: *flagQueryHistoryFile,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing query history: %s\n", err)
		os.Exit(1)
	}

	// 初始化默认参数
	initialTime := "-1h"           // 当未指定时间时，默认读取最近1小时
	initialLStreams := "localhost" // 当未指定log streams时，默认为localhost，其底层默认从 /var/log/messages 读取
	if runtime.GOOS == "windows" {
		// On Windows, "localhost" doesn't make much sense, since there are usually no
		// plain log files and no journalctl, so using a different default here.
		initialLStreams = "myserver.com:22" // 由于 windows 平台没有 plain log file 和 journalctl，因此指定 localhost 在该场景中没有意义
	}
	initialQuery := ""                       // 初始awk查询为""
	initialSelectQuery := DefaultSelectQuery // 默认为 time STICKY, message, lstream, *
	connectRightAway := false                // 立即连接查询标志位

	if *flagTime != "" {
		initialTime = *flagTime
		connectRightAway = true // 当用户手动指定了查询时间，代表需要立刻执行查询
	}

	if *flagLStreams != "" {
		initialLStreams = *flagLStreams
		connectRightAway = true // 当用户手动指定了log stream，代表需要立刻执行查询
	}

	if *flagQuery != "" {
		initialQuery = *flagQuery
		connectRightAway = true // 当用户手动指定了awk查询，代表需要立刻执行查询
	}

	if *flagSelectQuery != "" {
		initialSelectQuery = SelectQuery(*flagSelectQuery)
		connectRightAway = true // 当用户手动指定了select query查询，代表需要立刻执行查询
	}

	// 初始化查询，使用四要素组装
	initialQueryData := QueryFull{
		Time:        initialTime,        // time --time
		Query:       initialQuery,       // awk --pattern
		LStreams:    initialLStreams,    // log streams --lstreams
		SelectQuery: initialSelectQuery, // select query --selquery
	}

	// 当不符合立即查询的条件时，即四要素数据均没有指定，则尝试从历史文件 /home/mawen/.nerdlog_query_history 中获取最近一条
	if !connectRightAway {
		// No query params were given, try to get the last one from the history.
		// 获取历史记录中最新的一个
		item, _ := queryCLHistory.Prev("")
		if item.Str != "" {
			var qf QueryFull
			// 使用str更新到 queryFull，如果没有错误的时候，则更新到initialQueryData中
			if err := qf.UnmarshalShellCmd(item.Str); err != nil {
				// Ignore the error, just use the defaults
			} else {
				// Successfully parsed the last item from query history, use that.
				initialQueryData = qf
			}
		}
	}

	// 剪切板可用检查，如果报错，则控制台输出，但是不退出
	if clipboard.InitErr != nil {
		fmt.Printf("NOTE: X Clipboard is not available: %s\n", clipboard.InitErr.Error())
	}

	// 设置日志级别，将字符串映射为对应日志级别，对于非法的日志级别，报错并退出
	logLevel := log.Info
	if *flagLogLevel == "error" {
		logLevel = log.Error
	} else if *flagLogLevel == "warning" {
		logLevel = log.Warning
	} else if *flagLogLevel == "info" {
		logLevel = log.Info
	} else if *flagLogLevel == "verbose1" {
		logLevel = log.Verbose1
	} else if *flagLogLevel == "verbose2" {
		logLevel = log.Verbose2
	} else if *flagLogLevel == "verbose3" {
		logLevel = log.Verbose3
	} else {
		fmt.Fprintf(os.Stderr, "Invalid --loglevel, try error, warning, info, verbose1, verbose2 or verbose3")
		os.Exit(1)
	}

	// 初始化 app
	app, err := newNerdlogApp(
		// 汇总参数项
		nerdlogAppParams{
			initialOptionSets:    *flagSet,
			initialQueryData:     initialQueryData,
			connectRightAway:     connectRightAway,
			clipboardInitErr:     clipboard.InitErr,
			logLevel:             logLevel,
			sshConfigPath:        *flagSSHConfig,
			logstreamsConfigPath: *flagLStreamsConfig,
			cmdHistoryFile:       *flagCmdHistoryFile,
			sshKeys:              *flagSSHKeys,

			noJournalctlAccessWarn: *flagNoJournalctlAccessWarn,
		},
		// 查询历史
		queryCLHistory,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	// 启动 UI 界面
	fmt.Println("Starting UI ...")
	if err := app.runTViewApp(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	// We end up here when the user quits the UI

	// 退出 UI 界面
	fmt.Println("")
	fmt.Println("Closing connections...")

	app.Close()
	app.Wait()

	fmt.Println("Have a nice day.")
}
