package core

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/dimonomid/nerdlog/log"
	"github.com/juju/errors"
	"github.com/mvdan/sh/shell"
)

const echoMarkerConnected = "__CONNECTED__"

// ShellTransportCustomCmd is an implementation of ShellTransport that opens an
// shell session using external custom command (such as ssh).
type ShellTransportCustomCmd struct {
	params ShellTransportCustomCmdParams
}

type ShellTransportCustomCmdParams struct {
	// ShellCommand is a command such as this one:
	// "ssh -o 'BatchMode=yes' ${NLPORT:+-p ${NLPORT}} ${NLUSER:+${NLUSER}@}${NLHOST} /bin/sh"
	//
	// It's interpreted not by an external shell, but https://github.com/mvdan/sh
	// It can use vars from the EnvOverride below, as well as any env vars.
	// 自定义的shell命令
	ShellCommand string

	// EnvOverride overrides env vars.
	//
	// An empty value unsets the one from environment, so e.g. if the environment
	// contains a var FOO=123, but EnvOverride contains they key "FOO" with an
	// empty string, then it'll be interpreted as if FOO just didn't exist.
	//
	// For non-localhost commands, Nerdlog sets 3 env vars here:
	//
	// - "NLHOST": Hosname, always present;
	// - "NLPORT": Port, only present if was specified explicitly or was present
	//   in nerdlog logstreams config.
	// - "NLUSER": Username, only present if was specified explicitly or was
	//   present in nerdlog logstreams config.
	// 要覆盖的环境变量
	EnvOverride map[string]string

	Logger *log.Logger
}

// NewShellTransportCustomCmd creates a new ShellTransportCustomCmd with the given shell command.
func NewShellTransportCustomCmd(params ShellTransportCustomCmdParams) *ShellTransportCustomCmd {
	params.Logger = params.Logger.WithNamespaceAppended("TransportCustomCmd")

	return &ShellTransportCustomCmd{
		params: params,
	}
}

// Connect starts the local shell and sends the result to the provided channel.
// 启动连接，其上游是 lstream_client
func (s *ShellTransportCustomCmd) Connect(resCh chan<- ShellConnUpdate) {
	go s.doConnect(resCh)
}

