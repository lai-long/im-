#!/usr/bin/env bash
# run.sh —— 一键构建并运行本地 IM 平台（企微兼容，单二进制 im-server + SQLite 数据目录）。
#
# 覆盖：自动构建（源码比二进制新时）、默认 :7788、自签 TLS、局域网 -public-url、
# 后台运行、数据目录重置、自动打开浏览器并打印开箱即用接入信息。
#
# 用法：
#   ./run.sh                  # 构建(如需)并前台启动，自动打开浏览器
#   ./run.sh -p 8080          # 指定端口
#   ./run.sh -t               # 开启 TLS（自签证书）
#   ./run.sh -u http://192.168.1.10:7788   # 局域网协作（response_url 可达）
#   ./run.sh -b               # 后台启动，日志写入 ./im-server.log
#   ./run.sh -r               # 清空数据目录后重新初始化（会删除现有数据！）
#   ./run.sh -o               # 不自动打开浏览器
#
# 依赖：go（构建）、curl（就绪探测）。浏览器打开依赖 xdg-open / open。

set -euo pipefail

# 定位仓库根目录（无论从哪个目录调用）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

BIN=./im-server
DATA=./data
LOG=./im-server.log
PORT=7788
TLS=0
PUBLIC_URL=""
OPEN_BROWSER=1
BG=0
RESET=0

usage() {
  cat <<'EOF'
用法: ./run.sh [选项]

一键构建并启动 im-server（就绪后自动打开浏览器并打印接入信息）。

选项:
  -p, --port PORT       监听端口（默认 7788）
  -u, --public-url URL  对外可达地址，如 http://192.168.1.10:7788（局域网协作必填）
  -t, --tls             开启 TLS（首次自动生成自签证书）
  -b, --bg              后台启动（日志写入 ./im-server.log）
  -o, --no-open         不自动打开浏览器
  -r, --reset           清空数据目录后重新初始化（会删除现有数据！）
  -h, --help            显示帮助
EOF
  exit "${1:-0}"
}

while [ $# -gt 0 ]; do
  case "$1" in
    -p|--port) PORT="${2:?缺端口}"; shift 2 ;;
    -u|--public-url) PUBLIC_URL="${2:?缺地址}"; shift 2 ;;
    -t|--tls) TLS=1; shift ;;
    -b|--bg) BG=1; shift ;;
    -o|--no-open) OPEN_BROWSER=0; shift ;;
    -r|--reset) RESET=1; shift ;;
    -h|--help) usage 0 ;;
    *) echo "未知参数: $1"; usage 1 ;;
  esac
done

# 1. 构建（源码比二进制新时才重编）
build() {
  if [ -x "$BIN" ] && [ -z "$(find cmd internal -name '*.go' -newer "$BIN" -print -quit 2>/dev/null)" ]; then
    echo "▶ 二进制已是最新，跳过构建"
  else
    echo "▶ 构建 im-server ..."
    go build -o "$BIN" ./cmd/im-server
  fi
}

# 2. 数据目录重置（显式 -r 才执行）
if [ "$RESET" = 1 ]; then
  if [ -d "$DATA" ]; then
    echo "⚠ 删除数据目录 $DATA（原有消息/机器人/应用配置将丢失）"
    rm -rf "$DATA"
  fi
fi

build

# 3. 组装启动参数
ARGS=(-addr ":$PORT" -data "$DATA")
[ "$TLS" = 1 ] && ARGS+=(-tls)
[ -n "$PUBLIC_URL" ] && ARGS+=(-public-url "$PUBLIC_URL")

SCHEME=http
[ "$TLS" = 1 ] && SCHEME=https
BASE_URL="$SCHEME://127.0.0.1:$PORT"

# 4. 就绪探测 + 打开浏览器（前台/后台通用）
wait_ready() {
  for _ in $(seq 1 50); do # 最长约 10s
    if curl -kfsS -o /dev/null "$BASE_URL/" 2>/dev/null; then
      return 0
    fi
    if ! kill -0 "$1" 2>/dev/null; then
      return 1 # 进程已退出（如端口被占用）
    fi
    sleep 0.2
  done
  return 1
}

open_browser() {
  if [ "$OPEN_BROWSER" = 1 ]; then
    echo "▶ 打开浏览器: $BASE_URL/"
    if command -v xdg-open >/dev/null 2>&1; then
      xdg-open "$BASE_URL/" >/dev/null 2>&1 || true
    elif command -v open >/dev/null 2>&1; then
      open "$BASE_URL/" >/dev/null 2>&1 || true
    fi
  fi
}

# 5. 启动
if [ "$BG" = 1 ]; then
  nohup "$BIN" "${ARGS[@]}" >"$LOG" 2>&1 &
  PID=$!
  echo "▶ 后台启动 pid=$PID（日志 $LOG）"
  if ! wait_ready "$PID"; then
    echo "✗ 服务器启动失败，最近日志："
    tail -n 20 "$LOG" 2>/dev/null || true
    exit 1
  fi
  echo "▶ 已就绪。开箱即用接入信息如下："
  tail -n 20 "$LOG" 2>/dev/null | sed -n '/示例机器人/,$p' || true
  open_browser
  exit 0
fi

# 前台：子进程后台运行以支持先开浏览器，退出时清理
"$BIN" "${ARGS[@]}" &
PID=$!
trap 'kill "$PID" 2>/dev/null || true' EXIT INT TERM

if wait_ready "$PID"; then
  open_browser
  echo "▶ 就绪（Ctrl-C 停止）。访问：客户端 $BASE_URL/  控制台 $BASE_URL/admin"
else
  echo "✗ 服务器启动失败或未就绪（端口被占用？见上方输出）。"
  exit 1
fi
wait "$PID"
