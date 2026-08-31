# Argus 生产部署与运维指南 (Production Deployment & Operations Guide)

本文档面向系统管理员、边缘计算运维工程师及开发者，详细介绍 `Argus` 系统的多进程架构、Linux FHS 规范目录规划、版本化 Profile 配置、systemd 守护进程托管、Docker 容器化以及边缘硬件适配。

---

## 1. 系统架构与通信拓扑

Argus 采用**控制面（Go 后端）**与**数据面（C++ 媒体推理引擎）**分离的双进程架构：

```text
               ┌────────────────────────────────────────────────────────┐
               │                  Nginx (端口 80 / 443)                 │
               └──────────────┬──────────────────────────┬──────────────┘
                              │ 静态资源 / HTTP API       │ 实时视频流 WebRTC/WS-FLV
                              ▼                          ▼
               ┌────────────────────────┐      ┌────────────────────────┐
               │  Go 后端 (argus-backend)│      │ C++ 引擎 (argus-engine) │
               │     端口 :8000          │      │   媒体流端口 :8080      │
               └───────────┬────────────┘      └───────────┬────────────┘
                           │                               │
                           │◄─────── 双向 UDS gRPC 通信 ───►│
                           │  - /var/run/argus/engine.sock │
                           │  - /var/run/argus/app.sock    │
                           │                               │
                           ▼                               ▼
               ┌────────────────────────┐      ┌────────────────────────┐
               │ 嵌入式 SQLite 数据库    │      │  package_validator     │
               │ (WAL模式, data/argus.db)│      │  算法包 7 步安全沙箱子进程│
               └────────────────────────┘      └────────────────────────┘
```

---

## 2. Linux FHS 标准目录规范

为保证系统的安全性、可审计性及不同发行版间的一致性，生产环境必须严格遵循 Linux FHS（Filesystem Hierarchy Standard）目录规范：

| 绝对路径 | 属主与权限 | 对应配置项 | 用途与存储内容 |
| :--- | :--- | :--- | :--- |
| `/etc/argus/` | `root:argus` (0750) | `--` | **静态配置文件**：`engine-profile.json`、`config.yaml`、`zlm.ini` |
| `/var/run/argus/` (`/run/argus/`) | `argus:argus` (0750) | `paths.runtime_dir` | **运行态通信目录**：存放 `engine.sock`、`app.sock` 及 PID 文件 |
| `/var/lib/argus/data/` | `argus:argus` (0750) | `db.path` | **数据库持久化目录**：嵌入式 SQLite 数据库文件（`argus.db`、`argus.db-wal`、`argus.db-shm`） |
| `/var/lib/argus/packages/` | `argus:argus` (0750) | `paths.package_root` | **算法包持久化根目录**：解压后的模型权重、动态库、激活版本标记 (`active/`) |
| `/var/lib/argus/images/` | `argus:argus` (0750) | `paths.image_root` | **抓拍与告警图片库**：告警抓拍原图、缩略图及 `.tmp` 临时交换区 |
| `/var/lib/argus/uploads/` | `argus:argus` (0750) | `storage.local.root` | **Web 管理上传区**：人脸底库、组织架构头像等通用文件 |
| `/var/log/argus/` | `argus:argus` (0750) | `paths.log_root` | **日志输出目录**：主进程与子进程运行日志（配合 journald 轮转） |

### 目录一键初始化与授权脚本

```bash
#!/usr/bin/env bash
set -euo pipefail

# 1. 创建专用低特权系统用户 argus
if ! id -u argus >/dev/null 2>&1; then
    sudo useradd -r -s /usr/sbin/nologin -d /var/lib/argus -m -c "Argus Edge AI Service" argus
fi

# 2. 创建 FHS 标准目录结构
sudo mkdir -p /etc/argus \
              /var/run/argus \
              /var/lib/argus/data \
              /var/lib/argus/packages \
              /var/lib/argus/images \
              /var/lib/argus/uploads \
              /var/log/argus

# 3. 设置最小安全权限
sudo chown -R root:argus /etc/argus
sudo chmod 750 /etc/argus

sudo chown -R argus:argus /var/run/argus /var/lib/argus /var/log/argus
sudo chmod 750 /var/run/argus /var/lib/argus /var/log/argus
```

---

## 3. 部署配置文件详解

### 3.1 引擎部署 Profile：`/etc/argus/engine-profile.json`

生产环境下，Engine 与 Go 后端通过该文件强约束运行拓扑与端点，**禁止通过散落的环境变量随意覆盖单个 socket 或平台 ID**。

