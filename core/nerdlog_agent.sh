# NOTE: we intentionally don't rely on shebang here, and expect this script to
# be invoked as "bash nerdlog_agent.sh" explicitly, since it's likely the most
# portable way (short of using /bin/sh, but that would be a bigger effort since
# this script does rely on bash).

# NOTE: ABANDON ALL HOPE.
#
# This script logic is really convoluted and hard to understand, and begs for a
# major rewrite.

# 当脚本退出时，自动执行一下命令，其中 $? 是上一个命令的退出状态码
# 该命令用途意为：脚本正常或异常退出时，都会输出 exit_code:后跟状态码，方便调用方获取脚本的退出状态
trap 'echo "exit_code:$?"' EXIT

# Arguments:
#
# --from, --to: time in the format "2006-01-02-15:04".

# Those numbers are supposed to go up as the query progresses; the Go app
# will then be able to tell which node is the slowest and show info for it.

STAGE_INDEX_FULL=1
STAGE_INDEX_APPEND=2
STAGE_QUERYING=3
STAGE_DONE=4

SPECIAL_FILENAME_AUTO="auto"
SPECIAL_FILENAME_JOURNALCTL="journalctl"

# The output looks like this:
# 2025-04-27T21:31:11.670468+00:00 myhot systemd[1]: Something happened.
JOURNALCTL_FORMAT_FLAG="--output=short-iso-precise"

# 索引文件为/tmp/nerdlog_agent_index_*
indexfile=/tmp/nerdlog_agent_index

# 前一个文件，auto,依赖于logfile_last，自动从 /var/log/messages.1 => /var/log/syslog.1 => journalctl => 报错
logfile_prev="${SPECIAL_FILENAME_AUTO}"
# 最新文件， auto，则自动从 /var/log/messages => /var/log/syslog => journalctl => 报错
logfile_last="${SPECIAL_FILENAME_AUTO}"

positional_args=()

max_num_lines=100

awktime_month='monthByName[substr($0, 1, 3)]'
awktime_year='yearByMonth[month]'
awktime_day='(substr($0, 5, 1) == " ") ? "0" substr($0, 6, 1) : substr($0, 5, 2)'
awktime_hhmm='substr($0, 8, 5)'
# 用于awk脚本中提取一行日志的前12个字符，并作为分钟级时间键
awktime_minute_key='substr($0, 1, 12)'
# TODO: double check that if any of these is provided manually in a flag,
# then all of them are provided manually.

# 执行 which gawk 和 which awk，并检查其是否存在
function find_gawk_binary() { # {{{
  # 将 which gawk 执行结果赋值给 gawk_path 变量
  gawk_path="$(which gawk)"
  # 如果上一个命令执行成功（即找到了 gawk 命令）
  if [[ $? == 0 ]]; then
    # 检查 gawk_path 指向的文件是否可执行
    if [ -x "$gawk_path" ]; then
      # 获取 gawk 的版本信息，并赋给 awk_version_str 变量
      awk_version_str="$($gawk_path --version)"
      # 如果获取版本信息成功
      if [[ $? == 0 ]]; then
        ## 检查版本信息中是否包含 'GNU Awk' 字符串，如果存在，则输出 gawk_path 并退出成功
        if echo "$awk_version_str" | grep -q 'GNU Awk'; then
          # gawk works fine
          # 输出会被调用方捕获
          echo "$gawk_path"
          exit 0
        fi
      fi
    fi
  fi

  # 没有找到 gawk，尝试查找 awk 命令
  awk_path="$(which awk)"
  if [[ $? == 0 ]]; then
    # 检查 awk_path 指向的文件是否可执行
    if [ -x "$awk_path" ]; then
      # 获取 awk 的版本信息，并赋给 awk_version_str 变量
      awk_version_str="$($awk_path --version)"
      if [[ $? == 0 ]]; then
        # 检查版本信息中是否包含 'GNU Awk' 字符串，如果存在，则输出 awk_path 并退出成功
        if echo "$awk_version_str" | grep -q 'GNU Awk'; then
          # 输出会被调用方捕获
          echo "$awk_path"
          exit 0
        fi
      fi
    fi
  fi

  # 如果都没有找到 gawk 或 awk，或者它们不是 GNU Awk，则退出失败
  exit 1
} # }}}

# 检测 timezone，从 TZ => timedatectl => /etc/timezone => /usr/share/zoneinfo 依次尝试
function detect_timezone() { # {{{
  # Prefer TZ env var if available
  # 如果变量已被设置，则输出并退出
  if [[ "$TZ" != "" ]]; then
    echo "$TZ"
    exit 0
  fi

  # Next check timedatectl if available
  # read from timedatectl
  host_timezone="$(timedatectl show --property=Timezone --value)"
  if [[ $? == 0 ]]; then
    echo "$host_timezone"
    exit 0
  fi

  # Resort to /etc/timezone
  # read from /etc/timezone
  if [ -r /etc/timezone ]; then
    host_timezone="$(cat /etc/timezone)"
    if [[ $? == 0 ]]; then
      echo "$host_timezone"
      exit 0
    fi
  fi

  # Then resort to browsing zoneinfo files manually
  # 反推系统时区
  zone_file="$(find /usr/share/zoneinfo  -type f -exec cmp -s /etc/localtime '{}' \; -print)"
  if [[ $? == 0 ]]; then
    host_timezone="$(echo "$zone_file" | sed -e 's|^/usr/share/zoneinfo/||' -e '/posix/d')"
    if [[ $? == 0 ]]; then
      echo "$host_timezone"
      exit 0
    fi
  fi

  exit 1
} # }}}

# function concat_cmds_array() {{{
#
# Concatenates the global `cmds` array into a single bash command, using " && ".
# Escapes things properly. The result can be passed to "eval" or "bash -c".

# 使用 && 拼接 cmds 数组中的命令，并进行适当的转义
# cmds=("tail -c +100 /var/log/syslog" "head -c 500")
function concat_cmds_array() {
  # 标识是否为第一个命令
  local first=1
  # 遍历 cmds 中的每个元素
  for cmd in "${cmds[@]}"; do
    # 第一个命令前不加 &&，后续命令前加 &&作为连接符
    if [[ $first == 1 ]]; then
      first=0
    else
      echo -n " && "
    fi
    # 单引号转义，${变量/pattern/replacement} 全部替换，此处是将单引号替换为 '\''，以便在单引号字符串中嵌入单引号
    echo -n "${cmd//\'/\'\"\'\"\'}"
  done
} # }}}

