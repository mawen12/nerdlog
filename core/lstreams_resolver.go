package core

import (
	"fmt"
	"strings"

	"github.com/dimonomid/nerdlog/shellescape"
	"github.com/dimonomid/ssh_config"
	"github.com/gobwas/glob"
	"github.com/juju/errors"
)

type LStreamsResolver struct {
	params LStreamsResolverParams
}

type LStreamsResolverParams struct {
	// CurOSUser is the current OS username, it's used as the last resort when
	// determining the user for a particular host connection.
	// 当前系统用户名称，在确定特定主机连接的用户时，它是作为最后的手段使用的
	CurOSUser string

	// DefaultTransportMode will be used for all logstreams which don't explicitly
	// define transport.
	// 对于所有未显示定义传输方式的日志流，将使用默认传输模式
	DefaultTransportMode *TransportMode

	// ConfigLogStreams is the nerdlog-specific config, typically coming from
	// ~/.config/nerdlog/logstreams.yaml.
	// nerdlog 特定配置，通常来自于 ~/.config/nerdlog/logstreams.yaml
	ConfigLogStreams ConfigLogStreams

	// SSHConfig is the general SSH config, typically coming from ~/.ssh/config
	// 通用的 SSH 配置，通常位于 ～/.ssh/config
	SSHConfig *ssh_config.Config
}

func NewLStreamsResolver(params LStreamsResolverParams) *LStreamsResolver {
	return &LStreamsResolver{
		params: params,
	}
}

// 标准示例数据 mawen@localhost:22:journalctl
type LogStream struct {
	// Name is an arbitrary string which will be included in log messages as the
	// "lstream" context tag; it must uniquely identify the LogStream.
	Name string

	// NOTE: all fields below are shell-specific; so if at some point we want
	// to support other kinds of backends, we'll probably need to factor all
	// these details to a separate struct like LogStreamShell or something.

	// Transport specifies how we can get shell access to the host containing
	// the logstream.
	Transport ConfigLogStreamShellTransport

	// LogFiles contains a list of files which are part of the logstream, like
	// ["/var/log/syslog", "/var/log/syslog.1"]. The [0]th item is the latest log
	// file [1]st is the previous one, etc. One special case here is journalctl:
	// if [0]th item is "journalctl", then we won't use plain log files, and
	// instead will get the data straight from journalctl.
	//
	// It must contain at least a single item, otherwise LogStream is invalid.
	LogFiles []string

	Options LogStreamOptions
}

// ConfigLogStreamShellTransportSSHLib contains params for the ssh transport
// using internal ssh library.
type ConfigLogStreamShellTransportSSHLib struct {
	Host     ConfigHost
	Jumphost *ConfigHost
}

// ConfigLogStreamShellTransportCustomCmd contains params for the custom
// shell command transport.
type ConfigLogStreamShellTransportCustomCmd struct {
	// See description for ShellTransportCustomCmdParams.ShellCommand
	ShellCommand string

	// See description for ShellTransportCustomCmdParams.EnvOverride
	EnvOverride map[string]string
}

type ConfigLogStreamShellTransportLocalhost struct {
	// No details are needed here
}

// 用于描述 log stream 的传输方式，支持 localhost / ssh-lib / custom-cmd 三种方式
type ConfigLogStreamShellTransport struct {
	SSHLib    *ConfigLogStreamShellTransportSSHLib
	CustomCmd *ConfigLogStreamShellTransportCustomCmd
	Localhost *ConfigLogStreamShellTransportLocalhost
}

type LogStreamOptions struct {
	SudoMode SudoMode

	// ShellInit can contain arbitrary shell commands which will be executed
	// right after connecting to the host. A common use case is setting
	// custom env vars for tests, like: "export TZ=America/New_York", but
	// might be useful outside of tests as well.
	ShellInit []string
}

// SudoMode can be used to configure nerdlog to read log files with "sudo -n".
// See constants below for more details.
// 可使用 sudo -n 来运行 nerdlog_agent.sh 脚本读取日志文件
// sudo -n 是指不提示输入密码的模式，如果 sudo 需要密码，则会直接失败
type SudoMode string

