# C++ Engine 日志与 systemd/journald 生产部署说明

## 1. 部署架构与服务配置

在 Linux / 边缘计算宿主机（如 RK3576 / Atlas / Linux x86/ARM）上，`aivision-engine` 主进程与子进程统一通过 `stderr` 写入单行合法 JSONL。

`systemd` 与 `journald` 负责接收、追加标准时间戳、轮转、持久化与防磁盘写满保护。

### 1.1 systemd unit 文件示例 (`/etc/systemd/system/aivision-engine.service`)

```ini
[Unit]
Description=AIVision Edge AI Media & Inference Engine
After=network.target local-fs.target

[Service]
Type=simple
User=aivision
Group=aivision
WorkingDirectory=/opt/aivision/engine
ExecStart=/opt/aivision/engine/bin/aivision-engine

# 环境变量配置：日志过滤阈值 (debug | info | warn | error | fatal)
Environment="AIVISION_LOG_LEVEL=info"

# 标准输入输出配置：
# 机器日志统一交付给 journald，不额外写独立文件
StandardOutput=null
StandardError=journal
SyslogIdentifier=aivision-engine

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

### 2.1 查看实时结构化日志流
```bash
# 实时跟踪输出
journalctl -u aivision-engine -f -o cat
```

### 2.2 筛选特定错误码与高级别日志
```bash
# 筛选所有 warning / error / fatal 级别的事件
journalctl -u aivision-engine -o cat | jq 'select(.level=="error" or .level=="warn" or .level=="fatal")'

# 查找所有算法超时事件
journalctl -u aivision-engine -o cat | jq 'select(.code=="ALGO_PROCESS_TIMEOUT")'
```

### 2.3 按相机与任务上下文过滤
```bash
# 追踪特定相机的全生命周期日志
journalctl -u aivision-engine -o cat | jq 'select(.camera_id=="cam_east_01")'

# 追踪特定算法实例执行流
journalctl -u aivision-engine -o cat | jq 'select(.instance_id=="inst_yolo_01")'
```

### 2.4 子进程 (Package Validator) 故障排查
```bash
# 过滤沙箱包校验子进程输出
journalctl -u aivision-engine -o cat | jq 'select(.component=="validator")'
```