# 持续解析命令行参数，对于 -param value 这种模式的，需要使用两个 shift，对于 --flag 这种模式的，只需要使用一个 shift
while [[ $# -gt 0 ]]; do
  case $1 in
    # 索引文件路径
    -c|--index-file)
      indexfile="$2"
      shift # past argument
      shift # past value
      ;;
    # 当前日志文件
    --logfile-last)
      logfile_last="$2"
      shift # past argument
      shift # past value
      ;;
    # 上一个日志文件  
    --logfile-prev)
      logfile_prev="$2"
      shift # past argument
      shift # past value
      ;;
    # 开始查询的时间，格式为：2006-01-02-15:04  
    -f|--from)
      from="$2"
      shift # past argument
      shift # past value
      ;;
    # 结束查询的时间，格式为：2006-01-02-15:04  
    -t|--to)
      to="$2"
      shift # past argument
      shift # past value
      ;;
    -u|--lines-until)
      lines_until="$2"
      shift # past argument
      shift # past value
      ;;

    # The 3 arguments below:
    # --timestamp-until-seconds, --timestamp-until-precise, --skip-n-latest
    # are needed specifically for pagination in journalctl.
    #
    # It's all very ugly, but works correctly, and so far I'm not able to come
    # up with better alternatives (see below why --cursor etc isn't helpful for us).
    #
    # Let me explain what they mean exactly. Let's consider that we have the following
    # logs, some of which we already have loaded, and now we need to get the next page:
    #
    #    ........
    #    2025-03-10T11:49:44.123456+00:00 myhost myapp[123]: NEXT PAGE message
    #    2025-03-10T11:49:44.838785+00:00 myhost myapp[123]: NEXT PAGE message
    #    2025-03-10T11:49:44.838785+00:00 myhost myapp[123]: LOADED message
    #    2025-03-10T11:49:44.838785+00:00 myhost myapp[123]: LOADED message
    #    2025-03-10T11:49:44.988548+00:00 myhost myapp[123]: LOADED message
    #    2025-03-10T11:49:44.988548+00:00 myhost myapp[123]: LOADED message
    #    2025-03-10T11:49:44.999000+00:00 myhost myapp[123]: LOADED message
    #    2025-03-10T11:49:44.999000+00:00 myhost myapp[123]: LOADED message
    #    2025-03-10T11:49:45.002143+00:00 myhost myapp[123]: LOADED message
    #
    # So we already have two messages at the "2025-03-10T11:49:44.838785"
    # timestamp, and the next page should start from the remaining one message
    # on the same timestamp.
    #
    # So first, even though we technically can pass the --until '2025-03-10 11:49:44.838785
    # argument to journalctl, which takes it without errors, it doesn't work
    # reliably: apparently the time indexing journalctl is doing does not have
    # microsecond precision, and it will actually stop EARLIER than the given
    # timestamp, missing arbitrary number of messages.
    #
    # To make sure that we do get all the messages we need, we have to request
    # a bit more, and round the --until timestamp to the next whole second:
    # '2025-03-10 11:49:45'. This is precisely what needs to be passed as the
    # --timestamp-until-seconds flag.
    #
    # And having that, we also need to know how many latest messages to filter
    # out, because we already have them. There are a few ways of doing it, but
    # currently implemented as follows:
    #
    # 1) --timestamp-until-precise is the exact timestamp of the very latest
    #    message we have, formatted the way journalctl formats it, i.e.
    #    '2025-03-10T11:49:44.838785'
    # 2) --skip-n-latest is how many messages we already have on this timestamp,
    #    i.e. '2' in this case.
    #
    # Having that, the agent script knows everything it needs to know, and the
    # logic is as follows (btw don't forget that we call journalctl with the
    # --reverse, so we first get the latest messages, which we need to skip):
    #
    # - Check if the current timestamp from journalctl string is
    #   lexicographically larger than the given --timestamp-until-precise. If
    #   so, just skip it: we're only receiving this line because our
    #   --timestamp-until-seconds was rounded up to the whole second
    # - Check if the current timestamp from journalctl string is exactly the
    #   same as the given --timestamp-until-precise. If so, skip up to the
    #   --skip-n-latest of such messages.
    # - Otherwise, we're good to include this message in the output.
    #
    # Now, why the journalctl built-in pagination mechanism (the --cursor and
    # related flags) doesn't work. Two primary reasons:
    #
    # - Journalctl only allows us to see the cursor of the *latest* message in
    #   the output;
    # - We apply our filters, and limit number of lines, *after* journalctl,
    #   using the awk script.
    #
    # So, there's no reliable way to say "print the latest N messages maching
    # this awk pattern, and show me the cursor of the first one, so that I can
    # later get next page".
    #
    # If there was a way to show the cursor for every single line that
    # journalctl outputs, then it might be possible, but then it would likely
    # make things even slower.
    #
    # So for now, we just have to hack around with the timestamps. Ugly, but
    # works, and covered with tests.

    --timestamp-until-seconds)
      timestamp_until_seconds="$2"
      shift # past argument
      shift # past value
      ;;
    --timestamp-until-precise)
      timestamp_until_precise="$2"
      if [[ "$skip_n_latest" == "" ]]; then
        skip_n_latest=1
      fi
      shift # past argument
      shift # past value
      ;;
    --skip-n-latest)
      skip_n_latest="$2"
      shift # past argument
      shift # past value
      ;;
    # 是否刷新索引，如果需要刷新，则删除并重建索引文件
    --refresh-index)
      refresh_index="1"
      shift # past argument
      ;;
    -l|--max-num-lines)
      max_num_lines="$2"
      shift # past argument
      shift # past value
      ;;

    --awktime-month)
      awktime_month="$2"
      shift # past argument
      shift # past value
      ;;
    --awktime-year)
      awktime_year="$2"
      shift # past argument
      shift # past value
      ;;
    --awktime-day)
      awktime_day="$2"
      shift # past argument
      shift # past value
      ;;
    --awktime-hhmm)
      awktime_hhmm="$2"
      shift # past argument
      shift # past value
      ;;
    # 默认为：substr($0, 1, 12)，用户可自行覆盖
    --awktime-minute-key)
      awktime_minute_key="$2"
      shift # past argument
      shift # past value
      ;;

    -*|--*)
      echo "Unknown option $1" 1>&2
      exit 1
      ;;
    *)
      positional_args+=("$1") # save positional arg 保存非选项参数
      shift # past argument
      ;;
  esac
done

# 恢复位置参数
set -- "${positional_args[@]}" # restore positional parameters

# timestamp_until_precise, timestamp_until_seconds, skip_n_latest 必须一起使用
if [[ $timestamp_until_precise != "" || $timestamp_until_seconds != "" || $skip_n_latest != "" ]]; then
  if [[ "$timestamp_until_precise" == "" ]]; then
    echo "error:--timestamp-until-seconds, --timestamp-until-precise, --skip-n-latest should all be given together, but --timestamp-until-precise is not set" 1>&2
    exit 1
  fi

  if [[ "$timestamp_until_seconds" == "" ]]; then
    echo "error:--timestamp-until-seconds, --timestamp-until-precise, --skip-n-latest should all be given together, but --timestamp-until-seconds is not set" 1>&2
    exit 1
  fi

  if [[ "$skip_n_latest" == "" ]]; then
    echo "error:--timestamp-until-seconds, --timestamp-until-precise, --skip-n-latest should all be given together, but --skip-n-latest is not set" 1>&2
    exit 1
  fi
fi

# Either use the provided current year and month (for tests), or get the actual ones.
# 当前年份和月份设置
if [[ "$CUR_YEAR" == "" ]]; then
  CUR_YEAR="$(date +'%Y')"
fi
if [[ "$CUR_MONTH" == "" ]]; then
  CUR_MONTH="$(date +'%m')"
fi

# TODO: instead of always detecting it, add support for the --awk-binary flag,
# and only autodetect if it wasn't provided. Also, gotta always do this during
# logstream_info command.
# 检查 gawk/awk 是否存在
awk_binary="$(find_gawk_binary)"
if [[ $? != 0 ]]; then
  echo "error:gawk (GNU Awk) is a requirement, but not found on the system. Please install it, then retry" 1>&2
  exit 1
fi

# Use either a real journalctl, or a mocked one.
journalctl_binary="journalctl"
if [[ "${NERDLOG_JOURNALCTL_MOCK}" != "" ]]; then
  journalctl_binary="${NERDLOG_JOURNALCTL_MOCK}"
fi