// 支持 none / full 两种模式
const (
	// SudoModeNone is the same as an empty string, and it obviously means that
	// no sudo will be used. It exists as a separate mode to make it possible to
	// override the mode to not use sudo even if some config specifies some
	// non-empty sudo mode.
	// 不使用 sudo 模式
	SudoModeNone SudoMode = "none"

	// SudoModeFull means that the whole nerdlog_agent.sh script will be executed
	// with "sudo -n". Useful for cases when the log files are owned by root but
	// sudo doesn't require a password.
	// 可使用 sudo -n 来运行 nerdlog_agent.sh 脚本读取日志文件
	SudoModeFull SudoMode = "full"

	// If needed, we might implement something like SudoModeGranular, which would
	// mean that the agent script runs without sudo, but then internally it
	// executes some commands with sudo. It would mean a more complicated setup
	// and more maintenance burden and harder to configure sudoers file, so
	// postponed until we actually need it (if at all).
)

var ValidSudoModes = map[SudoMode]struct{}{
	SudoModeNone: {},
	SudoModeFull: {},
}

type ConfigHost struct {
	// Addr is the address to connect to, in the same format which is used by
	// net.Dial. To copy-paste some docs from net.Dial: the address has the form
	// "host:port". The host must be a literal IP address, or a host name that
	// can be resolved to IP addresses. The port must be a literal port number or
	// a service name.
	//
	// Examples: "golang.org:http", "192.0.2.1:http", "198.51.100.1:22".
	Addr string
	// User is the username to authenticate as.
	User string
}

func (ch *ConfigHost) Key() string {
	return fmt.Sprintf("%s@%s", ch.Addr, ch.User)
}

func (ls LogStream) LogFileLast() string {
	// LogFiles must contain at least a single item, so we don't check anything
	// here, and let it panic naturally if the invariant breaks due to some bug.
	return ls.LogFiles[0]
}

func (ls LogStream) LogFilePrev() (string, bool) {
	if len(ls.LogFiles) >= 2 {
		return ls.LogFiles[1], true
	}

	return "", false
}

// Resolve parses the given logstream spec, and returns the mapping from
// LogStream.Name to the corresponding LogStream. Examples of logstream spec are:
//
// - "myuser@myserver.com:22:/var/log/syslog"
// - "myuser@myserver.com:22"
// - "myuser@myserver.com"
// - "myserver.com"

// 解析 --lstreams，并返回名称对应的结果
func (r *LStreamsResolver) Resolve(lstreamsStr string) (map[string]LogStream, error) {
	// 去除首尾空格
	lstreamsStr = strings.TrimSpace(lstreamsStr)
	// 构造保存结果
	parsedLogStreams := map[string]LogStream{}

	// Special case for an empty input: it's allowed and just results in no
	// logstreams.
	// --lstreams = "" 的场景
	if lstreamsStr == "" {
		return parsedLogStreams, nil
	}

	// TODO: when json is supported, splitting by commas will need to be improved.
	// 使用,拆分，目前仅支持逗号拆分
	parts := strings.Split(lstreamsStr, ",")
	// 依次解析
	for i, part := range parts {
		// 去除首尾空格
		part = strings.TrimSpace(part)

		// 不允许 "," 的场景
		if part == "" {
			return nil, errors.Errorf("entry #%d is empty", i+1)
		}

		// 解析单条log stream，此处仅支持标准格式：user@host:port:/path/to/file 或者 user@host:port:/path/to/file:/path/to/file.1
		// 其他格式的字符串，都会返回错误
		cfs, err := r.parseLogStreamSpecEntry(part)
		if err != nil {
			return nil, errors.Annotatef(err, "parsing entry #%d (%s)", i+1, part)
		}

		// 将解析到的结果写入到最终结果中
		for _, ch := range cfs {
			key := ch.Name
			// 不允许重复的 logstream 名称
			if _, exists := parsedLogStreams[key]; exists {
				return nil, errors.Errorf("the logstream %s is present at least twice", key)
			}

			parsedLogStreams[key] = ch
		}
	}

	return parsedLogStreams, nil
}

