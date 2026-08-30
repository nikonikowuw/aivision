# Docker 交叉编译与打包配置

本文档介绍如何在 x86_64 / Apple Silicon 开发机上，通过 Docker 交叉编译生成适用于 **Rockchip (RK3576 / RK3588 等 aarch64)** 边缘设备的便携运行包。

---

## 1. 设计原理

- **开发机（Docker 容器）**：基于标准的 Ubuntu 20.04 / 22.04 aarch64 交叉编译链，内置官方 Rockchip `rknpu2` 库与头文件；
- **半静态链接 + RPATH**：
  - 核心业务代码、gRPC/Protobuf、ZLMediaKit、C++ 标准库（`-static-libgcc -static-libstdc++`）全部打进主二进制；
  - 硬件专有驱动库（如 `librknnrt.so`）放在 `./lib/` 目录下，通过 `-Wl,-rpath,'$ORIGIN/../lib'` 相对路径加载；
- **目标设备（RKNN 板卡）**：**无需安装任何编译工具链或依赖库**，直接拷贝便携包即可运行。

---

## 2. 目录结构与快速使用

### 2.1 构建编译镜像
在项目根目录下执行：

```bash
docker build -t argus-cross-builder:rknn -f deploy/docker/Dockerfile.cross-rknn .
```

### 2.2 一键交叉编译并生成便携包

```bash
bash deploy/scripts/build-rknn-bundle.sh
```

构建完成后，产物位于：
```text
dist/argus-engine-rk3576-linux-arm64.tar.gz
```

### 2.3 部署到开发板

将压缩包复制到板卡并解压：

```bash
# 复制到板卡
scp dist/argus-engine-rk3576-linux-arm64.tar.gz root@<板卡IP>:/opt/

# 在板卡上解压运行
ssh root@<板卡IP>
cd /opt
tar -zxvf argus-engine-rk3576-linux-arm64.tar.gz
cd argus-engine
./start.sh
```
