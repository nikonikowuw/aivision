# C++ Engine 日志与 systemd/journald 生产部署说明

## 1. 部署架构与服务配置

在 Linux / 边缘计算宿主机（如 RK3576 / Atlas / Linux x86/ARM）上，`argus-engine` 主进程与子进程统一通过 `stderr` 写入单行合法 JSONL。

`argus-engine` 主进程与 `package_validator` 子进程均将结构化日志写入各自的 `stderr`。Validator 不把日志合并到机器结果 `stdout`；Engine 只捕获并解析 validator 的有限 JSON 结果，validator 的 `stderr` 继承宿主进程的 stderr，因此两者都进入同一个 systemd unit 的 journald 流。

> 完整生产环境 FHS 目录规划、部署 Profile 及 Go 后端编排配置，请参阅 [`deploy/README.md`](../../deploy/README.md)。

### 1.1 systemd unit 文件示例 (`/etc/systemd/system/argus-engine.service`)

```ini
[Unit]
Description=Argus Edge AI Media & Inference Engine
After=network.target local-fs.target

[Service]
Type=simple
User=argus
Group=argus
WorkingDirectory=/var/lib/argus
ExecStart=/usr/local/bin/argus-engine

# 环境变量配置：Profile 路径与日志过滤阈值
Environment=ARGUS_ENGINE_PROFILE=/etc/argus/engine-profile.json
Environment=ARGUS_LOG_LEVEL=INFO

# 标准输入输出配置：
# 机器日志统一交付给 journald，不额外写独立文件
StandardOutput=null
StandardError=journal
SyslogIdentifier=argus-engine

# 进程与沙箱权限
Restart=always
RestartSec=3s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

---

## 2. 日志检索与常用排障命令 (`journalctl`)

由于 Engine 产生严格的一行一条 JSONL 格式日志，可以通过 `journalctl` 配合 `jq` 工具进行高效结构化排障。

### 2.1 查看 journald 原始 JSON 记录
```bash
# -o json 返回 journald 外层 JSON；结构化业务日志位于 MESSAGE 字段
journalctl -u argus-engine -n 100 -o json | jq -r '.MESSAGE'
```

### 2.2 查看实时结构化日志流
```bash
# 实时跟踪输出
journalctl -u argus-engine -f -o cat
```

### 2.3 筛选特定错误码与高级别日志
```bash
# 筛选所有 warning / error / fatal 级别的事件
journalctl -u argus-engine -o cat | jq 'select(.level=="error" or .level=="warn" or .level=="fatal")'

# 查找所有算法超时事件
journalctl -u argus-engine -o cat | jq 'select(.code=="ALGO_PROCESS_TIMEOUT")'
```

### 2.4 按相机与任务上下文过滤
```bash
# 追踪特定相机的全生命周期日志
journalctl -u argus-engine -o cat | jq 'select(.camera_id=="cam_east_01")'

# 追踪特定算法实例执行流
journalctl -u argus-engine -o cat | jq 'select(.instance_id=="inst_yolo_01")'
```

### 2.5 子进程 (Package Validator) 故障排查
```bash
# 过滤沙箱包校验子进程输出
journalctl -u argus-engine -o cat | jq 'select(.component=="validator")'
```