// draftLogStream is a draft version of LogStream; it's used as temporary
// storage in the process of resolving logstreams.
type draftLogStream struct {
	name     string
	host     ConfigHost
	jumphost *ConfigHost
	logFiles []string
	options  ConfigLogStreamOptions
}

// parseLogStreamSpecEntry parses a single logstream spec entry like
// "myuser@myserver.com:22:/var/log/syslog", or "myserver.com", or
// "myserver-*", and returns the corresponding LogStream-s. Note that the spec
// might contain a glob, in which case we might return more than 1 LogStream.
// If the glob didn't match anything, an error is returned.
//
// TODO: it should take a predefined config, to support globs

// 解析单条 log stream，比如 myuser@myserver.com:22:/var/log/syslog
// 如果是 myserver-* 这种带有通配符的格式，那么会返回多条 LogStream，如果通配符没有匹配任何主机，则会返回错误
func (r *LStreamsResolver) parseLogStreamSpecEntry(s string) ([]LogStream, error) {
	parts, err := shellescape.Parse(s)
	if err != nil {
		return nil, errors.Trace(err)
	}

	var plstream *parsedLStream
	var jhconf *ConfigHost
	var logFiles []string

	curFlag := ""
	for _, part := range parts {
		// 处理logstream 为 - 开头的场景
		if curFlag == "" && len(part) > 0 && part[0] == '-' {
			curFlag = part
			continue
		}

		switch curFlag {
		// 处理 -J/--jumphost，即其支持指定跳板机
		case "-J", "--jumphost":
			// 解析单条log stream，此处仅支持标准格式：user@host:port:/path/to/file 或者 user@host:port:/path/to/file:/path/to/file.1
			// 其他格式的字符串，都会返回错误
			jhparsed, err := r.parseLStreamStr(part)
			if err != nil {
				return nil, errors.Annotatef(err, "parsing %q as a jumphost", part)
			}

			//if jhparsed.logFileLast != "" || jhparsed.logFilePrev != "" {
			//return nil, errors.Annotatef(err, "jumphost config shouldn't contain files")
			//}

			// 读取端口
			jhPort := jhparsed.port

			// WARN 目前暂不支持指定超过1个文件的场景
			if len(jhparsed.colonParts) > 1 {
				return nil, errors.Errorf("parsing %q as a jumphost: too many colons", part)
			}

			// 构造地址和用户
			jhconf = &ConfigHost{
				Addr: fmt.Sprintf("%s:%s", jhparsed.hostname, jhPort),
				User: jhparsed.user,
			}
		// 不存在-的场景
		case "":
			var err error
			// 解析单条log stream，此处仅支持标准格式：user@host:port:/path/to/file 或者 user@host:port:/path/to/file:/path/to/file.1
			// 其他格式的字符串，都会返回错误
			plstream, err = r.parseLStreamStr(part)
			if err != nil {
				return nil, errors.Annotatef(err, "parsing %q as a logstream", part)
			}

			// 当其指定了文件部分，便将日志文件写入到 logfiles 中
			if len(plstream.colonParts) > 0 {
				logFiles = append(logFiles, plstream.colonParts[0])
			}

			if len(plstream.colonParts) > 1 {
				logFiles = append(logFiles, plstream.colonParts[1])
			}

			// WARN 目前暂不支持指定超过2个文件的场景
			if len(plstream.colonParts) > 2 {
				return nil, errors.Errorf("%q: too many colons", part)
			}
		// 其他场景，暂不支持，比如 -*
		default:
			return nil, errors.Errorf("invalid flag %s", curFlag)
		}

		curFlag = ""
	}

	// 没有解析出合法的logstream，此时报错
	if plstream == nil {
		return nil, errors.Errorf("no logstream specified in %q", s)
	}

	// 构建草稿的 log stream
	lstreams := []draftLogStream{
		{
			// 使用原始的名称作为name
			name: s,

			// 构造 host
			host: ConfigHost{
				Addr: fmt.Sprintf("%s:%s", plstream.hostname, plstream.port),
				User: plstream.user,
			},

			jumphost: jhconf,

			logFiles: logFiles,
		},
	}

	// Expand from nerdlog config.
	// nerdlog 特定配置，通常来自于 ~/.config/nerdlog/logstreams.yaml
	// 读取该配置对用户指定的数据进行完善
	lstreams, err = expandFromLogStreamsConfig(
		lstreams, r.params.ConfigLogStreams,
	)
	if err != nil {
		return nil, errors.Annotatef(err, "expanding from nerdlog config")
	}

	// Now that we filled things from the nerdlog logstreams config, we should
	// set the default transport mode for all the logstreams which didn't have it
	// specified explicitly in the config.
	//
	// We must do it before expanding things from the ssh config, because
	// expandFromLogStreamsConfig's behavior depends on the Transport: if it's
	// empty or "ssh-lib", then it'll expand connection details (host, port and
	// user); otherwise these connection details will be left intact even if
	// they're empty, because we want to leave all this to whatever external
	// command we'll be using for the transport.
	// 对于所有未设置传输模式的，使用默认的传输模式，
	defaultTransportSpec := r.params.DefaultTransportMode.String()
	for i := range lstreams {
		if lstreams[i].options.Transport == "" {
			lstreams[i].options.Transport = defaultTransportSpec
		}
	}

	// Expand from ssh config.
	// 读取 ssh config，并将其转换为 logstreams 配置
	lsConfigFromSSHConfig, err := sshConfigToLSConfig(r.params.SSHConfig)
	if err != nil {
		return nil, errors.Annotatef(err, "parsing ssh config")
	}

	// 基于 ssh 配置完善当前配置
	lstreams, err = expandFromLogStreamsConfig(
		lstreams, lsConfigFromSSHConfig,
	)
	if err != nil {
		return nil, errors.Annotatef(err, "expanding from ssh config")
	}

	// Set defaults.
	// 完善连接部分的缺失信息，比如默认端口22，用户为当前系统用户
	lstreams, err = setLogStreamsConnDefaults(lstreams, r.params.CurOSUser)
	if err != nil {
		return nil, errors.Annotatef(err, "setting defaults")
	}
	// 完善日志部分的缺失信息，对于没有指定日志文件的，添加 auto
	lstreams, err = setLogStreamsFileDefaults(lstreams)
	if err != nil {
		return nil, errors.Annotatef(err, "setting defaults")
	}

	// Check if some of the items were clearly indended to be globs matching
	// something (those with asterisks in them), and didn't match anything.
	// 对于那些带有通配符的项，检查其是否匹配到了任何主机，如果没有匹配到任何主机，则返回错误
	for _, ls := range lstreams {
		// TODO: would perhaps be useful to implement a function like IsValidDialAddress,
		// which checks a bunch of other things, but for now, a single asterisk check
		// will do.
		if strings.Contains(ls.host.Addr, "*") {
			return nil, errors.Errorf("glob %q didn't match anything (having address %q)", s, ls.host.Addr)
		}
	}

	// Convert draft logstreams to the actual ones.
	// 将草稿 log stream 转换为正式的 log stream
	ret := make([]LogStream, 0, len(lstreams))
	for _, ls := range lstreams {

		var transport ConfigLogStreamShellTransport
		// Using kinda hackish logic: if the hostname part is "localhost", then
		// ignore the port and user completely, and just use local shell.
		//
		// Maybe we need to treat some other strings similarly, like
		// "localhost.localdomain", or "127.0.0.1", or "::1"; but not sure if it
		// would actually bring any value. So for now, only "localhost" has this
		// special treatment (which is also the default when one opens Nerdlog for
		// the first time).
		// 当主机名部分为 localhost 时，忽略端口和用户，直接使用本地 shell
		// TODO 也许我们还需要类似地处理其他字符串，比如 localhost.localdomain，或者 127.0.0.1，或者 ::1；但不确定它是否真的有价值
		// 所以目前，只有 localhost 才有这种特殊处理（当第一次打开 Nerdlog 时，它也是默认值）
		if strings.HasPrefix(ls.host.Addr, "localhost:") {
			// Use local shell 
			// 使用本地 shell
			transport = ConfigLogStreamShellTransport{
				Localhost: &ConfigLogStreamShellTransportLocalhost{},
			}
		} else {
			// 解析传输模式
			tm, err := ParseTransportMode(ls.options.Transport)
			if err != nil {
				return nil, errors.Annotatef(err, "parsing transport mode for %s", ls.name)
			}

			// Use ssh-lib
			if tm.Kind() == TransportModeKindSSHLib {
				// Use internal ssh library
				// 使用内部 ssh 库
				transport = ConfigLogStreamShellTransport{
					SSHLib: &ConfigLogStreamShellTransportSSHLib{
						Host:     ls.host,
						Jumphost: ls.jumphost,
					},
				}
			} else {
				// Use external custom command.
				// 使用外部自定义命令
				parsedAddr, err := parseAddr(ls.host.Addr)
				if err != nil {
					return nil, errors.Annotatef(err, "parsing addr %s for external custom command", ls.host.Addr)
				}

				// 构造环境变量 NLHOST / NLPORT / NLUSER
				envOverride := map[string]string{
					"NLHOST": parsedAddr.host,
				}
				if parsedAddr.port != "" { // 仅当 port 存在时，才设置 NLPORT
					envOverride["NLPORT"] = parsedAddr.port
				}
				if ls.host.User != "" { // 仅当 user 存在时，才设置 NLUSER
					envOverride["NLUSER"] = ls.host.User
				}

				transport = ConfigLogStreamShellTransport{
					CustomCmd: &ConfigLogStreamShellTransportCustomCmd{
						ShellCommand: tm.CustomShellCommand(),
						EnvOverride:  envOverride,
					},
				}
			}
		}

		ret = append(ret, LogStream{
			Name:      ls.name,
			Transport: transport,
			LogFiles:  ls.logFiles,
			Options: LogStreamOptions{
				SudoMode:  ls.options.SudoMode,
				ShellInit: ls.options.ShellInit,
			},
		})
	}

	return ret, nil
}

