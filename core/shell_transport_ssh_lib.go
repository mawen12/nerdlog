package core

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dimonomid/nerdlog/log"
	"github.com/juju/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// ShellTransportSSHLib implements ShellTransport over SSH.
// ShellTransportSSHLib 通过 SSH 实现 ShellTransport。
type ShellTransportSSHLib struct {
	params ShellTransportSSHLibParams
}

// 编译时断言，确保 ShellTransportSSHLib 实现了 ShellTransport 接口
var _ ShellTransport = &ShellTransportSSHLib{}

func NewShellTransportSSHLib(params ShellTransportSSHLibParams) *ShellTransportSSHLib {
	// 构建自定义的命令空间日志记录器
	params.Logger = params.Logger.WithNamespaceAppended("TransportSSHLib")

	return &ShellTransportSSHLib{
		params: params,
	}
}

type ShellTransportSSHLibParams struct {
	// SSHKeys specifies paths to ssh keys to try, in the given order, until
	// an existing key is found.
	// SSHKeys 指定要尝试的 ssh 密钥的路径，按给定顺序，直到找到现有密钥。
	SSHKeys []string

	// 连接详情
	ConnDetails ConfigLogStreamShellTransportSSHLib

	// 日志，该日志的命名空间已附加 "TransportSSHLib"
	Logger *log.Logger
}

// 尝试连接到 shell。它只是生成一个 goroutine 并立即返回，
// 该 channel 是一个只能发送数据的通道，其用于将稍后连接结果（或者可能是对附加数据的请求，例如密码短语）传递到提供的通道。
// 启动连接，其上游是 lstream_client
func (st *ShellTransportSSHLib) Connect(resCh chan<- ShellConnUpdate) {
	go st.doConnect(resCh)
}

// 创建调试信息
func (st *ShellTransportSSHLib) makeDebugInfo(message string) *ShellConnDebugInfo {
	return &ShellConnDebugInfo{
		Message: message,
	}
}

// 负责启动连接，而且将执行过程发送到 DebugInfo，执行出错或成功后，将结果写入到 Result 上
func (st *ShellTransportSSHLib) doConnect(resCh chan<- ShellConnUpdate) (res ShellConnResult) {
	logger := st.params.Logger

	// 核心，负责将结果写入到 resCh 上
	defer func() {
		if res.Err != nil {
			logger.Errorf("Connection failed: %s", res.Err)
		}

		resCh <- ShellConnUpdate{
			Result: &res,
		}
	}()

	connDetails := st.params.ConnDetails

	// 发送调试信息，表示正在尝试使用内部 ssh 库连接
	resCh <- ShellConnUpdate{
		DebugInfo: st.makeDebugInfo(fmt.Sprintf(
			"Trying to connect using internal ssh library to addr: %s, user: %s",
			connDetails.Host.Addr, connDetails.Host.User,
		)),
	}

	var sshClient *ssh.Client // golang 内部库

	// 读取客户端配置
	conf, err := st.getClientConfig(resCh, logger, connDetails.Host.User)
	if err != nil {
		res.Err = errors.Annotatef(err, "getting ssh client for %s", connDetails.Host.User)
		return res
	}

	resCh <- ShellConnUpdate{
		DebugInfo: st.makeDebugInfo(fmt.Sprintf("Got client config: %s", conf.Descr)),
	}

	if connDetails.Jumphost != nil { // 处理跳板机场景
		logger.Infof("Connecting via jumphost")
		// Use jumphost
		jumphost, err := st.getJumphostClient(resCh, logger, connDetails.Jumphost)
		if err != nil {
			logger.Errorf("Jumphost connection failed: %s", err)
			res.Err = errors.Annotatef(err, "getting jumphost client")
			return res
		}

		conn, err := dialWithTimeout(jumphost, "tcp", connDetails.Host.Addr, connectionTimeout)
		if err != nil {
			res.Err = errors.Annotatef(err, conf.Descr)
			return res
		}

		authConn, chans, reqs, err := ssh.NewClientConn(conn, connDetails.Host.Addr, conf.ClientConfig)
		if err != nil {
			res.Err = errors.Annotatef(err, conf.Descr)
			return res
		}

		sshClient = ssh.NewClient(authConn, chans, reqs)
	} else {
		// 直接连接
		logger.Infof("Connecting to %s (%+v)", connDetails.Host.Addr, conf)
		var err error
		// 创建ssh连接
		sshClient, err = ssh.Dial("tcp", connDetails.Host.Addr, conf.ClientConfig)
		if err != nil {
			res.Err = errors.Annotatef(err, conf.Descr)
			return res
		}
	}

	shellBin := "/bin/sh"

	resCh <- ShellConnUpdate{
		DebugInfo: st.makeDebugInfo(fmt.Sprintf("Connected, creating pipes and starting %s", shellBin)),
	}
	logger.Infof("Connected to %s", connDetails.Host.Addr)

	// 创建 ssh 会话
	sshSession, err := sshClient.NewSession()
	if err != nil {
		res.Err = errors.Annotatef(err, conf.Descr)
		return res
	}

	// 获取 stdin 管道
	stdinBuf, err := sshSession.StdinPipe()
	if err != nil {
		res.Err = errors.Annotatef(err, conf.Descr)
		return res
	}

	// 获取 stdout 管道
	stdoutBuf, err := sshSession.StdoutPipe()
	if err != nil {
		res.Err = errors.Annotatef(err, conf.Descr)
		return res
	}

	// 获取 stderr 管道
	stderrBuf, err := sshSession.StderrPipe()
	if err != nil {
		res.Err = errors.Annotatef(err, conf.Descr)
		return res
	}

	// 执行 /bin/sh
	err = sshSession.Start(shellBin)
	if err != nil {
		res.Err = errors.Annotatef(err, conf.Descr)
		return res
	}

	// 返回带有 client, session, stdin, stdout, stderr 的连接结果
	res.Conn = &ShellConnSSHLib{
		sshClient:  sshClient,
		sshSession: sshSession,

		stdinBuf:  stdinBuf,
		stdoutBuf: stdoutBuf,
		stderrBuf: stderrBuf,
	}

	return res
}

