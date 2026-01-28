# Nerdlog Agent Bash

## 命令行参数

| 参数                        | 含义           | 短标识 | 变量                      |
| --------------------------- | -------------- | ------ | ------------------------- |
| `--index-file`              | 索引文件全路径 | `-c`   | `indexfile`               |
| `--logfile-last`            |                |        | `logfile_last`            |
| `--logfile-prev`            |                |        | `logfile_prev`            |
| `--from`                    |                | `-f`   | `from`                    |
| `--to`                      |                | `-t`   | `to`                      |
| `--lines-until`             |                | `-u`   | `to`                      |
| `--timestamp-until-seconds` |                |        | `timestamp_until_seconds` |
| `--timestamp-until-precise` |                |        | `timestamp_until_precise` |
| `--skip-n-latest`           |                |        | `skip_n_latest`           |
| `--refresh-index`           |                |        | `refresh_index`           |
| `--max-num-lines`           |                | `-l`   | `max_num_lines`           |
| `--awktime-month`           |                |        | `awktime_month`           |
| `--awktime-year`            |                |        | `awktime_year`            |
| `--awktime-day`             |                |        | `awktime_day`             |
| `--awktime-hhmm`            |                |        | `awktime_hhmm`            |
| `--awktime-minute-key`      |                |        | `awktime_minute_key`      |

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