// 示例：jack@abc.com@22:/path/to/logfile:/path/to/logfile.1
// - hostname: jack
// - user: jack
// - port: 22
// - colonParts: [/path/to/logfile, /path/to/logfile.1]
type parsedLStream struct {
	hostname string
	user     string
	port     string

	colonParts []string
}

// 将字符串解析为 log stream，例如 user@hostname:/path/to/logfile:/path/to/logfile.1
func (r *LStreamsResolver) parseLStreamStr(s string) (*parsedLStream, error) {
	// Parsing the logstream descriptor like
	// "user@hostname:/path/to/logfile:/path/to/logfile.1"

	// Parse user, if present
	username := ""
	// 解析@的位置，@之后是主机
	atIdx := strings.IndexRune(s, '@')
	// 没有提供username时报错
	if atIdx == 0 {
		return nil, errors.Errorf("username is empty")
	} else if atIdx > 0 {
		username = s[:atIdx] // 解析提取username
		s = s[atIdx+1:]      // 去除username@部分
	}

	// 解析port
	port := ""
	colonParts := []string{}
	// port 是 hostanme:port:path/to/logfile 的格式
	parts := strings.Split(s, ":")
	// 如果为0，代表整体格式错误，因为至少为1
	if len(parts) == 0 || parts[0] == "" {
		return nil, errors.Errorf("no hostname")
	}

	// 超过1，则取1
	if len(parts) > 1 {
		port = parts[1]
	}

	// 超过2，代表用户制定了多个user@hostname:/path/to/logfile:/path/to/logfile.1
	if len(parts) > 2 {
		colonParts = parts[2:]
	}

	// 返回结果
	return &parsedLStream{
		hostname:   parts[0],
		user:       username,
		port:       port,
		colonParts: colonParts,
	}, nil
}

