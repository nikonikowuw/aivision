#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/.."

echo "[check-boundary] Checking headers and symbol purity..."

# Check 1: No platform private headers in sdk/ or engine/core public headers
PRIVATE_SYMBOLS="CVPixelBuffer|MLModel|rknn_|acl"
BAD_FILES=$(grep -rnE "$PRIVATE_SYMBOLS" "$REPO_ROOT/sdk/include/" "$REPO_ROOT/engine/include/argus/core/" 2>/dev/null || true)
if [ -n "$BAD_FILES" ]; then
    echo "ERROR: Platform private symbols found in public ABI / core headers:"
    echo "$BAD_FILES"
    exit 1
fi

# Check 2: Algorithm package dynamic libraries only export whitelisted C ABI symbols.
# 白名单从 sdk/include/argus/algo.h 的 AV_ALGO_*_SYMBOL 宏解析，保持 SDK 为唯一真相源。
ALGO_HEADER="$REPO_ROOT/sdk/include/argus/algo.h"
if [ ! -f "$ALGO_HEADER" ]; then
    echo "ERROR: Cannot locate SDK header $ALGO_HEADER"
    exit 1
fi
ALLOWED_SYMBOLS=$(sed -n 's/^#define AV_ALGO_[A-Z_]*_SYMBOL "\([a-z_]*\)".*/\1/p' "$ALGO_HEADER")
if [ -z "$ALLOWED_SYMBOLS" ]; then
    echo "ERROR: No AV_ALGO_*_SYMBOL macros parsed from $ALGO_HEADER"
    exit 1
fi

ALGO_LIBS=$(find "$REPO_ROOT/algo-packages" -path "*/build/package_out/lib/*" \
    \( -name "*.dylib" -o -name "*.so" \) 2>/dev/null || true)
if [ -z "$ALGO_LIBS" ]; then
    echo "[check-boundary] No built algorithm package found; skipping export symbol audit."
else
    while IFS= read -r lib; do
        if [ "$(uname)" = "Darwin" ]; then
            EXPORTED=$(nm -gU "$lib" | awk '{print $3}' | sed 's/^_//')
        else
            EXPORTED=$(nm -g --defined-only "$lib" | awk '{print $3}')
        fi
        UNEXPECTED=$(comm -23 <(echo "$EXPORTED" | grep -v '^$' | sort -u) \
                              <(echo "$ALLOWED_SYMBOLS" | sort -u))
        if [ -n "$UNEXPECTED" ]; then
            echo "ERROR: $lib exports symbols outside the SDK whitelist:"
            echo "$UNEXPECTED" | sed 's/^/  /'
            exit 1
        fi
        if ! echo "$EXPORTED" | grep -qx "av_algo_get_abi"; then
            echo "ERROR: $lib does not export av_algo_get_abi"
            exit 1
        fi
    done <<< "$ALGO_LIBS"
fi

echo "[check-boundary] All boundary checks passed successfully."
