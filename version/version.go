package version

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/dimonomid/nerdlog/clipboard"
)

// These are being replaced with the actual values using ldflags;
// see ../.goreleaser.yaml and ../Makefile.
// 将被实际值替换的变量，使用 ldflags；见 ../.goreleaser.yaml 和 ../Makefile。
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"
)

// VersionFullDescr returns the full version description, printed at
// --version and :version
// VersionFullDescr 返回完整的版本描述，在 --version 和 :version 时打印
func VersionFullDescr() string {
	// 使用 strings.Builder 来高效地构建字符串
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Nerdlog %s\n", version))
	sb.WriteString(fmt.Sprintf("Commit: %s\n", commit))
	sb.WriteString(fmt.Sprintf("Build time: %s\n", date))
	sb.WriteString(fmt.Sprintf("Built by: %s\n", builtBy))
	sb.WriteString(fmt.Sprintf("GOOS: %s\n", runtime.GOOS))
	if cgoEnabled {
		sb.WriteString("CGO: enabled\n")
	} else {
		sb.WriteString("CGO: disabled\n")
	}
	if clipboard.InitErr == nil {
		sb.WriteString("Clipboard support: yes\n")
	} else {
		sb.WriteString(fmt.Sprintf("Clipboard support: no (%s)\n", clipboard.InitErr.Error()))
	}
	sb.WriteString("\n")
	sb.WriteString("Written by Dmitry Frank (https://dmitryfrank.com)\n")

	return sb.String()
}