# 检测系统类型 uname -s
os_kind=""
case "$(uname -s)" in
  Linux)
    os_kind="linux"
    ;;
  Darwin)
    os_kind="macos"
    ;;
  FreeBSD|OpenBSD|NetBSD|DragonFly)
    os_kind="bsd"
    ;;
  *)
    echo "error:unknown kernel name $(uname -s)" 2>&1
    exit 1
esac

# TODO: also check that gawk is recent enough; the -b option that we need
# was introduced in 4.0.0, released in 2011:
# https://lists.gnu.org/archive/html/info-gnu/2011-06/msg00013.html
# Since it's so old, not bothering to check the version for now.


# 对命令行 logfile_last 未给值时进行处理，分别尝试取值：/var/log/messages => /var/log/syslog => journalctl => 报错
if [[ "$logfile_last" == "${SPECIAL_FILENAME_AUTO}" ]]; then
  # 当 /var/log/messages 文件存在，则取该值
  if [ -e /var/log/messages ]; then
    logfile_last=/var/log/messages
  # 当 /var/log/syslog 文件存在，则取该值  
  elif [ -e /var/log/syslog ]; then
    logfile_last=/var/log/syslog
  # 当 journalctl 命令存在，则取 journalctl
  elif command -v journalctl > /dev/null 2>&1; then
    logfile_last="${SPECIAL_FILENAME_JOURNALCTL}"
  # 否则报错  
  else
    echo "error:failed to autodetect log file: neither /var/log/messages nor /var/log/syslog log files are present, and journalctl is not available either. Specify the log file manually" 1>&2
    exit 1
  fi
fi

# 对命令行 logfile_prev 未给值时进行处理，
if [[ "$logfile_prev" == "${SPECIAL_FILENAME_AUTO}" ]]; then
  # 对于logfile_last!=journalctl时，前一个日志文件直接在后面加 .1
  if [[ "$logfile_last" != "${SPECIAL_FILENAME_JOURNALCTL}" ]]; then
    # For now just blindly append ".1" to the first logfile; if it doesn't actually
    # exist, we'll handle this case right below.
    logfile_prev="${logfile_last}.1"
  # 对于 logfile_prev=journalctl 时，前一个日志文件也设置为 journalctl 
  else
    # Set it to the same special value
    logfile_prev="${SPECIAL_FILENAME_JOURNALCTL}"
  fi
fi

# A simple hack to account for cases when /var/log/syslog.1 doesn't exist:
# create an empty file and pretend that it's an empty log file.
# 检查当前一个日志文件不存在时，，创建一个/tmp/nerdlog-empty-file空文件代替
if [ ! -e "$logfile_prev" ] && [[ "$logfile_prev" != "${SPECIAL_FILENAME_JOURNALCTL}" ]]; then
  echo "debug:prev logfile $logfile_prev doesn't exist, using a dummy empty file /tmp/nerdlog-empty-file" 1>&2
  # TODO: instead of using the same file /tmp/nerdlog-empty-file , maybe
  # generate the name based on the index filename, to make the tests more
  # self-contained.
  logfile_prev="/tmp/nerdlog-empty-file"

  # 非常规文件或文件已经存在了，且不为空，则删除并覆盖
  if [ ! -f "$logfile_prev" ] || [ -s "$logfile_prev" ]; then
    # 删除文件，删除失败则退出
    rm -f $logfile_prev || exit 1
    # 创建文件，创建失败则退出
    touch $logfile_prev || exit 1
  fi

  # For stable output in tests, also update the creation/modification time of
  # that file to be the same as the first log file. It's not portable though
  # (not gonna work on BSD), but it's non-essential functionality, so we just
  # ignore any errors here and do nothing then.
  # 获取文件的创建时间
  ctime=$(stat -c %W $logfile_last 2>/dev/null)
  if [[ $? == 0 ]]; then
    # 将空占位文件的创建时间设置为和最新日志文件相同
    touch -d "@$ctime" $logfile_prev
  fi
fi

# 处理命令行第一个参数，因为前面已经重置过了，所以此处读取的是第一个非选项参数
command="$1"
# 如果 command 为空，则报错
if [[ "${command}" == "" ]]; then
  echo "error:command is required" 1>&2
  exit 1
fi

# 处理命令
case "${command}" in
  # query
  query)
    shift
    # Will be handled below.
    ;;

  # logstream_info，用于读取时区，日志文件的首行/最后一行内容，并出输出
  logstream_info)
    # 读取系统时区
    host_timezone="$(detect_timezone)"
    # 输出执行结果
    if [[ $? == 0 ]]; then
      echo "host_timezone:$host_timezone"
    else
      echo "warn:failed to detect host timezone"
    fi

    # 处理非 journalctl 的场景
    if [[ "${logfile_last}" != "${SPECIAL_FILENAME_JOURNALCTL}" ]]; then
      # 当要读取的日志文件不存在，报错退出
      if [ ! -e ${logfile_last} ]; then
        echo "error:${logfile_last} does not exist" 1>&2
        exit 1
      fi

      # 当要读取的日志文件不可读，报错退出
      if [ ! -r ${logfile_last} ]; then
        echo "error:${logfile_last} exists but is not readable, check your permissions" 1>&2
        exit 1
      fi

      # 当要读取的前一个日志文件不存在，报错退出
      if [ ! -e ${logfile_prev} ]; then
        echo "error:${logfile_prev} does not exist" 1>&2
        exit 1
      fi

      # 当要读取的前一个日志文件不可读，报错退出
      if [ ! -r ${logfile_prev} ]; then
        echo "error:${logfile_prev} exists but is not readable, check your permissions" 1>&2
        exit 1
      fi

      # Print a bunch of example log lines, so that the client can autodetect the
      # format.
      # 文件存在且有内容
      if [ -s ${logfile_last} ]; then
        # 读取文件最后一行内容
        last_line="$(tail -n 1 ${logfile_last})" || exit 1
        # 读取文件第一行内容
        first_line="$(head -n 1 ${logfile_last})" || exit 1
        echo "example_log_line:$last_line"
        echo "example_log_line:$first_line"
      fi
      # 前一个文件存在且有内容
      if [ -s ${logfile_prev} ]; then
        # 读取前一个文件最后一行内容
        last_line="$(tail -n 1 ${logfile_prev})" || exit 1
        # 读取前一个文件第一行内容
        first_line="$(head -n 1 ${logfile_prev})" || exit 1
        echo "example_log_line:$last_line"
        echo "example_log_line:$first_line"
      fi
    # 处理 journalctl 的场景  
    else
      # We need to use journalctl, check if it's executable
      # 检查 journalctl 是否存在
      if ! command -v "$journalctl_binary" > /dev/null 2>&1; then
        echo "error:journalctl is not found" 1>&2
        exit 1
      fi

      # Check if the user has access to all system logs (as opposed to only its
      # own logs). Ideally we'd ask journalctl, but it doesn't seem to provide
      # a way to learn this easily, so for now just checking user id and groups
      # manually.
      # id -u 返回用户Id，id -Gn 返回用户所属的组名列表，检查是否在 admin 或 systemd-journal 组中，
      # 整体逻辑为：用于既不是root，也不在adm组，也不在systemd-journal组，则打印警告
      if ! [[ "$(id -u)" == 0 || " $(id -Gn) " == *" adm "* || " $(id -Gn) " == *" systemd-journal "* ]]; then
        # User is not root, and is not in the adm or systemd-journal groups.
        # Print a warning so that the client script can show it on the UI somehow.
        echo "warn_journalctl_no_admin_access" 1>&2
      fi

      # 读取最后一行，命令为 journalctl --output=short-iso-precise --quiet -n 1
      # And print one line for the timestamp format autodetection.
      last_line="$($journalctl_binary $JOURNALCTL_FORMAT_FLAG --quiet -n 1)" || exit 1
      echo "example_log_line:$last_line"
    fi

    exit 0
    ;;

  # 不允许其他任何命令
  *)
    echo "error:invalid command ${command}" 1>&2
    exit 1
