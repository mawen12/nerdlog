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

const (
	Verbose3 LogLevel = iota
	Verbose2
	Verbose1
	Info
	Warning
	Error
)

var logFile *os.File
var logFileMtx sync.Mutex

// printf prints a formatted message to the log file ~/.nerdlog.log
func printf(toStdout bool, format string, a ...interface{}) {
	var w io.Writer
	if toStdout {
		w = os.Stdout
	} else {
		w = writer()
	}

	fmt.Fprintf(w, "%s: ", time.Now().Format("2006-01-02T15:04:05.999"))

	if !strings.HasSuffix(format, "\n") {
		format += "\n"
	}

	fmt.Fprintf(w, format, a...)
}

func writer() io.Writer {
	logFileMtx.Lock()
	defer logFileMtx.Unlock()

	if logFile == nil {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			panic(err.Error())
		}

		fname := filepath.Join(homeDir, ".nerdlog.log")

		logFile, err = os.OpenFile(fname, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			panic(err.Error())
		}
	}

	return logFile
}

type Logger struct {
	minLevel LogLevel

	toStdout bool

	namespace string
	context   map[string]string
}

func NewLogger(minLevel LogLevel) *Logger {
	return &Logger{
		minLevel: minLevel,
	}
}

func (l *Logger) thisOrDefault() *Logger {
	if l != nil {
		return l
	}

	return &Logger{
		minLevel: Info,
	}
}

func (l *Logger) WithNamespaceAppended(n string) *Logger {
	l = l.thisOrDefault()

	// 追加命名空间，以 / 方式分隔
	ns := l.namespace
	if ns != "" {
		ns += "/"
	}
	ns += n

	newLogger := *l
	newLogger.namespace = ns
	return &newLogger
}

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
	l = l.thisOrDefault()

	if level < l.minLevel {
		return
	}

	if l.namespace != "" {
		printf(l.toStdout, "[%s] %s", l.namespace, fmt.Sprintf(format, a...))
	} else {
		printf(l.toStdout, "%s", l.namespace, fmt.Sprintf(format, a...))
	}
}