type ConfigLogStreamWKey struct {
	// Key is the key at which the corresponding ConfigLogStream was
	// stored in the ConfigLogStreams map.
	Key string

	ConfigLogStream
}

// expandFromLogStreamsConfig goes through each of the logstreams, and
// potentially expands every item as per the provided config.
// 基于本地的 ~/.config/nerdlog/logstreams.yaml 完善当前配置，并返回完善后的配置
func expandFromLogStreamsConfig(
	logStreams []draftLogStream,
	lsConfig ConfigLogStreams,
) ([]draftLogStream, error) {
	// If there's no config, cut it short.
	// 未指定时，则直接返回
	if lsConfig == nil {
		return logStreams, nil
	}

	var ret []draftLogStream

	// 循环处理每一条 log stream
	for i, ls := range logStreams {
		var matchedConfigItems []*ConfigLogStreamWKey
		// 解析 host:port
		addr, err := parseAddr(ls.host.Addr)
		if err != nil {
			return nil, errors.Annotatef(err, "logstream #%d, parsing address", i+1)
		}

		// 假设其是一个 glob 模式，即 myserver-*
		globPattern := addr.host
		// 编译
		matcher, err := glob.Compile(globPattern)
		if err != nil {
			return nil, errors.Annotatef(err, "logstream #%d, parsing hostname %q as a glob pattern", i+1, addr.host)
		}

		// 读取所有的key，进行匹配
		for _, key := range lsConfig.Keys() {
			if matcher.Match(key) {
				// 将匹配到的结果与 key 一起保存
				matchedConfigItems = append(matchedConfigItems, &ConfigLogStreamWKey{
					Key:             key,
					ConfigLogStream: lsConfig[key],
				})
			}
		}

		// If there's no match, just copy that logstream unchanged.
		// 如果没有找到匹配项，则直接返回当前 log stream
		if len(matchedConfigItems) == 0 {
			ret = append(ret, ls)
			continue
		}

		// There are some matches, so we need to expand things.
		// 处理所有匹配到的项
		for _, matchedItem := range matchedConfigItems {
			lsCopy := ls
			addrCopy := addr

			// Always override the name with the key from the config.
			// 使用具体的配置替换通配符
			lsCopy.name = strings.Replace(lsCopy.name, globPattern, matchedItem.Key, -1)

			// 未指定传输模式，或者指定为 ssh-lib 的场景
			if lsCopy.options.Transport == "" || lsCopy.options.Transport == "ssh-lib" {
				// Overwrite the host address (since what we've had might be a glob):
				// either with the Hostname if it's specified explicitly, or if not, then
				// with the item key.
				// 使用具体的覆盖通配符的主机名
				if matchedItem.Hostname != "" {
					addrCopy.host = matchedItem.Hostname
				} else {
					addrCopy.host = matchedItem.Key
				}

				// Everything else we'll only override if it's not specified already.
				// 当没有指定端口时，使用配置中的端口
				if addrCopy.port == "" {
					addrCopy.port = matchedItem.Port
				}

				// 当没有指定用户时，使用配置中的用户
				if lsCopy.host.User == "" {
					lsCopy.host.User = matchedItem.User
				}
			} else {
				// Transport is using some external command, so we don't fill in port
				// etc, but we still replace the host with the matched item's key
				// (because or original host might have been a glob)
				// 处理使用外部命令的场景，此处仅替换主机名，因为原始主机名可能是一个通配符
				addrCopy.host = matchedItem.Key
			}

			// For non-connection details, override them if not specified already.
			// 但没有指定sudo模式时，采用配置中的sudo模式
			if lsCopy.options.SudoMode == "" {
				lsCopy.options.SudoMode = matchedItem.Options.EffectiveSudoMode()
			}

			// 当没有指定 shell init 时，使用配置中的 shell init
			if lsCopy.options.ShellInit == nil {
				lsCopy.options.ShellInit = matchedItem.Options.ShellInit
			}

			// 当没有指定传输模式时，使用配置中的传输模式
			if lsCopy.options.Transport == "" {
				lsCopy.options.Transport = matchedItem.Options.Transport
			}

			// 当没有指定日志文件时，使用配置中的日志文件
			if len(lsCopy.logFiles) == 0 {
				lsCopy.logFiles = matchedItem.LogFiles
			}

			// 更新地址
			lsCopy.host.Addr = fmt.Sprintf("%s:%s", addrCopy.host, addrCopy.port)

			ret = append(ret, lsCopy)
		}
	}

	return ret, nil
}

