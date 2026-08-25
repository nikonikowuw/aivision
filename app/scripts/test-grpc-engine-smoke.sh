#!/usr/bin/env bash
#
# test-grpc-engine-smoke.sh — Go 产品后端与真实 C++ aivision-engine（mock platform）
# 的跨语言 gRPC 冒烟 E2E。
#
# 流程：
#   1. 使用专用 mock 构建目录配置并构建 aivision-engine（PLATFORM_TARGET=mock）；
#   2. 以 AIVISION_ENGINE_BIN 传入绝对二进制路径，运行 Go integration 测试
#      （go test -tags integration ./tests/integration/ -run TestGrpcEngineE2E）。
#
# 普通 `go test ./...` 不依赖 C++ 构建；本脚本是唯一入口。
# 可通过 AIVISION_ENGINE_BIN 指向预构建二进制跳过构建。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ENGINE_DIR="$(cd "$APP_DIR/../engine" && pwd)"
MOCK_BUILD_DIR="${MOCK_BUILD_DIR:-$ENGINE_DIR/build_mock}"

# --- 1. 构建 mock engine（可跳过） -------------------------------------------
if [[ -n "${AIVISION_ENGINE_BIN:-}" ]]; then
    echo "==> using prebuilt engine: ${AIVISION_ENGINE_BIN}"
else
    echo "==> configuring mock engine build at ${MOCK_BUILD_DIR}"
    cmake -B "$MOCK_BUILD_DIR" -G Ninja \
        -DPLATFORM_TARGET=mock \
        -DCMAKE_BUILD_TYPE=Debug \
        -DENGINE_BUILD_TESTS=OFF \
        "$ENGINE_DIR"
    echo "==> building aivision-engine (mock)"
    cmake --build "$MOCK_BUILD_DIR" --target aivision-engine
    export AIVISION_ENGINE_BIN="${MOCK_BUILD_DIR}/aivision-engine"
fi

if [[ ! -x "${AIVISION_ENGINE_BIN}" ]]; then
    echo "error: engine binary not executable: ${AIVISION_ENGINE_BIN}" >&2
    exit 1
fi
echo "==> engine binary: ${AIVISION_ENGINE_BIN}"

# --- 2. 运行跨语言 Go E2E 测试 ------------------------------------------------
echo "==> running Go grpc cross-language E2E"
cd "$APP_DIR"
go test -tags integration ./tests/integration/ -run 'TestGrpcEngineE2E' -v -count=1
