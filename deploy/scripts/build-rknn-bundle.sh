#!/usr/bin/env bash
set -euo pipefail

# 获取工程根目录
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DIST_DIR="${PROJECT_ROOT}/dist"
STAGE_DIR="${DIST_DIR}/argus-engine"
IMAGE_NAME="argus-cross-builder:rknn"

echo "==> 1. 检查或构建 Docker 交叉编译镜像..."
if ! docker image inspect "${IMAGE_NAME}" >/dev/null 2>&1; then
    echo "未找到镜像 ${IMAGE_NAME}，正在构建..."
    docker build -t "${IMAGE_NAME}" -f "${PROJECT_ROOT}/deploy/docker/Dockerfile.cross-rknn" "${PROJECT_ROOT}"
fi

echo "==> 2. 清理旧构建并创建输出目录..."
rm -rf "${STAGE_DIR}"
mkdir -p "${STAGE_DIR}/bin" "${STAGE_DIR}/lib" "${STAGE_DIR}/etc" "${DIST_DIR}"

echo "==> 3. 在 Docker 容器内执行 aarch64 交叉编译..."
docker run --rm \
    -v "${PROJECT_ROOT}:/workspace" \
    -w /workspace/engine \
    "${IMAGE_NAME}" \
    bash -c '
        set -euo pipefail
        mkdir -p build_cross_rknn
        cmake -B build_cross_rknn -G Ninja \
            -DCMAKE_TOOLCHAIN_FILE=cmake/toolchain-aarch64-linux.cmake \
            -DPLATFORM_TARGET=mock \
            -DENGINE_BUILD_TESTS=OFF \
            -DCMAKE_BUILD_TYPE=Release
        cmake --build build_cross_rknn --target argus-engine -j$(nproc)
    '

echo "==> 4. 提取编译产物与 RKNN 依赖库..."
# 提取可执行文件
cp "${PROJECT_ROOT}/engine/build_cross_rknn/argus-engine" "${STAGE_DIR}/bin/"

# 从 Docker 容器中提取官方 librknnrt.so 动态库
docker run --rm "${IMAGE_NAME}" cat /usr/aarch64-linux-gnu/lib/librknnrt.so > "${STAGE_DIR}/lib/librknnrt.so"

# 生成开发板一键启动脚本
cat << 'EOF' > "${STAGE_DIR}/start.sh"
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export LD_LIBRARY_PATH="${SCRIPT_DIR}/lib:${LD_LIBRARY_PATH:-}"

echo "[Argus] 正在启动 Argus 边缘媒体推理引擎..."
exec "${SCRIPT_DIR}/bin/argus-engine"
EOF
chmod +x "${STAGE_DIR}/start.sh"

echo "==> 5. 打包为便携压缩包..."
ARCHIVE_NAME="argus-engine-rknn-linux-arm64.tar.gz"
tar -czf "${DIST_DIR}/${ARCHIVE_NAME}" -C "${DIST_DIR}" argus-engine
rm -rf "${STAGE_DIR}"

echo "==> 构建完成！便携包路径: ${DIST_DIR}/${ARCHIVE_NAME}"
echo "您可以直接将此压缩包拷贝到任意 RK3576 / RK3588 开发板上解压运行！"
