#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SDK_SRC="${REPO_ROOT}/sdk"

echo "[sync-sdk] Syncing upstream sdk/ to algo-packages vendors..."

for pkg_dir in $(find "${REPO_ROOT}/algo-packages" -name "vendor" -type d); do
    target_sdk="${pkg_dir}/argus-sdk"
    echo "Updating ${target_sdk}..."
    rm -rf "${target_sdk}"
    mkdir -p "${target_sdk}"
    cp -R "${SDK_SRC}/include" "${target_sdk}/"
    cp -R "${SDK_SRC}/toolkit" "${target_sdk}/"
    cp -R "${SDK_SRC}/cmake" "${target_sdk}/"
    cp "${SDK_SRC}/VERSION" "${target_sdk}/"
done

echo "[sync-sdk] Sync complete."
