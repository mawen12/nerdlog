# Nerdlog Agent Bash

## 命令行参数

| 参数                        | 含义                                                   | 短标识 | 变量                      |
| --------------------------- | ------------------------------------------------------ | ------ | ------------------------- |
| `--index-file`              | 索引文件全路径                                         | `-c`   | `indexfile`               |
| `--logfile-last`            | 当前的日志路径，比如 /var/log/messages 或是 journalctl |        | `logfile_last`            |
| `--logfile-prev`            | 前一个的日志路径，比如 /var/log/messages.1             |        | `logfile_prev`            |
| `--from`                    | 日志的开始时间，格式为：2006-01-02-15:04               | `-f`   | `from`                    |
| `--to`                      | 日志的结束时间，格式为：2006-01-02-15:04               | `-t`   | `to`                      |
| `--lines-until`             |                                                        | `-u`   | `to`                      |
| `--timestamp-until-seconds` |                                                        |        | `timestamp_until_seconds` |
| `--timestamp-until-precise` |                                                        |        | `timestamp_until_precise` |
| `--skip-n-latest`           |                                                        |        | `skip_n_latest`           |
| `--refresh-index`           | 是否刷新并重建索引文件，与 --index-file 搭配使用       |        | `refresh_index`           |
| `--max-num-lines`           |                                                        | `-l`   | `max_num_lines`           |
| `--awktime-month`           |                                                        |        | `awktime_month`           |
| `--awktime-year`            |                                                        |        | `awktime_year`            |
| `--awktime-day`             |                                                        |        | `awktime_day`             |
| `--awktime-hhmm`            |                                                        |        | `awktime_hhmm`            |
| `--awktime-minute-key`      |                                                        |        | `awktime_minute_key`      |

### 约束

`--timestamp-until-seconds, --timestamp-until-precise, --skip-n-latest` 必须同时提供。

## 命令行命令

| 命令             | 含义 |
| ---------------- | ---- |
| `query`          |      |
| `logstream_info` |      |

## 环境变量

| 变量                      | 含义     | 是否允许为空                     |
| ------------------------- | -------- | -------------------------------- |
| `SPECIAL_FILENAME_AUTO`   |          |                                  |
| `TZ`                      |          |                                  |
| `CUR_YEAR`                | 当前年份 | 允许为空，将从 `date +'%Y'` 取值 |
| `CUR_MONTH`               | 当前月份 | 允许为空，将从 `date +'%m'` 取值 |
| `NERDLOG_JOURNALCTL_MOCK` |          | 允许为空                         |

## 示例

```bash
sh nerdlog_agent.sh query '/INFO/' --from 2025-01-29-10:00 --to 2025-01-29-11:00 --max-num-lines 100
```

- `nerdlog_agent.sh` 要执行的脚本
- `query` 自定义的命令行参数，第一个必须是 query | logstream_info
- `'/INFO/'` 查询指定的 pattern
- `--from 2025-01-29-10:00` 被保存到 from 变量
- `--to 2025-01-29-11:00` 被保存到 to 变量
- `--max-num-lines 100` 被保存到

## 函数解析

| 变量                               | 含义                                                                                                       |
| ---------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `find_gawk_binary`                 | 使用 `which gawk` 和 `which awk` 查找 awk 执行路径                                                         |
| `detect_timezone`                  | 通过 TZ 和 `timedatectl show --property=Timezone --value`、`/etc/timezone`、`/usr/share/zoneinfo` 查找时区 |
| `concat_cmds_array`                | 将一组命令转换为可执行的单个命令，命令间使用 `&&` 拼接，在处理时将单引号替替换为 `'\''`                    |
| `printPercentage`                  | 打印执行百分比                                                                                             |
| `run_awk_script_logfiles`          | 构造 awk 脚本，并应用到 logfile 上                                                                         |
| `run_awk_script_journalctl`        | 构造 awk 脚本，并应用到 journalctl 上                                                                      |
| `get_file_modtime`                 | 获取文件的编辑时间，使用 `stat -c %y`                                                                      |
| `refresh_index`                    | 重建索引文件                                                                                               |
| `inferYear`                        | 根据月份和年份推断月份                                                                                     |
| `printIndexLine`                   | 将内容写入到 indexFile 中                                                                                  |
| `get_linenr_and_bytenr_from_index` | 从索引文件中读取行号和字节数                                                                               |
| `get_prevlog_lines_from_index`     | 从索引文件中读取前一个日志的行数                                                                           |
| `get_prevlog_modtime_from_index`   | 从索引文件中读取前一个日志的编辑时间                                                                       |
| `get_prevlog_bytenr`               | 读取文件字节数                                                                                             |

## 上游

shell_transport 的两个实现：

- `shell_transport_ssh_lib`，底层使用 `ssh.Client` & `ssh.Session`
- `shell_transport_custom_cmd`，底层使用 `exec.Cmd`，支持执行自定义的命令，比如连接本地时，就是 `/bin/sh`，