esac

# What follows is the handler for the "query" command.

# NOTE: we only show percentages with 5% increments, to save on traffic and
# other overhead. With all 24 my-nodes, having percentage being printed with
# 1% increments, it generates extra traffic of about 290KB per single query,
# wow. With 5% increments, the overhead is about 70 KB.
## =================== Query =============================

# 打印百分比，以5%为间隔，且只有当进入新的区间时才会打印，避免重复打印，进度输出到标准错误输出
awk_func_print_percentage='
function printPercentage(numCur, numTotal) {
  curPercent = int(numCur/numTotal*20);
  if (curPercent != lastPercent) {
    print "p:p:" curPercent*5 >> "/dev/stderr"
    lastPercent = curPercent
  }
}
'

# 将 awk 脚本应用到 logfile 上，而非 journalctl
function run_awk_script_logfiles {
  awk_pattern=''
  # 如果用户提供了搜索模式，则构建一个awk过滤脚本
  if [[ "$user_pattern" != "" ]]; then
    awk_pattern="!($user_pattern) {numFilteredOut++; next}"
  fi

  # NOTE: this script MUST be executed with the "-b" awk key, which means that
  # awk will work in terms of bytes, not characters. We use length($0) there and
  # we rely on it being number of bytes.
  #
  # Also btw, percentage calculation slows the whole query by about 10%, which
  # isn't ideal. TODO: maybe instead of doing the division on every line, we can
  # only do the division when the percentage changes, so we calculate the next
  # point when it'd change, and going forward we just compare it with a simple
  # "<".
  # 构建awk脚本，并以-b模式执行，
  awk_script='
  '$awk_func_print_percentage'

  # 初始化
  BEGIN {
    # bytenr 是跟踪字节数, curline 是当前行号，maxlines 是最大行数，lastPercent 刚开始为0
    bytenr=1; curline=0; maxlines='$max_num_lines'; lastPercent=0;
    # 已过滤的数量
    numFilteredOut=0;
    # 
    prevMinKey="";
  }
  # 跟踪字节数,length($0) 是当前行的字节长度，+1 是换行符
  { bytenr += length($0)+1 }
  # 每100行打印进度
  NR % 100 == 0 {
    printPercentage(bytenr, '$num_bytes_to_scan')
  }
  # 应用用户提供的过滤模式，比如 !(/ERROR/) {numFilteredOut++; next}，不匹配的行被过滤掉，匹配的行继续处理
  '$awk_pattern'

  {
    # 提取当前行的分钟级时间键
    curMinKey = '"$awktime_minute_key"';

    # NOTE: this was a naive attempt to better handle the case when timestamps
    # have decreased: instead of incrementing the bucket of the decreased
    # timestamp, we ideally want to increment the bucket of the last
    # non-decreased timestamp.
    #
    # However, to make it work properly, the minute key needs to be formatted
    # so that a later timestamp is always lexocographically larger than an
    # earlier timestamp, and while it is possible to implement it this way,
    # it slows things down significantly, which seems unjustified just to
    # handle this corner case more gracefully.
    #
    # We might still implement it at some point and make it optional, but for
    # now, keeping things simple and just not caring about this corner case.
    #
    ## Account for decreased timestamps.
    ##
    ## NOTE: to make it produce the correct result in all cases, this check
    ## needs to be before the pattern check, but we intentionally avoid doing
    ## that because it slows things down by 5-10% when the pattern filters out
    ## most of the lines, which I think is not worth it to account for this
    ## corner case.
    #if (curMinKey < prevMinKey) {
      #curMinKey = prevMinKey;
    #} else {
      #prevMinKey = curMinKey;
    #}

    stats[curMinKey]++;

    '$lines_until_check'

    # 将行数:内容放到lastlines中
    lastlines[curline] = $0;
    # 将行数:行号放到lastNRs中
    lastNRs[curline] = NR;
    # 行数+1
    curline++
    # 当行数达到最大值时，重置为0
    if (curline >= maxlines) {
      curline = 0;
    }

    next;
  }

  END {
    # 打印检索总行数，和过滤掉的行数
    print "debug:Filtered out " numFilteredOut " from " NR " lines" > "/dev/stderr"
    # 打印前一个日志文件名称
    print "logfile:'$logfile_prev':0";
    # 打印当前日志文件名称
    print "logfile:'$logfile_last':'$prevlog_lines'";

    for (x in stats) {
      print "s:" x "," stats[x]
    }

    
    for (i = 0; i < maxlines; i++) {
      ln = curline + i;
      if (ln >= maxlines) {
        ln -= maxlines;
      }

      if (!lastlines[ln]) {
        continue;
      }

      curNR = lastNRs[ln] + '$from_linenr_int' - 1;

      print "m:" curNR ":" lastlines[ln];
    }
  }
  '

  使用 /usr/bin/gawk -b <脚本> 将额外参数传递给awk脚本
  "$awk_binary" -b "$awk_script" "$@"
  if [[ "$?" != 0 ]]; then
    return 1
  fi
}