// dialWithTimeout is a hack needed to get a timeout for the ssh client.
// https://stackoverflow.com/questions/31554196/ssh-connection-timeout
//
// It's possible we could accomplish the same thing by using NewClient() with Conn.SetDeadline(), but that requires
// some refactoring.
func dialWithTimeout(client *ssh.Client, protocol, hostAddr string, timeout time.Duration) (net.Conn, error) {
	finishedChan := make(chan net.Conn)
	errChan := make(chan error)
	go func() {
		conn, err := client.Dial(protocol, hostAddr)
		if err != nil {
			errChan <- err
			return
		}
		finishedChan <- conn
	}()

	select {
	case conn := <-finishedChan:
		return conn, nil

	case err := <-errChan:
		return nil, errors.Trace(err)

	case <-time.After(connectionTimeout):
		// Don't close the connection here since it's reused
		return nil, errors.New("ssh client dial timed out")
	}
}

type ClientConfigWMeta struct {
	// ClientConfig is the actual client config.
	ClientConfig *ssh.ClientConfig

	// Descr is a human-readable string which is useful to include in any
	// error messages about this SSH connection.
	Descr string
}

type AuthMethodWMeta struct {
	AuthMethod ssh.AuthMethod

	// Descr is a human-readable string which is useful to include in any
	// error messages about this SSH connection.
	Descr string
}

func (st *ShellTransportSSHLib) getClientConfig(resCh chan<- ShellConnUpdate, logger *log.Logger, username string) (*ClientConfigWMeta, error) {
	auth, err := st.getSSHAuthMethod(resCh, logger)
	if err != nil {
		return nil, errors.Trace(err)
	}

	return &ClientConfigWMeta{
		ClientConfig: &ssh.ClientConfig{
			User: username,
			Auth: []ssh.AuthMethod{auth.AuthMethod},

			// TODO: fix it
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),

			Timeout: connectionTimeout,
		},
		Descr: auth.Descr,
	}, nil
}

var (
	sshAuthMethodShared    *AuthMethodWMeta
	sshAuthMethodSharedMtx sync.Mutex
)

