package main

import (
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dimonomid/nerdlog/clipboard"
	"github.com/dimonomid/nerdlog/version"
	"github.com/gdamore/tcell/v2"
	"github.com/juju/errors"
)

// NOTE: handleCmd is always called from the tview's event loop, so it's safe
// to use all UI primitives and nerdlogApp etc.
// 处理命令行输入，其总是由 tview 的事件循环调用，因此可以安全地使用所有 UI 原语和 nerdlogApp 等。
func (app *nerdlogApp) handleCmd(cmd string) {

	// 拆分命令为多个部分
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}

	switch parts[0] {
	// Help command
	case "h", "help":
		var sb strings.Builder
		sb.WriteString("There is no built-in help yet, but check out these resources:\n")
		sb.WriteString("\n")
		sb.WriteString("README.md in the repo:\n    https://github.com/dimonomid/nerdlog\n")
		sb.WriteString("Documentation:\n    https://github.com/dimonomid/nerdlog/blob/master/docs/index.md")

		app.mainView.showMessagebox("err", "Help", sb.String(), &MessageboxParams{
			BackgroundColor: tcell.ColorDarkBlue,
			CopyButton:      true,
		})
	// 设置时间范围
	case "time":
		// 解析时间范围
		ftr, err := ParseFromToRange(app.options.GetTimezone(), strings.Join(parts[1:], " "))
		if err != nil {
			app.printError(err.Error())
			return
		}
		// 应用时间范围并执行查询
		app.mainView.setTimeRange(ftr.From, ftr.To)
		app.mainView.doQuery(doQueryParams{})
	// 写入日志到文件
	case "w", "write":
		//if len(parts) < 2 {
		//app.printError(":write requires an argument: the filename to write")
		//return
		//}

		//fname := parts[1]

		// 默认写入到 /tmp/last_nerdlog
		fname := "/tmp/last_nerdlog"
		if len(parts) >= 2 {
			// 指定了文件名
			fname = parts[1]
		}

		// 没有日志可写
		if app.lastLogResp == nil {
			app.printError("No logs yet")
			return
		}

		// 创建文件
		lfile, err := os.Create(fname)
		if err != nil {
			app.printError(fmt.Sprintf("Failed to open %s for writing: %s", fname, err))
			return
		}

		// 写入日志
		for _, logMsg := range app.lastLogResp.Logs {
			fmt.Fprintf(lfile, "%s <ssh -t %s vim +%d %s>\n",
				logMsg.OrigLine,
				logMsg.Context["lstream"], logMsg.LogLinenumber, logMsg.LogFilename,
			)
		}

		// 关闭文件
		lfile.Close()

		app.printMsg(fmt.Sprintf("Saved to %s", fname))
	// 设置选项
	case "set":
		// 参数至少2个
		if len(parts) < 2 || len(parts[1]) == 0 {
			app.printError("set requires an argument")
			return
		}

		remaining := strings.TrimSpace(cmd[len(parts[0]):])
		setRes, err := app.setOption(remaining)
		if err != nil {
			app.printError(capitalizeFirstRune(err.Error()))
		}

		if setRes != nil {
			if setRes.got != nil {
				optName := setRes.got.optName
				optValue := setRes.got.optValue
				app.printMsg(fmt.Sprintf("%s is %s", optName, optValue))
			}
		}
		// 复制命令到剪贴板
	case "xc", "xclip":
		qf := app.mainView.getQueryFull()
		shellCmd := qf.MarshalShellCmd()
		if app.params.clipboardInitErr == nil {
			clipboard.WriteText([]byte(shellCmd))
			app.printMsg("Copied to clipboard")
		} else {
			app.printError(fmt.Sprintf("Clipboard is not available: %s", app.params.clipboardInitErr.Error()))
		}
	// 处理 nerdlog 命令
	case "nerdlog":
		// Mimic as if it was called from a shell

		if err := app.unmarshalAndApplyQuery(cmd, doQueryParams{}); err != nil {
			app.printError(err.Error())
			return
		}
	// 历史记录导航，向前，同时更新查询
	case "prev", "bac", "bck", "back":
		item := app.queryBLHistory.Prev()
		if item == nil {
			app.printError("No more history items")
			return
		}

		if err := app.unmarshalAndApplyQuery(item.Str, doQueryParams{
			dontAddHistoryItem: true,
		}); err != nil {
			// This shouldn't happen really provided a sane history.
			app.printError(err.Error())
			return
		}

		// TODO: print history item stats
	// 历史记录导航，向后，同时更新查询
	case "next", "fwd", "forward":
		item := app.queryBLHistory.Next()
		if item == nil {
			app.printError("No more history items")
			return
		}

		if err := app.unmarshalAndApplyQuery(item.Str, doQueryParams{
			dontAddHistoryItem: true,
		}); err != nil {
			// This shouldn't happen really provided a sane history.
			app.printError(err.Error())
			return
		}

		// TODO: print history item stats
	// 编辑查询
	case "e", "edit":
		app.mainView.openQueryEditView()
	// 退出应用
	case "q", "quit":
		app.tviewApp.Stop()
	// 重新连接
	case "reconnect":
		app.mainView.reconnect(true)
	// 断开连接
	case "disconnect":
		app.mainView.disconnect()
	// 刷新查询
	case "refresh":
		app.mainView.doQuery(doQueryParams{})
	// 强制刷新查询
	case "refresh!":
		app.mainView.doQuery(doQueryParams{
			refreshIndex: true,
		})
	// 显示连接调试信息
	case "conndebug", "cdebug":
		app.mainView.showConnDebugInfo()
	// 显示最后一次查询的调试信息
	case "querydebug", "qdebug", "debug":
		// 显示最后一次查询的调试信息
		app.mainView.showLastQueryDebugInfo()
	// 显示版本信息
	case "version", "about":
		// 展示版本信息对话框
		app.mainView.showMessagebox("version", "Version", version.VersionFullDescr(), &MessageboxParams{
			BackgroundColor: tcell.ColorDarkBlue,
			CopyButton:      true,
		})
	// 未知命令，报错
	default:
		app.printError(fmt.Sprintf("unknown command %q", parts[0]))
	}
}

// 解析并应用查询命令，用于在历史记录导航时更新查询
func (app *nerdlogApp) unmarshalAndApplyQuery(cmd string, dqp doQueryParams) error {
	var qf QueryFull
	// 更新查询命令
	if err := qf.UnmarshalShellCmd(cmd); err != nil {
		return errors.Annotatef(err, "parsing")
	}

	// 立即查询，将查询四要素进行解析，并开始查询
	if err := app.mainView.applyQueryEditData(qf, dqp); err != nil {
		return errors.Annotatef(err, "applying")
	}

	return nil
}

// 将首字母替换为大写
func capitalizeFirstRune(s string) string {
	// 获取第一个字符并转换为大写
	r, size := utf8.DecodeRuneInString(s)
	// 无法解码，直接返回原字符串
	if r == utf8.RuneError {
		return s
	}
	// 返回首字母大写的字符串
	return string(unicode.ToUpper(r)) + s[size:]
}
