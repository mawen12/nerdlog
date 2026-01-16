# Start

## 程序入口

`cmd/nerdlog/main.go`

## 命令选项

| 命令                           | 短命令 | 默认值                               | 含义                                                                                                                              |
| ------------------------------ | ------ | ------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------- |
| `version`                      | `v`    | false                                | 输出版本信息并退出                                                                                                                |
| `time`                         | `t`    |                                      | 时间格式范围，需要与 UI 接受的格式相同，例如：`1h`, `Mar27 12:00`                                                                 |
| `lstreams-config`              |        | `~/.config/nerdlog/logstreams.yaml`  | logstreams 配置文件，设置为空字符串代表禁用读取配置                                                                               |
| `cmdhistory-file`              |        | `~/.nerdlog_history`                 | 命令行历史文件                                                                                                                    |
| `queryhistory-file`            |        | `~/.nerdlog_query_history`           | 查询历史文件                                                                                                                      |
| `lstreams`                     | `h`    | `""`                                 | 要连接的日志流，以逗号分隔的 glob 模式表示，如：`foo-*,bar-*`                                                                     |
| `pattern`                      | `p`    | `""`                                 | 用于初始化 awk 模式                                                                                                               |
| `selquery`                     | `s`    | `""`                                 | 类似于 SQL 查询来指定要展示的字段，例如：`time STICKY, message, lstream, level_name as level, *'`                                 |
| `loglevel`                     |        | `error`                              | 这不是指 nerdlog 从远程服务器获取的日志，而是其自身的日志，合法值：`error`, `warning`, `info`, `verbose1`, `verbose2`, `verbose3` |
| `ssh-config`                   |        | `~/.ssh/config`                      | 要使用的 ssh 配置文件，设置为空字符串代表禁用读取配置                                                                             |
| `ssh-key`                      |        | `~/.ssh/id_ed25519 id_ecdsa id_rsa ` | 要使用 ssh-key，只有第一个存在的文件才会被使用                                                                                    |
| `set`                          |        | `[]`                                 | 以`option=value`的表单初始化选项值，同时也可以通过 `:set` 命令设置，该值可以被设置多次                                            |
| `no-journalctl-access-warning` |        | `false`                              | 当无权读取所有系统日志的用户使用 `journalctl` 时，抑制此警告                                                                      |

### 启动示例

- 输出版本信息然后退出 `go run ./cmd/nerdlog/ -v`

- 读取本机的 `journalctl`,设置 lstreams 为 `localhost:22:journalctl`
- 读取本机的任意日志，设置 lstreams 为 `localhost:22:/home/mawen/logs/monitor.log`

- `./bin/nerdlog --lstreams 'localhost:22:journalctl'`

- `./bin/nerdlog --lstreams 'localhost:22:/home/mawen/logs/monitor.log'`

- `./bin/nerdlog --lstreams 'localhost:22:/home/mawen/logs/monitor.log,localhost:22:/home/mawen/logs/nacos/remote.log'`

## Mapping

历史记录：`:1768526287302979453:119:0:nerdlog --lstreams 'localhost:22:journalctl' --time -1h --pattern /Error/ --selquery 'time STICKY, message, lstream, *'`

- `clhistory.Item`

命令行：`nerdlog --lstreams 'localhost:22:journalctl' --time -1h --pattern /Error/ --selquery 'time STICKY, message, lstream, *'`

- `clhisotry.Item.Str`
- `main.QueryFull`
