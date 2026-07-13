#!/usr/bin/env bash
# cpa-xai-quota-guard one-click installer (Linux/macOS)
# Usage:
#   CPA_PLUGINS_DIR=/path/to/plugins CPA_MGMT_KEY=xxx ./scripts/install.sh
# Optional:
#   VER=v0.3.7 USE_GHPROXY=1 CPA_MGMT_URL=http://127.0.0.1:8317
set -euo pipefail
VER="${VER:-v0.3.7}"
USE_GHPROXY="${USE_GHPROXY:-0}"
CPA_PLUGINS_DIR="${CPA_PLUGINS_DIR:-}"
CPA_MGMT_URL="${CPA_MGMT_URL:-http://127.0.0.1:8317}"
CPA_MGMT_KEY="${CPA_MGMT_KEY:-}"

if [[ -z "${CPA_PLUGINS_DIR}" ]]; then
  echo "请设置 CPA_PLUGINS_DIR=CPA的plugins目录" >&2
  exit 1
fi

ARCH=$(uname -m)
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS-$ARCH" in
  linux-x86_64|linux-amd64)  GOOS=linux;  GOARCH=amd64; LIB=cpa-xai-quota-guard.so;   ZIP=cpa-xai-quota-guard_linux_amd64.zip ;;
  linux-aarch64|linux-arm64) GOOS=linux;  GOARCH=arm64; LIB=cpa-xai-quota-guard.so;   ZIP=cpa-xai-quota-guard_linux_arm64.zip ;;
  darwin-x86_64)             GOOS=darwin; GOARCH=amd64; LIB=cpa-xai-quota-guard.dylib; ZIP=cpa-xai-quota-guard_darwin_amd64.zip ;;
  darwin-arm64)              GOOS=darwin; GOARCH=arm64; LIB=cpa-xai-quota-guard.dylib; ZIP=cpa-xai-quota-guard_darwin_arm64.zip ;;
  *) echo "unsupported: $OS $ARCH" >&2; exit 1 ;;
esac

BASE="https://github.com/Mortal520/cpa-xai-quota-guard/releases/download/${VER}/${ZIP}"
URL="$BASE"
[[ "$USE_GHPROXY" == "1" ]] && URL="https://ghproxy.com/${BASE}"

DEST="${CPA_PLUGINS_DIR}/${GOOS}/${GOARCH}"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "==> download $URL"
curl -fL --retry 3 -o "$TMP/$ZIP" "$URL"
echo "==> install -> $DEST/$LIB"
mkdir -p "$DEST"
unzip -qo "$TMP/$ZIP" -d "$TMP/out"
FOUND=$(find "$TMP/out" -type f \( -name '*.so' -o -name '*.dylib' -o -name '*.dll' \) | head -n1)
[[ -n "$FOUND" ]] || { echo "zip 内未找到库文件" >&2; exit 1; }
cp -f "$FOUND" "$DEST/$LIB"
ls -la "$DEST/$LIB"

if docker ps --format '{{.Names}}' 2>/dev/null | grep -qx 'cli-proxy-api'; then
  echo "==> docker restart cli-proxy-api"
  docker restart cli-proxy-api >/dev/null
  sleep 3
fi

if [[ -n "$CPA_MGMT_KEY" ]]; then
  echo "==> verify"
  curl -sS -m 12 -H "X-Management-Key: ${CPA_MGMT_KEY}" \
    "${CPA_MGMT_URL}/v0/management/cpa-xai-quota-guard/state?view=focus" || true
  echo
fi

echo "OK. 请确认 config.yaml 已配置 plugins.configs.cpa-xai-quota-guard"