# 将 awk 脚本应用到 journalctl 上
function run_awk_script_journalctl {
  awk_pattern_check=''
  # 如果用户提供了搜索模式，则构建一个awk过滤脚本
  if [[ "$user_pattern" != "" ]]; then
    awk_pattern_check="!($user_pattern) {numFilteredOut++; next}"
  fi

  # 跳过最后n行的检查脚本
  awk_skip_n_latest_check=''
  # 只有当 timestamp_until_precise 和 skip_n_latest 都被提供时，才启用该检查脚本
  if [[ "$timestamp_until_precise" != "" && "$skip_n_latest" != "" ]]; then
    awk_skip_n_latest_check='
    # 如果还需要跳过行
    (needToSkip) {
      # 提取行记录的时间
      curtime = substr($0, 1, timestampUntilPreciseLen);

      # If the timestamp is larger than what we already have, just skip.
      # 如果当前时间大于 timestamp_until_precise，则跳过
      if (curtime > timestampUntilPrecise) {
        next;
      }

      # If the timestamp is exactly the same as what we already have,
      # skip the skip_n_latest lines.
      # 如果时间戳和 timestamp_until_precise 相同，则跳过 skip_n_latest 行
      if (curtime == timestampUntilPrecise) {
        # 计数相同时间戳的行数
        numSameTimestamp++;
        # 如果已经跳过了足够的行，则不再跳过
        if (numSameTimestamp <= '"$skip_n_latest"') {
          next;
        }

        # We have skipped enough lines, remember that
        # 我们已经跳过了足够的行，记住这一点
        print "debug:Skipped " NR-1 " latest lines" > "/dev/stderr"
        needToSkip = 0;
      }

      # If the timestamp is earlier than what we already have,
      # remember that we are done skipping, to avoid doing useless work.
      # 如果当前时间小于 timestamp_until_precise，则不再跳过
      if (curtime < timestampUntilPrecise) {
        print "debug:Skipped " NR-1 " latest lines" > "/dev/stderr"
        needToSkip = 0;
      }
    }
    '
  fi

  early_exit_check=''
  # 如果设置了最大行数，则在达到该行数后提前退出
  if [[ "$stop_after_max_num_lines" != "" ]]; then
    early_exit_check='curline >= maxlines {
      print "debug:Exiting early after collecting " curline " lines" > "/dev/stderr"
      exit
    }'
  fi

  awk_script='
  '$awk_func_print_percentage'

  # Takes timestamp in the same format as we use for --from and --to and
  # store in the index ("2006-01-02-15:04"), and returns the corresponding unix
  # timestamp.
  function indexTimestrToTimestamp(timestr) {
    # 从时间戳中提取年/月/日/时/分
    year = substr(timestr, 1, 4);
    month = substr(timestr, 6, 2);
    day = substr(timestr, 9, 2);
    hh = substr(timestr, 12, 2);
    mm = substr(timestr, 15, 2);

    return mktime(year " " month " " day " " hh " " mm " 00");
  }

  BEGIN {
    curline=0;
    lastline="";
    maxlines='$max_num_lines';
    numFilteredOut=0;
    lastPercent=-1;
    timestampUntilPrecise="'"$timestamp_until_precise"'";
    timestampUntilPreciseLen=length(timestampUntilPrecise);
    numSameTimestamp=0;
    needToSkip = timestampUntilPreciseLen > 0 ? 1 : 0;

    # Find out earliest and latest timestamp for percentage calculations.
    earliestTimestamp=0;
    latestTimestamp=0;

    # 处理指定了 from 的场景
    if ("'$from'" != "") {
      earliestTimestamp = indexTimestrToTimestamp("'$from'");
    } else {
      # No "from" timestamp; technically it is possible to get it using
      # "journalctl --no-pager | head -n 1", but not bothering for now
      # because nerdlog always provides the --from.
      #
      # If it happens, the script will just not print any percentages
      # because timespanSeconds will be 0.
    }

    # 处理指定了 to 的场景
    if ("'$to'" != "") {
      latestTimestamp = indexTimestrToTimestamp("'$to'");
    } else {
      # No "to" timestamp; just use the current time.
      # 未指定时，便使用当前时间
      latestTimestamp = systime();
    }

    timespanSeconds = 0;
    # 计算时间跨度
    if (earliestTimestamp != 0 && latestTimestamp != 0) {
      timespanSeconds = latestTimestamp - earliestTimestamp;
    }
  }

  {
    # Unfortunately journalctl prints multiline messages without the leading
    # timestamp and other details: instead, they just add padding with spaces,
    # which breaks our parsing; so we manually replace this padding with the
    # details from the previous non-padded line.
    # 当解析到的行以空格开头时，表示是多行日志的续行
    if (substr($0, 1, 1) == " ") {
      # Find out the number of leading spaces
      # 计算前导空格数量
      numLeadingSpace = length($0)
      if (NF > 0) {
        numLeadingSpace = index($0, $1) - 1;
      }

      # 如果前一行的长度小于前导空格数量，则报错退出
      if (length(lastline) < numLeadingSpace) {
        print "error:line has more leading whitespaces than the length of the previous line";
        exit 1;
      }

      # 替换前导空格
      # Replace these leading spaces with the same amount of characters from the previous line.
      $0 = substr(lastline, 1, numLeadingSpace) substr($0, numLeadingSpace + 1);
    }

    lastline = $0;
  }

  # Print percentage based on time. It is not as great as if it was
  # based on the number of bytes as we have it for the logfiles (because the
  # pace at which the percentage progresses will vary based on the intensivity
  # of the logs), but for journalctl we can hardly do any better.
  NR % 1000 == 0 {
    month = '"$awktime_month"';
    year = '"$awktime_year"';
    day = '"$awktime_day"';
    hhmm = '"$awktime_hhmm"';
    hh = substr(hhmm, 1, 2);
    mm = substr(hhmm, 4, 2);
    curTimestamp = mktime(year " " month " " day " " hh " " mm " 00");

    if (timespanSeconds > 0) {
      printPercentage(latestTimestamp-curTimestamp, timespanSeconds)
    } else {
      # We do not know the timespan, so just do not print any percentages.
    }
  }

  '$awk_pattern_check'
  '$awk_skip_n_latest_check'
  {
    stats['"$awktime_minute_key"']++;

    if (curline < maxlines) {
      lines[curline] = $0;
      curline++
    }
  }
  '$early_exit_check'

  END {
    print "debug:Filtered out " numFilteredOut " from " NR " lines" > "/dev/stderr"

    print "logfile:'$logfile_last':0";

    for (x in stats) {
      print "s:" x "," stats[x]
    }

    for (i = curline-1; i >= 0; i--) {
      print "m:0:" lines[i];
    }
  }
  '

  "$awk_binary" "$awk_script" "$@"
  if [[ "$?" != 0 ]]; then
    return 1
  fi
}

# 读取用户提供的搜索模式，比如 /INFO/
user_pattern=$1

# 处理检索 journalctl 的场景
if [[ "$logfile_last" == "${SPECIAL_FILENAME_JOURNALCTL}" ]]; then
  echo "p:stage:$STAGE_QUERYING:querying logs:Note that journalctl can be SLOW. Consider using log files." 1>&2

  # For both $from and $to, convert the format
  # "2006-01-02-15:04" -> "2006-01-02 15:04:00"
  # 基于 from + to 构建 journalctl 的时间范围，其会忽略秒
  journalctl_from=""
  if [[ "$from" != "" ]]; then
    journalctl_from="${from:0:10} ${from:11}:00"
  fi

  journalctl_to=""
  if [[ "$to" != "" ]]; then
    journalctl_to="${to:0:10} ${to:11}:00"
  fi

  stop_after_max_num_lines=""

  # Build journalctl command.
  #
  # --quiet is needed to suppress lines like "-- No entries --" or other
  # human-readable informative things; we only need logs since we parse them.
  #
  # --reverse is needed because it simplifies things and allows optimization in
  # some cases: in the awk script, we don't have to keep circular buffer for
  # all the lines and then print the last ones (like we do when reading log
  # files); and also when we're just getting the next page and not interested
  # in timeline histogram data for the full period, we just exit early after
  # accumulating $max_num_lines.
  # 构建 journalctl 命令，journalctl --output=short-iso-precise --quite --reverse
  cmd="$journalctl_binary $JOURNALCTL_FORMAT_FLAG --quiet --reverse"

  # 变为：journalctl --output=short-iso-precise --quite --reverse --since 2006-01-02 15:04:00
  if [[ -n "$journalctl_from" ]]; then
    cmd="$cmd --since \"$journalctl_from\""
  fi

  # 变为：journalctl --output=short-iso-precise --quite --reverse --util 2006-01-02 15:04:00
  if [[ -n "$timestamp_until_seconds" ]]; then
    cmd="$cmd --until \"$timestamp_until_seconds\""
    stop_after_max_num_lines="1"
    # NOTE: we'll also skip the $skip_n_latest messages with the latest timestamp.
  elif [[ -n "$journalctl_to" ]]; then
    cmd="$cmd --until \"$journalctl_to\""
  fi

  echo "debug:Command to filter logs by time range:" 1>&2
  echo "debug: $cmd" 1>&2

  # 构建 journalctl 命令，并将输出传递给下一个命令，在调用前设置环境变量
  eval "${cmd}" |                         \
    user_pattern="$user_pattern"     \
    max_num_lines="$max_num_lines"   \
    stop_after_max_num_lines="$stop_after_max_num_lines"   \
    timestamp_until_precise="$timestamp_until_precise"   \
    skip_n_latest="$skip_n_latest"   \
    run_awk_script_journalctl -

  codes=(${PIPESTATUS[@]})
  for status in "${codes[@]}"; do
    # The exit code 141 means SIGPIPE + 128, which is what journalctl returns
    # if awk didn't consume the whole output, which is totally normal when
    # we're querying the next page and exiting after getting enough lines.
    if [[ $status -ne 0 && $status -ne 141 ]]; then
      exit 1
    fi
  done

  echo "p:stage:$STAGE_DONE:done" 1>&2

  exit 0