// setLogStreamsConnDefaults goes through each of the logstreams, and fills in
// missing pieces for which it knows the defaults for how to connect to the
// hosts: port 22, user as the current OS user.
//
// Note that it shouldn't be used when we're utilizing external custom command:
// in this case, we want to leave it all up to that external command.
// 针对每一条 log stream，完善连接部分的缺失信息，比如默认端口22，用户为当前系统用户
func setLogStreamsConnDefaults(
	logStreams []draftLogStream,
	osUser string,
) ([]draftLogStream, error) {
	ret := make([]draftLogStream, 0, len(logStreams))

	for i, ls := range logStreams {
		// Only fill in default port and user if the custom transport command is
		// not set; because if it's set, we'll want to defer all the defaults to
		// that external command.
		// 仅当未指定自定义传输命令时，才完善默认端口和用户，默认的传输模式为 ssh-lib
		if ls.options.Transport == "ssh-lib" {
			port, err := portFromAddr(ls.host.Addr)
			if err != nil {
				return nil, errors.Annotatef(err, "logstream #%d, getting port", i+1)
			}

			// 完善端口，默认22
			if port == "" {
				ls.host.Addr += "22"
			}

			// 完善用户，默认当前系统用户
			if ls.host.User == "" {
				ls.host.User = osUser
			}
		}

		ret = append(ret, ls)
	}

	return ret, nil
}

