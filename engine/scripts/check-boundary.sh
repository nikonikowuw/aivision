#!/usr/bin/env bash
set -euo pipefail

echo "[check-boundary] Checking headers and symbol purity..."

# Check 1: No platform private headers in sdk/ or engine/core public headers
PRIVATE_SYMBOLS="CVPixelBuffer|MLModel|rknn_|acl"
BAD_FILES=$(grep -rnE "$PRIVATE_SYMBOLS" sdk/include/ engine/include/aivision/core/ 2>/dev/null || true)
if [ -n "$BAD_FILES" ]; then
    echo "ERROR: Platform private symbols found in public ABI / core headers:"
    echo "$BAD_FILES"
    exit 1
fi

echo "[check-boundary] All boundary checks passed successfully."