fi

# A portable function to get file size.
# Usage: get_file_size /path/to/file
# 获取文件大小
get_file_size() {
  case $os_kind in
    linux)
      # stat -c %s /var/log/messages
      stat -c %s "$1"
      ;;
    macos|bsd)
      stat -f %z "$1"
      ;;
    *)
      echo "error:internal error: invalid os_kind '$os_kind'" 1>&2
      return 1
  esac
}

# A portable function to get file modification time.
# Usage: get_file_modtime /path/to/file
# 获取文件最后编辑时间
get_file_modtime() {
  case $os_kind in
    linux)
      # stat -c %y /var/log/messages
      stat -c %y "$1"
      ;;
    macos|bsd)
      # It's not exactly equivalent of the GNU version: it doesn't print
      # fractional seconds, but good enough for our needs.
      stat -f "%SB" -t "%Y-%m-%d %H:%M:%S" "$1"
      ;;
    *)
      echo "error:internal error: invalid os_kind '$os_kind'" 1>&2
      return 1
  esac
}

# 读取前一个日志文件大小
logfile_prev_size=$(get_file_size $logfile_prev) || exit 1
# 读取当前日志文件大小
logfile_last_size=$(get_file_size $logfile_last) || exit 1
# 计算两个日志文件的总大小
total_size=$((logfile_prev_size+logfile_last_size)) || exit 1

# 如果 --refresh-index 被提供，则删除索引文件
if [[ "$refresh_index" == "1" ]]; then
  rm -f $indexfile || exit 1
fi

# 重建索引文件
function refresh_index { # {{{

  local last_linenr=0
  local last_bytenr=0
  local prevlog_bytes=$(get_prevlog_bytenr)

  awk_vars='
    monthByName["Jan"] = "01";
    monthByName["Feb"] = "02";
    monthByName["Mar"] = "03";
    monthByName["Apr"] = "04";
    monthByName["May"] = "05";
    monthByName["Jun"] = "06";
    monthByName["Jul"] = "07";
    monthByName["Aug"] = "08";
    monthByName["Sep"] = "09";
    monthByName["Oct"] = "10";
    monthByName["Nov"] = "11";
    monthByName["Dec"] = "12";

    curYear = '${CUR_YEAR}';
    curMonth = '${CUR_MONTH}';

    yearByMonth["01"] = inferYear(1, curYear, curMonth) "";
    yearByMonth["02"] = inferYear(2, curYear, curMonth) "";
    yearByMonth["03"] = inferYear(3, curYear, curMonth) "";
    yearByMonth["04"] = inferYear(4, curYear, curMonth) "";
    yearByMonth["05"] = inferYear(5, curYear, curMonth) "";
    yearByMonth["06"] = inferYear(6, curYear, curMonth) "";
    yearByMonth["07"] = inferYear(7, curYear, curMonth) "";
    yearByMonth["08"] = inferYear(8, curYear, curMonth) "";
    yearByMonth["09"] = inferYear(9, curYear, curMonth) "";
    yearByMonth["10"] = inferYear(10, curYear, curMonth) "";
    yearByMonth["11"] = inferYear(11, curYear, curMonth) "";
    yearByMonth["12"] = inferYear(12, curYear, curMonth) "";
  '

  # Add new entries to index, if needed

  # NOTE: syslogFieldsToIndexTimestr parses the traditional systemd timestamp
  # format, like this: "Apr  5 11:07:46". But in the recent versions of
  # rsyslog, it's not the default; that traditional timestamp format can be
  # enabled by adding this line:
  #
  # $ActionFileDefaultTemplate RSYSLOG_TraditionalFileFormat
  #
  # to /etc/rsyslog.conf
  #
  # To use ISO 1806 instead (which is the default in recent rsyslog versions),
  # like "2025-04-05T11:07:46.161001+03:00":
  #
  # $ActionFileDefaultTemplate RSYSLOG_FileFormat
  #
  # But this function (and its usages) need to be updated to support it, and a
  # bunch of other time-filtering logic here. Although it's cool since it
  # includes the year, microseconds, and timezone.
  awk_functions='
function inferYear(logMonth, curYear, curMonth) {
  # 计算月份差值，并对齐进行
  delta = logMonth - curMonth

  if (delta <= -11)       # log month is Jan, current is Dec -> next year
    return curYear + 1
  else if (delta >= 8)    # log month is Sep-Dec, current is Jan -> previous year
    return curYear - 1
  else
    return curYear
}

function printIndexLine(outfile, timestr, linenr, bytenr) {
  print "idx\t" timestr "\t" linenr "\t" bytenr >> outfile;
}

'$awk_func_print_percentage'
  '
# NOTE: this script MUST be executed with the "-b" awk key, which means that
# awk will work in terms of bytes, not characters. We use length($0) there and
# we rely on it being number of bytes.

  scriptInitFromLastTimestr='
    lastHHMM = substr(lastTimestr, 8, 5);
    '

  scriptSetCurTimestr='
    bytenr_cur = bytenr_next - length($0) - 1;

    month = '"$awktime_month"';
    year = '"$awktime_year"';
    day = '"$awktime_day"';
    hhmm = '"$awktime_hhmm"';

    curTimestr = year "-" month "-" day "-" hhmm;

    # Ignore decreased timestamps: treat them as if the timestamp did not change.
    if (curTimestr < lastTimestr) {
      # TODO: make sure to print that once per occurrence, and uncomment.
      # print "warn_timestamp_decreased:from " lastTimestr " to " curTimestr > "/dev/stderr"
      next;
    }
  '
  scriptSetLastTimestrEtc='
    lastTimestr = curTimestr;
    lastHHMM = curHHMM;
  '

  script1='BEGIN { bytenr_next=1; lastPercent=0 }
{
  bytenr_next += length($0)+1
  curHHMM = '"$awktime_hhmm"';
}'

  # 当存在索引文件时的处理
  # 索引文件一行的格式：idx	2025-03-12-10:56	1053	69939
  if [ -s $indexfile ]
  then
    echo "p:stage:$STAGE_INDEX_APPEND:indexing up" 1>&2

    # 读取索引文件最后一行，并提取第二个字段，为时间
    local lastTimestr="$(tail -n 1 $indexfile | cut -f2)"
    # 读取索引文件最后一行，并提取第三个字段，为行号
    local last_linenr="$(tail -n 1 $indexfile | cut -f3)"
    # 读取索引文件最后一行，并提取第四个字段，为字节数
    local last_bytenr="$(tail -n 1 $indexfile | cut -f4)"
    local size_to_index=$((total_size-last_bytenr))

    # 基于上次读取的内容，从日志文件中继续读取，并应用awk脚本
    tail -c +$((last_bytenr-prevlog_bytes)) $logfile_last | "$awk_binary" -b "$awk_functions
  BEGIN {
    $awk_vars
    lastTimestr = \"$lastTimestr\"; $scriptInitFromLastTimestr
  }"'
  '"$script1"'
  ( lastHHMM != curHHMM ) {
    '"$scriptSetCurTimestr"';
    printIndexLine("'$indexfile'", curTimestr, NR+'$(( last_linenr-1 ))', bytenr_cur+'$(( last_bytenr-1 ))');
    printPercentage(bytenr_cur, '$size_to_index');
    '"$scriptSetLastTimestrEtc"'
  }
  ' -
    if [[ "$?" != 0 ]]; then
      echo "debug:failed to index up, removing index file" 1>&2
      rm $indexfile
      exit 1
    fi
  else
    echo "p:stage:$STAGE_INDEX_FULL:indexing from scratch" 1>&2

    echo "prevlog_modtime	$(get_file_modtime $logfile_prev)" > $indexfile

    "$awk_binary" -b "$awk_functions BEGIN { $awk_vars lastHHMM=\"\"; }"'
  '"$script1"'
  ( lastHHMM != curHHMM ) {
    '"$scriptSetCurTimestr"';
    printIndexLine("'$indexfile'", curTimestr, NR, bytenr_cur);
    printPercentage(bytenr_cur, '$total_size');
    '"$scriptSetLastTimestrEtc"'
  }
  END { print "prevlog_lines\t" NR >> "'$indexfile'" }
  ' $logfile_prev
    if [[ "$?" != 0 ]]; then
      echo "debug:failed to index from scratch $logfile_prev, removing index file" 1>&2
      rm $indexfile
      exit 1
    fi

  # Before we start handling $logfile_last, gotta read the last idx line (which is
  # last-but-one line) and set it for the next script, otherwise there is a gap
  # in index before the first line in the $logfile_last.
  # TODO: make sure that if there are no logs in the $lotfile1, we don't screw up.
    local lastTimestr=""
    local lastTimestrLine="$(tail -n 2 $indexfile | head -n 1)"
    if [[ "$lastTimestrLine" =~ ^idx$'\t' ]]; then
      lastTimestr="$(echo "$lastTimestrLine" | cut -f2)"
    fi
    "$awk_binary" -b "$awk_functions BEGIN { $awk_vars lastTimestr = \"$lastTimestr\"; $scriptInitFromLastTimestr }"'
  '"$script1"'
  ( lastHHMM != curHHMM ) {
    '"$scriptSetCurTimestr"';
    bytenr = bytenr_cur+'$prevlog_bytes';
    printIndexLine("'$indexfile'", curTimestr, NR+'$(get_prevlog_lines_from_index)', bytenr);
    printPercentage(bytenr, '$total_size');
    '"$scriptSetLastTimestrEtc"'
  }
  ' $logfile_last
    if [[ "$?" != 0 ]]; then
      echo "debug:failed to index from scratch $logfile_last, removing index file" 1>&2
      rm $indexfile
      exit 1
    fi
  fi
} # }}}

