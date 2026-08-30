#!/usr/bin/env bash
#
# generate-proto.sh — 从 engine/proto/aivision/v1 权威 proto 生成 Go protobuf/gRPC 代码。
#
# 用法：
#   ./scripts/generate-proto.sh                 # 输出到 app/internal/proto/argus/v1（提交目录）
#   ./scripts/generate-proto.sh <out-root>      # 输出到指定模块根（供 proto-check 漂移检查）
#
# 约定：
#   - 唯一 proto 权威源是 engine/proto/aivision/v1/*.proto，本脚本只读取该目录。
#   - go_package 为 argus/app/internal/proto/argus/v1，配合
#     --go_opt=module=argus/app 生成文件落在 <out-root>/internal/proto/argus/v1/。
#   - 生成文件必须提交到仓库；普通 build/test 不依赖本机 protoc。
#   - 不读取、不复制 engine/tests/stub_server/gen。
#   - 可通过 PROTOC / PROTOC_GEN_GO / PROTOC_GEN_GO_GRPC 指向受控工具链中的编译器。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
PROTO_SRC="$APP_DIR/../engine/proto/argus/v1"
REL_OUT="internal/proto/argus/v1"

# 固定的生成工具最低版本（与 app/go.mod 锁定依赖保持一致）。
PROTOC_GEN_GO_MIN="v1.36.0"
PROTOC_GEN_GO_GRPC_MIN="v1.6.0"

PROTOC="${PROTOC:-protoc}"
PROTOC_GEN_GO="${PROTOC_GEN_GO:-protoc-gen-go}"
PROTOC_GEN_GO_GRPC="${PROTOC_GEN_GO_GRPC:-protoc-gen-go-grpc}"

# 模块根：默认 app 根目录（提交目录），可选参数指定临时模块根用于漂移检查。
OUT_ROOT="${1:-$APP_DIR}"

fail() {
  echo "error: $*" >&2
  exit 1
}

# --- 工具检查 ---------------------------------------------------------------
if ! command -v "$PROTOC" >/dev/null 2>&1; then
  fail "protoc not found (PATH: $PROTOC). Install protobuf compiler, e.g. 'brew install protobuf' or use PROTOC to point at a controlled toolchain."
fi
if ! command -v "$PROTOC_GEN_GO" >/dev/null 2>&1; then
  fail "protoc-gen-go not found (PATH: $PROTOC_GEN_GO). Install with: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
fi
if ! command -v "$PROTOC_GEN_GO_GRPC" >/dev/null 2>&1; then
  fail "protoc-gen-go-grpc not found (PATH: $PROTOC_GEN_GO_GRPC). Install with: go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"
fi

gen_go_ver="$("$PROTOC_GEN_GO" --version | awk '{print $2}')"
gen_grpc_ver="$("$PROTOC_GEN_GO_GRPC" --version | awk '{print $2}')"
# 比较不带构建元数据的三段语义版本；不依赖 GNU sort，兼容 macOS/BSD shell 环境。
semver_at_least() {
  local actual="${1#v}"
  local required="${2#v}"
  actual="${actual%%-*}"
  actual="${actual%%+*}"
  required="${required%%-*}"
  required="${required%%+*}"

  local -a actual_parts required_parts
  IFS='.' read -r -a actual_parts <<< "$actual"
  IFS='.' read -r -a required_parts <<< "$required"
  local part
  for part in "${actual_parts[@]}" "${required_parts[@]}"; do
    [[ "$part" =~ ^[0-9]+$ ]] || return 1
  done
  local i actual_part required_part
  for i in 0 1 2; do
    actual_part="${actual_parts[$i]:-0}"
    required_part="${required_parts[$i]:-0}"
    if ((10#$actual_part > 10#$required_part)); then
      return 0
    fi
    if ((10#$actual_part < 10#$required_part)); then
      return 1
    fi
  done
  return 0
}

if ! semver_at_least "$gen_go_ver" "$PROTOC_GEN_GO_MIN"; then
  fail "$PROTOC_GEN_GO version $gen_go_ver < required $PROTOC_GEN_GO_MIN; upgrade the plugin"
fi
if ! semver_at_least "$gen_grpc_ver" "$PROTOC_GEN_GO_GRPC_MIN"; then
  fail "$PROTOC_GEN_GO_GRPC version $gen_grpc_ver < required $PROTOC_GEN_GO_GRPC_MIN; upgrade the plugin"
fi

if [[ ! -d "$PROTO_SRC" ]]; then
  fail "proto source dir not found: $PROTO_SRC"
fi

# --- 生成 --------------------------------------------------------------------
PROTOS=("$PROTO_SRC"/*.proto)
if [[ ${#PROTOS[@]} -eq 0 ]]; then
  fail "no .proto files found in $PROTO_SRC"
fi

"$PROTOC" \
  --proto_path="$APP_DIR/../engine/proto" \
  --plugin=protoc-gen-go="$(command -v "$PROTOC_GEN_GO")" \
  --plugin=protoc-gen-go-grpc="$(command -v "$PROTOC_GEN_GO_GRPC")" \
  --go_out="$OUT_ROOT" \
  --go_opt=module=argus/app \
  --go-grpc_out="$OUT_ROOT" \
  --go-grpc_opt=module=argus/app \
  "${PROTOS[@]}"

echo "generated with $PROTOC_GEN_GO $gen_go_ver / $PROTOC_GEN_GO_GRPC $gen_grpc_ver"
echo "output: $OUT_ROOT/$REL_OUT"