// setLogStreamsFileDefaults goes through each of the logstreams, and fills in
// missing non-connection pieces, such as default log files.
// 针对每一条 log stream，完善日志部分的缺失信息
func setLogStreamsFileDefaults(logStreams []draftLogStream) ([]draftLogStream, error) {
	ret := make([]draftLogStream, 0, len(logStreams))

	for _, ls := range logStreams {
		// 如果没有指定任何日志文件时，添加 auto
		if len(ls.logFiles) == 0 {
			// Will be autodetected by the agent script.
			ls.logFiles = append(ls.logFiles, "auto")
		}

		// 如果仅指定了1个日志文件时，添加 auto
		if len(ls.logFiles) == 1 {
			// Will be autodetected by the agent script.
			ls.logFiles = append(ls.logFiles, "auto")
		}

		ret = append(ret, ls)
	}

	return ret, nil
}

type parsedAddr struct {
	host string
	port string
}

// 解析地址，地址格式为：host:port，当格式不正确时，返回错误
func parseAddr(addr string) (parsedAddr, error) {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return parsedAddr{}, errors.Errorf("not a valid addr %q, expected host:port", addr)
	}

	return parsedAddr{
		host: parts[0],
		port: parts[1],
	}, nil
}

// hostnameFromAddr takes an address like net.Dial takes, in the form of
// "host:port", and returns the host part.
func hostnameFromAddr(addr string) (string, error) {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return "", errors.Errorf("not a valid addr %q, expected host:port", addr)
	}

	return parts[0], nil
}

// portFromAddr takes an address like net.Dial takes, in the form of
// "host:port", and returns the port part.
func portFromAddr(addr string) (string, error) {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return "", errors.Errorf("not a valid addr %q, expected host:port", addr)
	}

	return parts[1], nil
}

// 读取 ssh config，并将其转换为 logstreams 配置
func sshConfigToLSConfig(sshConfig *ssh_config.Config) (ConfigLogStreams, error) {
	if sshConfig == nil {
		return nil, nil
	}

	// 构造返回结果
	ret := make(ConfigLogStreams, len(sshConfig.Hosts))

	for _, host := range sshConfig.Hosts {
		// 忽略没有任何 pattern 的场景
		if len(host.Patterns) == 0 {
			continue
		}

		// 读取 host 的名称
		name := host.Patterns[0].String()
		if name == "" {
			continue
		}

		// If it's a pattern, ignore it
		// (there might be valid use cases where we'd want to use them, but
		// not bothering for now)
		// 如果是通配符，则忽略
		if strings.ContainsAny(name, "*?[]") {
			continue
		}

		// 读取 hostname、port、user
		hostname, _ := sshConfig.Get(name, "HostName")
		port, _ := sshConfig.Get(name, "Port")
		user, _ := sshConfig.Get(name, "User")

		// 如果三者都为空，则忽略
		if hostname == "" && port == "" && user == "" {
			// We can't get anything useful out of this entry anyway, so don't add it
			continue
		}

		// 保存结果
		ret[name] = ConfigLogStream{
			Hostname: hostname,
			Port:     port,
			User:     user,
		}
	}

	return ret, nil
}