```json
{
  "schema_version": 1,
  "platform_id": "linux-arm64-rknn",
  "adapter_version": "1.0.0",
  "paths": {
    "runtime_dir": "/var/run/argus",
    "package_root": "/var/lib/argus/packages",
    "image_root": "/var/lib/argus/images",
    "log_root": "/var/log/argus"
  },
  "ipc": {
    "engine_socket": "engine.sock",
    "app_socket": "app.sock"
  },
  "media": {
    "backend": "zlmediakit",
    "commit": "3196f7c",
    "config_file": "/etc/argus/zlm.ini"
  },
  "watchdog": {
    "ingest_timeout_ms": 5000,
    "decoder_stall_timeout_ms": 3000,
    "reconnect_backoff_ms": [1000, 2000, 4000, 8000, 16000, 30000]
  },
  "resource": {
    "total_units": 1000,
    "allocatable_units": 900,
    "reserved_units": 100,
    "min_free_memory_mb": 512,
    "source": "production-profile"
  }
}
```

> **安全规则**：`ipc.engine_socket` 与 `ipc.app_socket` 必须为**相对路径**，由引擎安全解析在 `paths.runtime_dir` 内，防止路径穿越逃逸。

---

### 3.2 后端应用配置：`/etc/argus/config.yaml`

```yaml
server:
  port: 8000

db:
  path: /var/lib/argus/data/argus.db
  busy_timeout: 5000 # 毫秒（WAL 并发等待）
  max_open: 20
  max_idle: 5
  max_lifetime: 1h

jwt:
  secret: "YourRandomSecure256BitSecretKey" # 生产必须通过 APP_JWT_SECRET 环境变量覆盖
  access_ttl: 2h
  refresh_ttl: 168h
  secure_cookie: true

storage:
  driver: local # local | minio
  max_size: 10485760 # 10MB
  local:
    root: /var/lib/argus/uploads
    url_prefix: /uploads

# 生产 IPC 端点统一关联 Engine Profile
ipc:
  profile_path: /etc/argus/engine-profile.json
```

---

## 4. systemd 服务托管（生产推荐）

在 Linux 生产环境中，利用 systemd 的 `RuntimeDirectory`、`StateDirectory`、`LogsDirectory` 特性，可以免去手动维护目录生命周期与重启守护。

### 4.1 C++ 媒体推理引擎服务 (`/etc/systemd/system/argus-engine.service`)

```ini
[Unit]
Description=Argus AI Media & Inference Engine
After=network.target local-fs.target
Wants=network-online.target

[Service]
Type=simple
User=argus
Group=argus
WorkingDirectory=/var/lib/argus

# systemd 自动创建并管理 FHS 目录权限
RuntimeDirectory=argus
RuntimeDirectoryMode=0750
StateDirectory=argus argus/packages argus/images
StateDirectoryMode=0750
LogsDirectory=argus
LogsDirectoryMode=0750

# 唯一加载 Profile 配置
Environment=ARGUS_ENGINE_PROFILE=/etc/argus/engine-profile.json
Environment=ARGUS_LOG_LEVEL=INFO
Environment=ARGUS_LIVE_HTTP_PORT=8080

ExecStart=/usr/local/bin/argus-engine

# 进程监控与优雅退出
Restart=always
RestartSec=3s
LimitNOFILE=65536
KillMode=mixed
KillSignal=SIGTERM
TimeoutStopSec=15s

# 结构化 JSONL 日志输出至 journald
StandardOutput=null
StandardError=journal
SyslogIdentifier=argus-engine

[Install]
WantedBy=multi-user.target
```

### 4.2 Go 后端控制面服务 (`/etc/systemd/system/argus-backend.service`)

```ini
[Unit]
Description=Argus Backend Control Plane & API Service
After=network.target argus-engine.service
Wants=argus-engine.service

[Service]
Type=simple
User=argus
Group=argus
WorkingDirectory=/var/lib/argus

# systemd 自动维护目录
StateDirectory=argus/data argus/uploads
StateDirectoryMode=0750

# 配置与 Profile 入口
Environment=APP_CONFIG_PATH=/etc/argus/config.yaml
Environment=AIVISION_ENGINE_PROFILE=/etc/argus/engine-profile.json

# 敏感凭据注入（建议通过 chmod 600 的 EnvironmentFile 或 systemd-creds 注入）
# EnvironmentFile=/etc/argus/secrets.env

ExecStart=/usr/local/bin/argus-backend

Restart=always
RestartSec=3s
LimitNOFILE=65536
KillSignal=SIGTERM
TimeoutStopSec=10s

StandardOutput=journal
StandardError=journal
SyslogIdentifier=argus-backend

[Install]
WantedBy=multi-user.target
```

---