# Performs index lookup by a timestr like "2006-01-02-15:04" (typically given
# as --from or --to, and it's also stored in the index in the same form).
#
# Prints result: one of "found", "before" or "after"; and if the result
# is "found", then also prints linenumber and bytenumber, space-separated.
# "before" means the given timestr is earlier than the earliest log we have,
# and "after" obviously means that it's later than the latest log we have.
#
# One possible use is:
#   read -r my_result my_linenr my_bytenr <<<$(get_linenr_and_bytenr_from_index my_timestr)
#
# Now we can use those vars $my_result, $my_linenr and $my_bytenr
# 从索引文件中获取指定时间的行号和字节数
function get_linenr_and_bytenr_from_index() { # {{{
  "$awk_binary" -F"\t" '
    BEGIN { isFirstIdx = 1; printed = 0; }
    $1 == "idx" {
      if ("'$1'" == $2) {
        print "found " $3 " " $4;
        printed = 1;
        exit
      } else if ("'$1'" < $2) {
        if (isFirstIdx) {
          print "before";
        } else {
          print "found " $3 " " $4;
        }
        printed = 1;
        exit
      } else {
        isFirstIdx = 0;
      }
    }
    END {
      if (!printed) {
        print "after";
      }
    }
  ' $indexfile
} # }}}

# 从索引文件中读取前一个日志文件的行数
function get_prevlog_lines_from_index() { # {{{
  if ! "$awk_binary" -F"\t" 'BEGIN { found=0 } $1 == "prevlog_lines" { print $2; found = 1; exit } END { if (found == 0) { exit 1 } }' $indexfile ; then
    return 1
  fi
} # }}}

# 读取前一个日志文件的修改时间
function get_prevlog_modtime_from_index() { # {{{
  if ! "$awk_binary" -F"\t" 'BEGIN { found=0 } $1 == "prevlog_modtime" { print $2; found = 1; exit } END { if (found == 0) { exit 1 } }' $indexfile ; then
    return 1
  fi
} # }}}

# 读取前一个日志的字节数
function get_prevlog_bytenr() { # {{{
  get_file_size $logfile_prev
} # }}}

is_outside_of_range=0
# 处理指定了日期的场景
if [[ "$from" != "" || "$to" != "" ]]; then
  # If indexfile exists, check if it's valid and relevant; if not, delete it.
  # 索引文件存在时
  if [ -e "$indexfile" ]; then
    # Check timestamp in the first line of /tmp/nerdlog_agent_index, and if
    # $logfile_prev's modification time is newer, then delete whole index
    # 检查索引文件中首行的时间戳，并且如果前一个日志文件的修改时间较新，则删除整个索引文件
    logfile_prev_stored_modtime="$(get_prevlog_modtime_from_index)"
    logfile_prev_cur_modtile=$(get_file_modtime $logfile_prev)
    if [[ "$logfile_prev_stored_modtime" != "$logfile_prev_cur_modtile" ]]; then
      echo "debug:prev logfile $logfile_prev has changed: stored '$logfile_prev_stored_modtime', actual '$logfile_prev_cur_modtile', deleting index file" 1>&2
      rm -f $indexfile || exit 1
    fi

    # 如果从索引文件中读取前一个日志文件的行数失败，则认为索引文件已损坏，删除它
    if ! get_prevlog_lines_from_index > /dev/null; then
      echo "debug:broken index file (no prevlog lines), deleting it" 1>&2
      rm -f $indexfile || exit 1
    fi
  fi

  refresh_and_retry=0

  # First try to find it in index without refreshing the index

  # 索引文件可读
  if [ -s "$indexfile" ]; then
    # 开始时间不为空
    if [[ "$from" != "" ]]; then
        # 从索引文件中获取开始时间的行号和字节数
        read -r from_result from_linenr from_bytenr <<<$(get_linenr_and_bytenr_from_index "$from") || exit 1
        # 开始时间未找到，则需要刷新索引文件
        if [[ "$from_result" != "found" ]]; then
          echo "debug:the from ${from} isn't found, gonna refresh the index" 1>&2
          refresh_and_retry=1
        fi
    fi

    # 结束时间不为空
    if [[ "$to" != "" ]]; then
      # 从索引文件中获取结束时间的行号和字节数
      read -r to_result to_linenr to_bytenr <<<$(get_linenr_and_bytenr_from_index "$to") || exit 1
      # 结束时间未找到，则需要刷新索引文件
      if [[ "$to_result" != "found" ]]; then
        echo "debug:the to ${to} isn't found, gonna refresh the index" 1>&2
        refresh_and_retry=1
      fi
    fi
  else
    # 索引文件不存在，或为空，则需要刷新重建
    echo "debug:index file doesn't exist or is empty, gonna refresh it" 1>&2
    refresh_and_retry=1
  fi

  # 刷新索引文件并重试
  if [[ "$refresh_and_retry" == 1 ]]; then
    refresh_index || exit 1

    if [[ "$from" != "" ]]; then
      read -r from_result from_linenr from_bytenr <<<$(get_linenr_and_bytenr_from_index "$from") || exit 1

      if [[ "$from_result" == "before" ]]; then
        echo "debug:the from ${from} isn't found, will use the beginning" 1>&2
      elif [[ "$from_result" == "found" ]]; then
        echo "debug:the from ${from} is found: $from_linenr ($from_bytenr)" 1>&2
        if [[ "$from_bytenr" == "" || "$from_linenr" == "" ]]; then
          echo "error:from_result is found but from_bytenr and/or from_linenr is empty" 1>&2
          exit 1
        fi
      elif [[ "$from_result" == "after" ]]; then
        echo "debug:the from ${from} is after the latest log we have, will return nothing" 1>&2
        is_outside_of_range=1
      else
        echo "error:invalid from_result: $from_result" 1>&2
        exit 1
      fi
    fi

    if [[ "$to" != "" ]]; then
      read -r to_result to_linenr to_bytenr <<<$(get_linenr_and_bytenr_from_index "$to") || exit 1

      if [[ "$to_result" == "after" ]]; then
        echo "debug:the to ${to} isn't found, will use the end" 1>&2
      elif [[ "$to_result" == "found" ]]; then
        echo "debug:the to ${to} is found: $to_linenr ($to_bytenr)" 1>&2
        if [[ "$to_bytenr" == "" || "$to_linenr" == "" ]]; then
          echo "error:to_result is found but to_bytenr and/or to_linenr is empty" 1>&2
          exit 1
        fi
      elif [[ "$to_result" == "before" ]]; then
        echo "debug:the to ${to} is before the first log we have, will return nothing" 1>&2
        is_outside_of_range=1
      else
        echo "error:invalid to_result: $to_result" 1>&2
        exit 1
      fi
    fi

  fi