func (st *ShellTransportSSHLib) getSSHAuthMethod(resCh chan<- ShellConnUpdate, logger *log.Logger) (*AuthMethodWMeta, error) {
	sshAuthMethodSharedMtx.Lock()
	defer sshAuthMethodSharedMtx.Unlock()

	// 检查authMethod是否已缓存
	if sshAuthMethodShared != nil {
		return sshAuthMethodShared, nil
	}

	// Try ssh-agent first
	var sshAgentErr error
	// 读取 SSH_AUTH_SOCK 环境变量
	sshAuthSock := os.Getenv("SSH_AUTH_SOCK")
	if sshAuthSock != "" {
		logger.Infof("Trying ssh-agent via SSH_AUTH_SOCK=%s", sshAuthSock)
		// 连接到 ssh-agent
		sshAgent, err := net.Dial("unix", sshAuthSock)
		if err != nil {
			logger.Infof("Failed to connect to ssh-agent: %s", err.Error())
			sshAgentErr = errors.Annotatef(err, "using SSH_AUTH_SOCK env var")
		} else {
			// 使用 ssh-agent 进行身份验证
			sshAuthMethodShared = &AuthMethodWMeta{
				AuthMethod: ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers),
				Descr:      "using ssh-agent",
			}
			return sshAuthMethodShared, nil
		}
	} else {
		sshAgentErr = errors.Errorf("SSH_AUTH_SOCK env var is empty")
		logger.Infof("SSH_AUTH_SOCK not set; skipping ssh-agent")
	}

	// Fall back to private key
	// 退回到私钥
	logger.Infof("Fallback to parsing ssh key...")

	var keyPath string
	var keyData []byte
	var errBuilder strings.Builder
	// 查找第一个存在的密钥文件
	for _, keyPath = range st.params.SSHKeys {
		var err error
		keyData, err = os.ReadFile(keyPath)
		if err != nil {
			if errBuilder.Len() > 0 {
				errBuilder.WriteString(", ")
			}
			errBuilder.WriteString(fmt.Sprintf("%s: %s", keyPath, err.Error()))
			continue
		}

		// Found the key file.
		break
	}

	// 当没有找到任何密钥文件时，返回错误
	if len(keyData) == 0 {
		return nil, errors.Errorf(
			"failed to read key data from any of the following: %s (%s)",
			st.params.SSHKeys,
			errBuilder.String(),
		)
	}

	// 解析私钥
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		if _, ok := err.(*ssh.PassphraseMissingError); ok {
			// We need a passphrase to decrypt the private key. Request it from
			// the client code.
			passphraseCh := make(chan string, 1)

			// 发送数据请求，要求提供密码短语
			resCh <- ShellConnUpdate{
				DataRequest: &ShellConnDataRequest{
					Title:      "SSH key is passphrase-protected",
					Message:    fmt.Sprintf("Unable to use ssh-agent: %s, falling back to ssh keys.\nPlease enter passphrase for %s.\nAlternatively, use ssh-agent, and make sure the SSH_AUTH_SOCK environment variable is set correctly.\nTo use a different ssh key, provide it with the --ssh-key flag.", sshAgentErr.Error(), keyPath),
					DataKind:   ShellConnDataKindPassword,
					ResponseCh: passphraseCh,
				},
			}

			// Now wait for the client code to provide the passphrase.
			//
			// TODO: support teardown; as of now, if the user tries to exit the app,
			// it'll be stuck on the "Closing connections" stage, until the Ctrl+C is
			// pressed.
			// 等待客户端代码提供密码短语
			passphrase := <-passphraseCh

			var err error
			// 使用提供的密码短语解析私钥
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(passphrase))
			if err != nil {
				// Something has failed even with the provided passphrase.
				// We don't implement any retries here in case of typos, because the
				// whole connection will be retried, and we'll naturally ask for the
				// passphrase again.
				return nil, errors.Annotatef(err, "parsing private key from %s with the given passphrase", keyPath)
			}
		} else {
			return nil, errors.Annotatef(err, "parsing private key from %s", keyPath)
		}
	}

	logger.Infof("Using private key from %s", keyPath)
	// 保存认证方法到共享变量
	sshAuthMethodShared = &AuthMethodWMeta{
		AuthMethod: ssh.PublicKeys(signer),
		Descr:      fmt.Sprintf("using key %s", keyPath),
	}
	return sshAuthMethodShared, nil
}

var (
	jumphostsShared    = map[string]*ssh.Client{}
	jumphostsSharedMtx sync.Mutex
)

// 跳板机
func (st *ShellTransportSSHLib) getJumphostClient(resCh chan<- ShellConnUpdate, logger *log.Logger, jhConfig *ConfigHost) (*ssh.Client, error) {
	jumphostsSharedMtx.Lock()
	defer jumphostsSharedMtx.Unlock()

	key := jhConfig.Key()
	jh := jumphostsShared[key]
	if jh == nil {
		logger.Infof("Connecting to jumphost... %+v", jhConfig)

		parts := strings.Split(jhConfig.Addr, ":")
		if len(parts) != 2 {
			return nil, errors.Errorf("malformed jumphost address %q", jhConfig.Addr)
		}

		addrs, err := net.LookupHost(parts[0])
		if err != nil {
			return nil, errors.Trace(err)
		}

		if len(addrs) != 1 {
			return nil, errors.New("Address not found")
		}

		conf, err := st.getClientConfig(resCh, logger, jhConfig.User)
		if err != nil {
			return nil, errors.Trace(err)
		}

		jh, err = ssh.Dial("tcp", jhConfig.Addr, conf.ClientConfig)
		if err != nil {
			return nil, errors.Trace(err)
		}

		jumphostsShared[key] = jh

		logger.Infof("Jumphost ok")
	}

	return jh, nil
}

// ShellConnSSHLib implements ShellConn for SSH.
type ShellConnSSHLib struct {
	// ssh 客户端
	sshClient  *ssh.Client
	// ssh 会话
	sshSession *ssh.Session

	// 输入
	stdinBuf  io.WriteCloser
	// 输出
	stdoutBuf io.Reader
	// 错误输出
	stderrBuf io.Reader
}

var _ ShellConn = &ShellConnSSHLib{}

func (c *ShellConnSSHLib) Stdin() io.Writer {
	return c.stdinBuf
}

func (c *ShellConnSSHLib) Stdout() io.Reader {
	return c.stdoutBuf
}

func (c *ShellConnSSHLib) Stderr() io.Reader {
	return c.stderrBuf
}

// Close closes underlying SSH connection.
// 关闭
func (c *ShellConnSSHLib) Close() {
	// 关闭输入
	c.stdinBuf.Close()
	// 关闭会话
	c.sshSession.Close()
	// 关闭客户端
	c.sshClient.Close()
}
