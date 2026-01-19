package core

import "io"

// ShellTransport provides an abstraction for getting shell access to a host;
// e.g. via SSH or just local shell. In the future, tsh (Teleport) might be
// supported as well, and maybe something else.
// ShellTransport 提供了一个抽象，用于获取对主机的 shell 访问；
// 例如通过 SSH 或本地 shell。将来，tsh（Teleport）也可能得到支持，甚至可能还有其他东西。
type ShellTransport interface {
	// Connect attempts to connect to the shell. It just spawns a goroutine and
	// returns immediately, and later on the result (or maybe requests for
	// additional data such as passphrases) will be delivered to the provided
	// channel.
	// Connect 尝试连接到 shell。它只是生成一个 goroutine 并立即返回，
	// 稍后连接结果（或者可能是对附加数据的请求，例如密码短语）将被传递到提供的通道。
	Connect(resCh chan<- ShellConnUpdate)
}

// ShellConn provides an abstraction of a shell connection; can be implemented
// by local shell, or SSH, or maybe something else.
// ShellConn 提供了 shell 连接的抽象；可以由本地 shell、SSH 或其他东西实现。
type ShellConn interface {
	Stdin() io.Writer
	Stdout() io.Reader
	Stderr() io.Reader

	// Close should be called when the connection is not needed anymore.
	// 当不再需要连接时，应调用 Close。
	Close()
}

// ShellConnUpdate contains the update from ssh connection. Exactly one
// field must be non-nil.
// ShellConnUpdate 包含来自 ssh 连接的更新。恰好有一个字段必须为非 nil。
type ShellConnUpdate struct {
	// Info contains some debugging info about the connection, typically sent
	// before trying to connect.
	// 包含有关连接的一些调试信息，通常在尝试连接之前发送。
	DebugInfo *ShellConnDebugInfo

	// DataRequest contains request for additional data from the user.
	// 包含来自用户的附加数据请求，可能在连接过程中需要，例如解密 ssh 私钥的密码短语。
	DataRequest *ShellConnDataRequest

	// Result contains the final connection result. After receiving an update
	// with non-nil Result, there will be no more messages to this channel.
	// 包含最终的连接结果。在收到带有非 nil Result 的更新后，
	// 此通道将不再有更多消息。
	Result *ShellConnResult
}

// ShellConnDebugInfo contains some debugging human-readable info about the
// connection.
// ShellConnDebugInfo 包含有关连接的一些调试人类可读信息。
type ShellConnDebugInfo struct {
	// Message contains some arbitrary human-readable details about the connection.
	Message string
}

// ShellConnResult contains the connection result. If the connection is
// successful, Conn is non-nil; otherwise, Err is non-nil.
// 包含连接结果。如果连接成功，Conn 为非 nil；否则，Err 为非 nil。
type ShellConnResult struct {
	Conn ShellConn
	Err  error
}

// ShellConnDataRequest contains request for additional data from the user
// which might be needed during connection, e.g. the passphrase to decrypt
// ssh private key.
// 包含来自用户的附加数据请求，可能在连接过程中需要，例如解密 ssh 私钥的密码短语。
type ShellConnDataRequest struct {
	// Title is a human-readable title to show on the data request dialog.
	// If empty, will be "Data request".
	// Title 是一个人类可读的标题，用于显示在数据请求对话框上。
	Title string

	// Message is a human-readable message to show to the user when asking for data.
	// Message 是一个人类可读的消息，用于在请求数据时向用户显示。
	Message string

	// DataKind specifies what kind of data we're requesting from the user.
	// DataKind 指定我们正在向用户请求的数据类型。
	DataKind ShellConnDataKind

	// ResponseCh is where the user response should be sent.
	// ResponseCh 是用户响应应发送到的通道。
	ResponseCh chan<- string
}

type ShellConnDataKind int

const (
	ShellConnDataKindPassword ShellConnDataKind = iota
)
