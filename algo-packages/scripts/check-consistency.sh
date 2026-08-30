#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

echo "[check-consistency] Verifying platform_id and vendor SDK integrity..."

# Full vendored SDK surface: include + toolkit + cmake + VERSION
sdk_full_hash() {
    local sha_cmd="sha256sum"
    if command -v shasum >/dev/null 2>&1; then
        sha_cmd="shasum -a 256"
    fi
    (cd "$1" && find . -type f \( -path './include/*' -o -path './toolkit/*' -o -path './cmake/*' -o -name 'VERSION' \) -exec ${sha_cmd} {} + | sort | ${sha_cmd} | awk '{print $1}')
}

UPSTREAM_SDK_HASH=$(sdk_full_hash "${REPO_ROOT}/sdk")

# path -> platform_id derivation (D8). <family>/<model>/<algorithm>/
# 目录仅为仓库组织方式；platform_id 唯一权威是 manifest.platform_id，
# 本表按 D8 示例编码，新增平台族时必须同步扩展。
derive_platform_id() {
    local family="$1"
    local model="$2"
    case "${family}" in
        macos)   echo "${family}-${model}-coreml" ;;
        rknn)    echo "${model}-rknn" ;;
        ascend)  echo "${model}-cann" ;;
        *)       echo "" ;;
    esac
}

# Check each algorithm package
for manifest in $(find "${REPO_ROOT}/algo-packages" -name "manifest.json" -not -path "*/build/*"); do
    pkg_dir=$(dirname "$manifest")
    echo "Checking ${pkg_dir}..."

    # AC20-a: vendor SDK integrity (full surface)
    vendor_sdk="${pkg_dir}/vendor/argus-sdk"
    if [ -d "${vendor_sdk}" ]; then
        vendor_hash=$(sdk_full_hash "${vendor_sdk}")
        if [ "${UPSTREAM_SDK_HASH}" != "${vendor_hash}" ]; then
            echo "ERROR: Vendor SDK in ${pkg_dir} out of sync with upstream sdk/ (run bash algo-packages/scripts/sync-sdk.sh)!"
            exit 1
        fi
    fi

    # AC20-b: path-derived platform_id == manifest declared value (D8)
    pkg_rel="${pkg_dir#"${REPO_ROOT}/algo-packages/"}"
    family=$(echo "${pkg_rel}" | cut -d/ -f1)
    model=$(echo "${pkg_rel}" | cut -d/ -f2)
    expected=$(derive_platform_id "${family}" "${model}")
    declared=$(python3 -c "import json,sys; print(json.load(open('${manifest}'))['platform_id'])" 2>/dev/null || true)
    if [ -z "${expected}" ]; then
        echo "ERROR: Unknown platform family '${family}' in ${pkg_dir}; update derive_platform_id in check-consistency.sh"
        exit 1
    fi
    if [ "${expected}" != "${declared}" ]; then
        echo "ERROR: platform_id mismatch in ${manifest}: path-derived '${expected}' != manifest '${declared}'"
        exit 1
    fi
done

echo "[check-consistency] All consistency checks passed."
