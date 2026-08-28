#!/usr/bin/env bash
# hosts-hijack.sh —— 针对硬编码企微域名的 SDK 的接入适配脚本（M3）。
#
# 某些接入方 SDK 把 https://qyapi.weixin.qq.com 写死在代码里，无法只改 Base URL。
# 本脚本把该域名指向本机，配合平台自签 TLS 证书（-tls）即可完成接入：
#
#   # 1. 用自签证书启动平台（首次会自动生成 data/tls/ 下的证书与 CA）
#   ./im-server -tls -addr :443 -public-url https://127.0.0.1
#
#   # 2. 安装 hosts 劫持（默认指向 127.0.0.1）
#   sudo ./scripts/hosts-hijack.sh install 127.0.0.1
#
#   # 3. 信任平台自签 CA（否则 SDK 的 HTTPS 握手会失败）
#   sudo cp data/tls/ca.crt /usr/local/share/ca-certificates/im-local-ca.crt
#   sudo update-ca-certificates
#
#   # 4. 验证
#   curl -s 'https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=...' -k | head -c 200
#
# 回滚：
#   sudo ./scripts/hosts-hijack.sh remove
#
# 注意：仅用于本地联调；生产环境勿改动 /etc/hosts。

set -euo pipefail

HOSTS=/etc/hosts
MARK_BEGIN="# >>> im-local (qyapi) >>>"
MARK_END="# <<< im-local (qyapi) <<<"
DOMAINS="qyapi.weixin.qq.com"

# 定位本脚本所在目录，保证任何工作目录下都能跑
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

usage() {
  cat <<'EOF'
用法:
  sudo ./scripts/hosts-hijack.sh install [IP]   # 把企微域名指向 IP（默认 127.0.0.1）
  sudo ./scripts/hosts-hijack.sh remove          # 移除本脚本添加的行
  ./scripts/hosts-hijack.sh status               # 查看当前映射状态
EOF
  exit "${1:-0}"
}

install() {
  local ip="${1:-127.0.0.1}"
  if grep -q "$MARK_BEGIN" "$HOSTS"; then
    echo "已安装过（存在 im-local 标记），先移除再重装：$0 remove"
    exit 1
  fi
  # 备份一次（仅当尚无备份）
  if [ ! -f "${HOSTS}.im-local.bak" ]; then
    cp "$HOSTS" "${HOSTS}.im-local.bak"
    echo "已备份原 hosts 到 ${HOSTS}.im-local.bak"
  fi
  {
    echo "$MARK_BEGIN"
    for d in $DOMAINS; do
      echo "$ip $d"
    done
    echo "$MARK_END"
  } >>"$HOSTS"
  echo "已把以下域名指向 $ip："
  for d in $DOMAINS; do echo "  $d"; done
  echo "提示：若 SDK 走 HTTPS，请同步信任平台自签 CA（见脚本头注释）。"
}

remove() {
  if ! grep -q "$MARK_BEGIN" "$HOSTS"; then
    echo "未找到 im-local 标记，无需移除。"
    exit 0
  fi
  # 删除标记之间的行
  sed -i "/^$MARK_BEGIN\$/,/^$MARK_END\$/d" "$HOSTS"
  echo "已移除 im-local 劫持行。"
}

status() {
  echo "当前 /etc/hosts 中的 im-local 映射："
  if grep -q "$MARK_BEGIN" "$HOSTS"; then
    awk "/^$MARK_BEGIN\$/{f=1} f{print} /^$MARK_END\$/{f=0}" "$HOSTS"
  else
    echo "  （无）"
  fi
}

case "${1:-}" in
install) install "${2:-127.0.0.1}" ;;
remove) remove ;;
status) status ;;
*) usage 1 ;;
esac
