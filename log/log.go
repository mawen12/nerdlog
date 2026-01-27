package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type LogLevel int

// 日志级别从低到高排列
const (
	Verbose3 LogLevel = iota
	Verbose2
	Verbose1
	Info
	Warning
	Error
)

// 全局日志文件log/log.go
var logFile *os.File

// 互斥锁
var logFileMtx sync.Mutex

// printf prints a formatted message to the log file ~/.nerdlog.log
// 打印格式化消息到日志文件 ~/.nerdlog.log
func printf(toStdout bool, format string, a ...interface{}) {
	var w io.Writer
	if toStdout {
		w = os.Stdout
	} else {
		w = writer()
	}

	// 格式为：时间: 消息内容
	fmt.Fprintf(w, "%s: ", time.Now().Format("2006-01-02T15:04:05.999"))

	// 添加换行符
	if !strings.HasSuffix(format, "\n") {
		format += "\n"
	}

	// 写入到文件/控制台
	fmt.Fprintf(w, format, a...)
}

// 初始化日志文件写入器
func writer() io.Writer {
	// 加上互斥锁，防止并发创建文件
	logFileMtx.Lock()
	defer logFileMtx.Unlock()

	// 如果日志文件还没有打开，则打开它
	if logFile == nil {
		// 获取用户主目录
		homeDir, err := os.UserHomeDir()
		if err != nil {
			panic(err.Error())
		}

		// 拼接日志文件路径，具体为 ~/.nerdlog.log
		fname := filepath.Join(homeDir, ".nerdlog.log")

		// 打开或创建日志文件，追加写入
		logFile, err = os.OpenFile(fname, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			panic(err.Error())
		}
	}

	return logFile
}

type Logger struct {
	// 最小日志级别
	minLevel LogLevel
	// 是否输出到标准输出
	toStdout bool

	// 命名空间
	namespace string
	// 额外上下文信息
	context map[string]string
}

// 使用给定的最小日志级别创建新的 Logger 实例
func NewLogger(minLevel LogLevel) *Logger {
	return &Logger{
		minLevel: minLevel,
	}
}

// 返回当前 Logger 实例，或者如果为 nil，则返回默认 Logger 实例
// 因为该方法是带指针接收者的，所以可以直接调用 nil 指针上的该方法
// 对于这种情况，增加非空的检查逻辑
func (l *Logger) thisOrDefault() *Logger {
	if l != nil {
		return l
	}

	return &Logger{
		minLevel: Info,
	}
}

// 返回一个新的 Logger 实例，命名空间为当前实例的命名空间加上给定的命名空间
func (l *Logger) WithNamespaceAppended(n string) *Logger {
	// 确保 Logger 实例不为 nil
	l = l.thisOrDefault()

	// 追加命名空间，以 / 方式分隔，比如 a/b/c
	ns := l.namespace
	if ns != "" {
		ns += "/"
	}
	ns += n

	// 把指针指向的Logger按值复制一份，修改命名空间后返回新实例
	newLogger := *l
	newLogger.namespace = ns
	return &newLogger
}

// 返回一个新的 Logger 实例，设置是否输出到标准输出
func (l *Logger) WithStdout(toStdout bool) *Logger {
	l = l.thisOrDefault()

	newLogger := *l
	newLogger.toStdout = toStdout
	return &newLogger
}

func (l *Logger) Verbose3f(format string, a ...interface{}) {
	l.Printf(Verbose3, format, a...)
}

func (l *Logger) Verbose2f(format string, a ...interface{}) {
	l.Printf(Verbose2, format, a...)
}

func (l *Logger) Verbose1f(format string, a ...interface{}) {
	l.Printf(Verbose1, format, a...)
}

func (l *Logger) Infof(format string, a ...interface{}) {
	l.Printf(Info, format, a...)
}

func (l *Logger) Warnf(format string, a ...interface{}) {
	l.Printf(Warning, format, a...)
}

func (l *Logger) Errorf(format string, a ...interface{}) {
	l.Printf(Error, format, a...)
}

func (l *Logger) Printf(level LogLevel, format string, a ...interface{}) {
	// 确保 Logger 实例不为 nil
	l = l.thisOrDefault()

	// 检查日志级别
	if level < l.minLevel {
		return
	}

	// 如果有命名空间，则需要在输出格式中加上 [%s] 前缀
	if l.namespace != "" {
		printf(l.toStdout, "[%s] %s", l.namespace, fmt.Sprintf(format, a...))
	} else {
		printf(l.toStdout, "%s", l.namespace, fmt.Sprintf(format, a...))
	}
}