else
  # 重建索引文件
  if ! [ -s $indexfile ]; then
    echo "debug:neither --from or --to are given, but index doesn't exist at all, gonna rebuild" 1>&2
    refresh_index || exit 1
  fi
fi

# 如果指定的时间范围在日志范围之外，则直接退出
if [[ $is_outside_of_range == 1 ]]; then
  echo "p:stage:$STAGE_DONE:done" 1>&2
  exit 0
fi

echo "p:stage:$STAGE_QUERYING:querying logs" 1>&2

prevlog_lines=$(get_prevlog_lines_from_index)
prevlog_bytes=$(get_prevlog_bytenr)

from_linenr_int=$from_linenr
if [[ "$from_linenr" == "" ]]; then
  from_linenr_int=1
fi

lines_until_check=''
if [[ "$lines_until" != "" ]]; then
  lines_until_check="if (NR >= $((lines_until-from_linenr_int+1))) { next; }"
fi

num_bytes_to_scan=0
if [[ "$from_bytenr" == "" && "$to_bytenr" == "" ]]; then
  # Getting _all_ available logs
  num_bytes_to_scan=$total_size
elif [[ "$from_bytenr" != "" && "$to_bytenr" == "" ]]; then
  # Getting logs from some point in time to the very end (most frequent case)
  num_bytes_to_scan=$((total_size-from_bytenr))
elif [[ "$from_bytenr" == "" && "$to_bytenr" != "" ]]; then
  # Getting logs from the beginning until some point in time
  num_bytes_to_scan=$((to_bytenr))
else
  # Getting logs between two points T1 and T2
  num_bytes_to_scan=$((to_bytenr-from_bytenr))
fi


# NOTE: there are multiple ways to tail a file, and performance differs greatly:
# Log file has 21789347 lines:
#
#ubuntu@dummy-node-01:~$ time cat /var/log/syslog.1 | tail -n +16789340 > /dev/null

#real    0m4.523s
#user    0m0.869s
#sys     0m6.915s
#ubuntu@dummy-node-01:~$ time tail -n +16789340 /var/log/syslog.1 > /dev/null

#real    0m2.184s
#user    0m0.660s
#sys     0m1.524s
#ubuntu@dummy-node-01:~$ time tail -n 5000000 /var/log/syslog.1 > /dev/null

#real    0m1.260s
#user    0m0.412s
#sys     0m0.848s

# So it's best to tail file directly (without cat) and also whenever possible
# do the "-n N", not "-n +N" (but for the latest logfile, which is constantly
# appended to, we have to use the "-n +N")

# Generate commands to get all the logs as per requested timerange.
declare -a cmds
if [[ "$from_bytenr" != "" && $(( from_bytenr > prevlog_bytes )) == 1 ]]; then
  # Only $logfile_last is used.
  from_bytenr=$(( from_bytenr - prevlog_bytes ))
  if [[ "$to_bytenr" != "" ]]; then
    to_bytenr=$(( to_bytenr - prevlog_bytes ))
    echo "debug:Getting logs from offset $from_bytenr, only $((to_bytenr - from_bytenr)) bytes, all in the latest $logfile_last" 1>&2
    cmds+=("tail -c +$from_bytenr $logfile_last | head -c $((to_bytenr - from_bytenr))")
  else
    # Most common case
    echo "debug:Getting logs from offset $from_bytenr until the end of latest $logfile_last." 1>&2
    cmds+=("tail -c +$from_bytenr $logfile_last")
  fi
elif [[ "$to_bytenr" != "" && $(( to_bytenr <= prevlog_bytes )) == 1 ]]; then
  # Only $logfile_prev is used.
  if [[ "$from_bytenr" != "" ]]; then
    echo "debug:Getting logs from offset $from_bytenr, only $((to_bytenr - from_bytenr)) bytes, all in the prev $logfile_prev" 1>&2
    cmds+=("tail -c +$from_bytenr $logfile_prev | head -c $((to_bytenr - from_bytenr))")
  else
    echo "debug:Getting logs from the very beginning to offset $(( to_bytenr - 1 )), all in the prev $logfile_prev." 1>&2
    cmds+=("head -c $(( to_bytenr - 1)) $logfile_prev")
  fi
else
  # Both log files are used
  if [[ "$from_bytenr" != "" ]]; then
    info="Getting logs from offset $from_bytenr in prev $logfile_prev"
    cmds+=("tail -c +$from_bytenr $logfile_prev")
  else
    info="Getting logs from the very beginning in prev $logfile_prev"
    cmds+=("cat $logfile_prev")
  fi

  if [[ "$to_bytenr" != "" ]]; then
    info="$info to offset $(( to_bytenr - prevlog_bytes - 1 )) in latest $logfile_last"
    cmds+=("head -c $(( to_bytenr - prevlog_bytes - 1 )) $logfile_last")
  else
    info="$info until the end of latest $logfile_last"
    cmds+=("cat $logfile_last")
  fi

  echo "debug:$info" 1>&2
fi

cmds_concatenated="$(concat_cmds_array)"
echo "debug:Command to filter logs by time range:" 1>&2
echo "debug: bash -c '$cmds_concatenated'" 1>&2

# Now execute all those commands, and feed those logs to the awk script
# which will analyze them and produce the final output.
# 执行所有的命令，然后将日志传递给 awk 脚本进行分析并生成最终输出
eval $cmds_concatenated | \
  user_pattern="$user_pattern"                          \
  max_num_lines="$max_num_lines"                        \
  num_bytes_to_scan="$num_bytes_to_scan"                \
  lines_until_check="$lines_until_check"                \
  prevlog_lines="$prevlog_lines"                        \
  from_linenr_int="$from_linenr_int"                    \
  run_awk_script_logfiles -

codes=(${PIPESTATUS[@]})
for status in "${codes[@]}"; do
  if [[ $status -ne 0 ]]; then
    exit 1
  fi
done

echo "p:stage:$STAGE_DONE:done" 1>&2
