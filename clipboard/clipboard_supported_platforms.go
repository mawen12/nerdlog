//go:build (darwin || linux || windows) && cgo
// +build darwin linux windows
// +build cgo

package clipboard

import (
	"os"

	"github.com/juju/errors"
	"golang.design/x/clipboard"
)

var InitErr error = nil

// init is a wrapper around clipboard.Init, it only exists because
// clipboard.Init panics if it was built with CGO_ENABLED=0, but we want just
// an error, not a panic; therefore we have this wrapper guarded with build
// flags above.
func init() {
	// 读取 NERDLOG_NO_CLIPBOARD 环境变量，该配置用于禁用剪切板
	if os.Getenv("NERDLOG_NO_CLIPBOARD") != "" {
		InitErr = errors.Errorf("clipboard is disabled via NERDLOG_NO_CLIPBOARD env var")
		return
	}

	InitErr = errors.Trace(clipboard.Init())
}

// WriteText is a wrapper around clipboard.Write with FmtText; it exists so
// that we can avoid compiling it on unsupported platforms (e.g. FreeBSD) and
// still have nerdlog working (without clipboard support).
func WriteText(value []byte) {
	clipboard.Write(clipboard.FmtText, value)
}
