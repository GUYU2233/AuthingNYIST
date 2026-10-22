#!/usr/bin/env bash
# oneclick-auth.sh — 校园网一键认证 (Linux/macOS)
# 用法: ./oneclick-auth.sh [config.json] [--keep-online] [--deauth]
# 在脚本目录创建私有临时 JSON 配置，退出时仅清理该文件；不修改 ~/.auth-nyist。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
CONFIG="$SCRIPT_DIR/config.json"
CONFIG_SET=0
KEEP_ONLINE=0
DEAUTH=0
for arg in "$@"; do
  case "$arg" in
    --keep-online) KEEP_ONLINE=1 ;;
    --deauth) DEAUTH=1 ;;
    -*) echo "未知参数: $arg" >&2; exit 1 ;;
    *)
      if [[ "$CONFIG_SET" == 1 ]]; then
        echo "只能指定一个配置文件: $arg" >&2; exit 1
      fi
      CONFIG="$arg"
      CONFIG_SET=1
      ;;
  esac
done

if [[ ! -f "$CONFIG" ]]; then
  echo "找不到配置文件: $CONFIG" >&2
  echo "请执行: cp config.json.example config.json 并填入账号密码" >&2
  exit 1
fi

EXE="$REPO_ROOT/auth-nyist"
if [[ ! -x "$EXE" ]]; then
  echo "未找到可执行的 $EXE, 请先执行:" >&2
  echo "  cd $REPO_ROOT && go build -o auth-nyist ./cli" >&2
  exit 1
fi

if command -v python3 >/dev/null 2>&1; then
  JSON_TOOL=python3
elif command -v jq >/dev/null 2>&1; then
  JSON_TOOL=jq
else
  echo "需要 python3 或 jq 解析 config.json" >&2; exit 1
fi

umask 077
AUTH_CONFIG="$(mktemp "$SCRIPT_DIR/.auth-nyist.XXXXXX")"
cleanup() {
  rm -f -- "$AUTH_CONFIG"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
chmod 600 "$AUTH_CONFIG"

# 由 JSON 编码器处理引号、反斜杠、换行和 Unicode，不拼接密码。
if [[ "$JSON_TOOL" == python3 ]]; then
  python3 - "$CONFIG" "$AUTH_CONFIG" "$KEEP_ONLINE" <<'PY'
import json
import sys

try:
    with open(sys.argv[1], encoding="utf-8") as source:
        data = json.load(source)
    if not isinstance(data, dict):
        raise ValueError("config.json must be an object")
    result = {
        "username": data.get("username"),
        "password": data.get("password"),
        "host": data.get("host") or "auth.nyist.edu.cn",
        "acId": data.get("acId") or "1",
        "insecure": data.get("insecure", False),
        "keepOnline": data.get("keepOnline", False),
    }
    for key in ("username", "password", "host", "acId"):
        if not isinstance(result[key], str) or not result[key]:
            raise ValueError(key + " must be a non-empty string")
    for key in ("insecure", "keepOnline"):
        if not isinstance(result[key], bool):
            raise ValueError(key + " must be a boolean")
    result["keepOnline"] = result["keepOnline"] or sys.argv[3] == "1"
    with open(sys.argv[2], "w", encoding="utf-8") as target:
        json.dump(result, target, ensure_ascii=False)
except (OSError, ValueError):
    sys.exit("配置无效：请检查 JSON、账号密码及字段类型")
PY
  jget() {
    python3 - "$AUTH_CONFIG" "$1" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as source:
    value = json.load(source)[sys.argv[2]]
print(str(value).lower() if isinstance(value, bool) else value)
PY
  }
else
  jq -e --argjson keep "$KEEP_ONLINE" '
    if type != "object" then error("config must be an object") else . end
    | {username, password,
       host: (if .host == null or .host == "" then "auth.nyist.edu.cn" else .host end),
       acId: (if .acId == null or .acId == "" then "1" else .acId end),
       insecure: (if has("insecure") then .insecure else false end),
       keepOnline: (if has("keepOnline") then .keepOnline else false end)}
    | if ([.username, .password, .host, .acId]
          | all(.[]; type == "string" and length > 0))
         and (.insecure | type == "boolean")
         and (.keepOnline | type == "boolean")
      then .keepOnline = (.keepOnline or ($keep == 1))
      else error("invalid config field types or missing credentials") end
  ' "$CONFIG" > "$AUTH_CONFIG"
  jget() { jq -r --arg key "$1" '.[$key]' "$AUTH_CONFIG"; }
fi

USERNAME="$(jget username)"
HOST="$(jget host)"
ACID="$(jget acId)"
if [[ "$(jget keepOnline)" == true ]]; then KEEP_ONLINE=1; fi

# --config-file 和 --daemonize 是全局参数，必须位于子命令之前。
CMD=("$EXE" --config-file "$AUTH_CONFIG" --daemonize auth)
if [[ "$KEEP_ONLINE" == 1 ]]; then CMD+=(--keep-online); fi
if [[ "$DEAUTH" == 1 ]]; then CMD=("$EXE" --config-file "$AUTH_CONFIG" --daemonize deauth); fi

echo "=== 校园网一键认证 ==="
echo "账号: $USERNAME"
echo "主机: $HOST   acId: $ACID"
echo "模式: $( [ "$DEAUTH" = 1 ] && echo 登出 || ( [ "$KEEP_ONLINE" = 1 ] && echo 登录+保活 || echo 单次登录 ) )"
echo
"${CMD[@]}"