// 负责启动连接，而且会将执行过程发到送 DebugInfo，执行出错或成功后，将结果写入到 Result 上
func (s *ShellTransportCustomCmd) doConnect(resCh chan<- ShellConnUpdate) (res ShellConnResult) {
	logger := s.params.Logger

	// 核心，负责将结果写入到 resCh 上
	defer func() {
		if res.Err != nil {
			logger.Errorf("Connection failed: %s", res.Err)
		}
		resCh <- ShellConnUpdate{
			Result: &res,
		}
	}()

	// Parse shell commands into separate fields.
	cmdFields, err := shell.Fields(s.params.ShellCommand, func(varName string) string {
		// 优先读取 EnvOverride 里的值，实现覆盖环境变量的效果
		if value, ok := s.params.EnvOverride[varName]; ok {
			return value
		}

		return os.Getenv(varName)
	})

	if err != nil {
		res.Err = errors.Annotatef(err, "parsing shell command %q", s.params.ShellCommand)
		return res
	}

	// 应该至少有一个命令
	if len(cmdFields) == 0 {
		res.Err = errors.Errorf("command is empty")
		return res
	}

	var sshCmdDebugBuilder strings.Builder
	// 构建命令的 string，使用空格分隔
	for i, v := range cmdFields {
		if i > 0 {
			sshCmdDebugBuilder.WriteString(" ")
		}
		// 处理单引号
		sshCmdDebugBuilder.WriteString(shellQuote(v))
	}
	sshCmdDebug := sshCmdDebugBuilder.String()

	resCh <- ShellConnUpdate{
		DebugInfo: s.makeDebugInfo(fmt.Sprintf(
			"Trying to connect using external command: %q", sshCmdDebug,
		)),
	}
	logger.Infof("Executing external command: %q", sshCmdDebug)

	// 构造支持取消的context
	ctx, cancel := context.WithCancel(context.Background())
	// 执行命令
	cmd := exec.CommandContext(ctx, cmdFields[0], cmdFields[1:]...)
	// 获取 stdin 管道
	stdin, err := cmd.StdinPipe()
	if err != nil {
		res.Err = errors.Annotatef(err, "getting stdin pipe")
		return res
	}
	// 获取 stdout 管道
	rawStdout, err := cmd.StdoutPipe()
	if err != nil {
		res.Err = errors.Annotatef(err, "getting stdout pipe")
		return res
	}
	// 获取 stderr 管道
	stderr, err := cmd.StderrPipe()
	if err != nil {
		res.Err = errors.Annotatef(err, "getting stderr pipe")
		return res
	}

	// 启动命令
	if err := cmd.Start(); err != nil {
		res.Err = errors.Annotatef(err, "starting shell")
		return res
	}

	// To make sure we were able to connect, we just write "echo __CONNECTED__"
	// to stdin, and wait for it to show up in the stdout.

	resCh <- ShellConnUpdate{
		DebugInfo: s.makeDebugInfo(fmt.Sprintf(
			"Command started, writing \"echo %s\", waiting for it in stdout", echoMarkerConnected,
		)),
	}

	// 写入 echo __CONNECTED__ 命令，
	_, err = fmt.Fprintf(stdin, "echo %s\n", echoMarkerConnected)
	if err != nil {
		res.Err = errors.Annotatef(err, "writing connection marker")
		return res
	}

	// 创建管道，支持读写，
	clientStdoutR, clientStdoutW := io.Pipe()
	// 将reader包装为scanner
	scanner := bufio.NewScanner(rawStdout)
	// 构建保存错误的通道
	connErrCh := make(chan error)
	// 异步goroutine，用于持续从cmd中读取数据
	go func() {
		// 执行结束前关闭通道
		defer clientStdoutW.Close()
		// 持续从cmd的stdout读取数据
		for scanner.Scan() {
			// 读取到一行数据
			line := scanner.Text()
			logger.Verbose3f("Got line while looking for connected marker: %s", line)
			// 如果读取到连接标记
			if line == echoMarkerConnected {
				logger.Verbose3f("Got the marker, switching to raw passthrough for stdout")
				// Done waiting, switch to raw passthrough
				// 发送 nil 到 connErrCh，表示没有错误
				connErrCh <- nil
				// 将stdout中的数据拷贝到clientStdoutW中，直到读取到EOF
				io.Copy(clientStdoutW, rawStdout)
				return
			}
		}

		// 当读取到错误时的处理
		if err := scanner.Err(); err != nil {
			logger.Errorf("Got scanner error while waiting for connection marker: %s", err.Error())
			connErrCh <- errors.Annotatef(err, "reading from stdout while waiting for connection marker")
		} else {
			// Got EOF while waiting for the marker; apparently ssh failed to connect,
			// so just read up all stderr (which likely contains the actual error message),
			// and return it as an error.
			// 读取到了 EOF，说明连接失败，则从 stderr 读取消息
			stderrBytes, _ := io.ReadAll(stderr)
			connErrCh <- errors.Errorf(
				"failed to connect using external command \"%s\": %s",
				sshCmdDebug, string(stderrBytes),
			)
		}
	}()

	// Wait for the marker to show up in output.
	select {
	// 读取到错误信息
	case err := <-connErrCh:
		// 处理错误，
		if err != nil {
			res.Err = errors.Trace(err)
			return res
		}

		// 空错误，即上述的执行 echo __CONNECTED__ 命令返回了的场景
		resCh <- ShellConnUpdate{
			DebugInfo: s.makeDebugInfo("Got the marker, connected successfully"),
		}

		// Got the marker, so we're done.
		// 返回带有 cmd, stdin, stderr, stdout(已包装), cancel
		res.Conn = &ShellConnCustomCmd{
			cmd:    cmd,
			stdin:  stdin,
			stdout: clientStdoutR,
			stderr: stderr,

			ctxCancel: cancel,
		}
		return res

	case <-time.After(connectionTimeout):
		// 等待5s超时后，返回
		res.Err = errors.New("timeout waiting for SSH connection marker")
		return res
	}
}

func (s *ShellTransportCustomCmd) makeDebugInfo(message string) *ShellConnDebugInfo {
	return &ShellConnDebugInfo{
		Message: message,
	}
}

type ShellConnCustomCmd struct {
	// cmd 引用
	cmd *exec.Cmd

	// 输入
	stdin io.WriteCloser
	// 输出
	stdout io.Reader
	// 错误输出
	stderr io.Reader

	// 取消，调用取消时，便会 kill 掉 cmd 的进程
	ctxCancel context.CancelFunc
}

func (s *ShellConnCustomCmd) Stdin() io.Writer {
	return s.stdin
}

func (s *ShellConnCustomCmd) Stdout() io.Reader {
	return s.stdout
}

func (s *ShellConnCustomCmd) Stderr() io.Reader {
	return s.stderr
}

func (s *ShellConnCustomCmd) Close() {
	// Close stdin; normally this is enough for the external process to finish
	// gracefully.
	// 关闭输入
	s.stdin.Close()

	// Cancel context too, so the external process gets killed (closing stdin is
	// not always enough; e.g. after the OS gets suspended for long enough time,
	// and resumed, the connection keeps hanging without it).
	// 强行kill掉cmd的进程
	s.ctxCancel()
}
