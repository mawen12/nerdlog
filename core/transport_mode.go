package core

import (
	"fmt"
	"strings"

	"github.com/juju/errors"
)

type TransportModeKind string

// 支持三种传输模式： ssh-lib、ssh-bin 和 custom。默认使用 ssh-lib。
const (
	TransportModeKindSSHLib = "ssh-lib"
	TransportModeKindSSHBin = "ssh-bin"
	TransportModeKindCustom = "custom"
)

type TransportMode struct {
	kind TransportModeKind

	// customCommand is only relevant when kind == TransportModeKindCustom;
	// it's the external shell command.
	// 仅当 kind == TransportModeKindCustom 时才相关；它是外部 shell 命令。
	customCommand string
}

func NewTransportModeSSHLib() *TransportMode {
	return &TransportMode{
		kind: TransportModeKindSSHLib,
	}
}

func NewTransportModeSSHBin() *TransportMode {
	return &TransportMode{
		kind: TransportModeKindSSHBin,
	}
}

func NewTransportModeCustom(customCommand string) *TransportMode {
	return &TransportMode{
		kind:          TransportModeKindCustom,
		customCommand: customCommand,
	}
}

// 解析传输模式，当传递 ssh-lib 时，即为 ssh-lib 模式；当传递 ssh-bin 时，即为 ssh-bin 模式；
// 当传输 custom:<cmd> 时，即为 custom 模式
func ParseTransportMode(spec string) (*TransportMode, error) {
	// 构造 custom: 前缀
	customPrefix := fmt.Sprintf("%s:", TransportModeKindCustom)

	switch {
	// ssh-lib
	case spec == TransportModeKindSSHLib:
		return &TransportMode{
			kind: TransportModeKindSSHLib,
		}, nil
	// ssh-bin
	case spec == TransportModeKindSSHBin:
		return &TransportMode{
			kind: TransportModeKindSSHBin,
		}, nil
	// custom:...
	case strings.HasPrefix(spec, customPrefix):
		cmd := strings.TrimPrefix(spec, customPrefix)

		return &TransportMode{
			kind:          TransportModeKindCustom,
			customCommand: cmd,
		}, nil

	// invalid
	default:
		return nil, errors.Errorf("invalid transport mode %q", spec)
	}
}

// 返回传输模式的类型
func (m *TransportMode) Kind() TransportModeKind {
	return m.kind
}

// 返回自定义的 shell 命令，仅当传输模式为 custom 时才会有值，否则为 ""
func (m *TransportMode) CustomShellCommand() string {
	switch m.kind {
	case TransportModeKindSSHLib:
		return ""
	case TransportModeKindSSHBin:
		return DefaultSSHShellCommand
	case TransportModeKindCustom:
		return m.customCommand
	}

	panic("should never be here")
}

// 返回传输模式的字符串表示
func (m *TransportMode) String() string {
	switch m.kind {
	case TransportModeKindSSHLib, TransportModeKindSSHBin:
		return string(m.kind)
	case TransportModeKindCustom:
		return fmt.Sprintf("%s:%s", m.kind, m.customCommand)
	}

	// Should never be here
	return "invalid"
}

// DefaultSSHShellCommand is a custom shell command which is used with ssh-bin
// transport.
//
// It's interpreted not by an external shell, but by https://github.com/mvdan/sh.
//
// Vars NLHOST, NLPORT and NLUSER are set by the nerdlog internally, but it can
// also use arbitrary environment vars.
const DefaultSSHShellCommand = "ssh -o 'BatchMode=yes' ${NLPORT:+-p ${NLPORT}} ${NLUSER:+${NLUSER}@}${NLHOST} /bin/sh"