## 5. 服务启动与常用运维命令

### 5.1 服务加载与启动

```bash
# 重新加载 systemd 配置
sudo systemctl daemon-reload

# 设置开机自启
sudo systemctl enable argus-engine argus-backend

# 依次启动引擎与后端服务
sudo systemctl start argus-engine
sudo systemctl start argus-backend

# 检查服务运行状态
sudo systemctl status argus-engine argus-backend
```

### 5.2 结构化日志实时排查 (`journalctl` + `jq`)

Argus 引擎产生严格的单行合法 JSONL 日志，配合 `jq` 工具可进行高维度过滤排障：

```bash
# 1. 实时跟踪引擎日志流
journalctl -u argus-engine -f -o cat

# 2. 仅筛选 warning / error / fatal 级别的事件
journalctl -u argus-engine -o cat | jq 'select(.level=="error" or .level=="warn" or .level=="fatal")'

# 3. 追踪特定相机的处理流与事件
journalctl -u argus-engine -o cat | jq 'select(.camera_id=="cam_entrance_01")'

# 4. 追踪算法沙箱校验 (package_validator) 过程日志
journalctl -u argus-engine -o cat | jq 'select(.component=="validator")'

# 5. 查看 Go 后端控制面日志
journalctl -u argus-backend -f -o cat
```

---

## 6. Nginx 反向代理配置 (`/etc/nginx/sites-available/argus.conf`)

```nginx
server {
    listen 80;
    server_name argus.local; # 替换为您的域名或 IP

    client_max_body_size 50M;

    # 1. 前端静态单页应用托管
    location / {
        root /var/www/argus-ui;
        index index.html;
        try_files $uri $uri/ /index.html;
    }

    # 2. 后端 REST API 与 Swagger 代理
    location /api/ {
        proxy_pass http://127.0.0.1:8000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # 3. 引擎实时流媒体代理 (WebRTC / WebSocket-FLV)
    location /live/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "Upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }
}
```

---

## 7. 算法包发布与热更新流程

生产环境中，算法模型以**独立分发包**（`.tar.gz` 包含 `manifest.json`、`config.schema.json`、`lib/*.so`、`model/*` 及 `testimage.jpg`）形式通过 Web 界面或 API 上传：

1. **上传与接收**：Go 后端接收算法包后，通过 UDS 调用引擎 `InstallPackage` 接口。
2. **7 步沙箱安全校验**：
   - 引擎拉起独立的低特权子进程 `package_validator`；
   - 审计动态库符号表纯洁性（禁止 `fork`、`exec`、网络及未授权调用）；
   - 使用自带的 `testimage.jpg` 执行全流程推理自检；
   - 校验通过后原子解压安装至 `/var/lib/argus/packages/<algorithm_id>/<version>/`。
3. **版本激活与热切换**：
   - 写入 `/var/lib/argus/packages/active/<algorithm_id>.version` 标记当前激活版本；
   - 正在运行的分析任务通过原子引用计数安全切换至新版本，旧版本动态库等待所有帧推理完成后安全 `dlclose`。

---

## 8. 常见故障排查表 (Troubleshooting)

| 异常现象 | 可能原因 | 排查与处置步骤 |
| :--- | :--- | :--- |
| **`ENGINE_UDS_START_FAILED`** | `/var/run/argus/` 目录不存在或 `argus` 用户无写权限；残留旧 `.sock` 文件被独占 | 检查 `ls -ld /var/run/argus` 权限；删除残留 socket 并确保 systemd 正确设置了 `RuntimeDirectory=argus`。 |
| **`ENGINE_PROFILE_ENV_CONFLICT`** | 同时设置了 `ARGUS_ENGINE_PROFILE` 与 `ARGUS_ENGINE_SOCKET` | 移除多余的散落环境变量，生产环境强制只保留单一 Profile 配置。 |
| **后端提示 `image stream read failed`** | Go 后端与 Engine 的 `image_root` 路径不一致 | 检查 `/etc/argus/engine-profile.json` 中的 `image_root` 是否为 `/var/lib/argus/images`，并确保 Go 能够读取。 |
| **算法包安装校验超时 / 失败** | `package_validator` 依赖缺失；NPU 驱动版本不匹配；自检推理耗时超过沙箱阈值 | 执行 `journalctl -u argus-engine -o cat \| jq 'select(.component=="validator")'` 查看沙箱详细输出。 |
| **视频流播放黑屏或卡顿** | 摄像头 RTSP 流不可达；解码器硬解资源耗尽 | 查看引擎日志中 `ingest_timeout` 或 `decoder_stall` 错误码；调整 Profile 中的 `watchdog` 参数。 |
