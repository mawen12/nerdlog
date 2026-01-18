package core

import "sort"

// 为了 lstream 和它们解析后的配置
type ConfigLogStreams map[string]ConfigLogStream

type ConfigLogStream struct {
	// HostAddr is the actual host to connect to.
	//
	// If empty, we'll resort to the ssh config, and if there's no info for that
	// host either, we'll try to get host addr from the key in
	// ConfigLogStreams.LogStreams.
	// 实际要连接的主机地址。
	//
	// 如果为空，我们将求助于 ssh 配置，如果该主机也没有信息，我们将尝试从
	// ConfigLogStreams.LogStreams 中的密钥获取主机地址。
	Hostname string `yaml:"hostname"`

	// Port is the actual port to connect to. If empty, the same overriding rules
	// apply.
	// 实际要连接的端口。如果为空，同样的覆盖规则适用。
	Port string `yaml:"port"`

	// User is the user to authenticate as. If empty, same overriding rules
	// apply.
	// 要认证的用户。如果为空，同样的覆盖规则适用。
	User string `yaml:"user"`

	// TODO: optional Jumphost configuration, also with addr and user.

	// LogFiles contains a list of files which are part of the logstream, like
	// ["/var/log/syslog", "/var/log/syslog.1"]. The [0]th item is the latest log
	// file [1]st is the previous one, etc.
	//
	// During the final usage (after resolving everything), it must contain at
	// least a single item, otherwise LogStream is invalid. However in the configs,
	// it's optional (and eventually, if empty, will be set to default values by
	// the LStreamsResolver).
	// 包含属于该 logstream 的文件列表，例如 ["/var/log/syslog", "/var/log/syslog.1"]。
	// 第[0]个项目是最新的日志文件，[1]是前一个，依此类推。
	//
	// 在最终使用过程中（解析所有内容后），它必须至少包含一个项目，否则 LogStream 无效。
	// 但是在配置中，它是可选的（最终，如果为空，将由 LStreamsResolver 设置为默认值）。
	LogFiles []string `yaml:"log_files"`

	// 额外的选项
	Options ConfigLogStreamOptions `yaml:"options"`
}

// ConfigLogStreamOptions contains additional options for a particular logstream.
// 指定特定 logstream 的额外选项。
type ConfigLogStreamOptions struct {
	// Transport overrides the default transport option; the format is exactly the
	// same as in the transport option: "ssh-lib", "ssh-bin", or "custom:foo bar baz".
	// 传输模式，支持 ssh-lib、ssh-bin 或 custom:foo bar baz 格式。
	Transport string `yaml:"transport,omitempty"`

	// Sudo is a shortcut for SudoMode: if Sudo is true, it's an equivalent of
	// setting SudoMode to SudoModeFull.
	// 是否使用 sudo 模式，等同于 SudoMode
	Sudo bool `yaml:"sudo,omitempty"`

	// SudoMode can be used to configure nerdlog to read log files with "sudo -n".
	// See constants for the SudoMode type for more details.
	// 是否使用 sudo 模式，配置 nerdlog 使用 "sudo -n" 读取日志文件。
	SudoMode SudoMode `yaml:"sudo_mode,omitempty"`

	// ShellInit can contain arbitrary shell commands which will be executed
	// right after connecting to the host. A common use case is setting
	// custom env vars for tests, like: "export TZ=America/New_York", but
	// might be useful outside of tests as well.
	// ShellInit 可以包含任意的 shell 命令，这些命令将在连接到主机后立即执行。
	// 一个常见的用例是为测试设置自定义的环境变量，例如："export TZ=America/New_York"，但在测试之外也可能有用。
	ShellInit []string `yaml:"shell_init,omitempty"`
}

// 读取 ConfigLogStreams 的所有键，并返回排序后的字符串切片
func (lss ConfigLogStreams) Keys() []string {
	// 构造特定长度的切片以存储键
	keys := make([]string, 0, len(lss))
	// 将键添加到切片中
	for k := range lss {
		keys = append(keys, k)
	}

	// 排序
	sort.Strings(keys)

	return keys
}

// EffectiveSudoMode returns the SudoMode considering all fields that can
// affect it: Sudo and SudoMode.
// EffectiveSudoMode 返回考虑所有可能影响它的字段的 SudoMode：Sudo 和 SudoMode。
func (opts ConfigLogStreamOptions) EffectiveSudoMode() SudoMode {
	if opts.SudoMode != "" {
		return opts.SudoMode
	}

	if opts.Sudo {
		return SudoModeFull
	}

	return ""
}
