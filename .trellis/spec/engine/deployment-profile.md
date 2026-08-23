# 部署 Profile 规范

> 每个 `platform_id` 必须交付独立部署 Profile。Profile 固定运行时组合、目录、UDS、媒体参数、看门狗、资源门槛、进程管理和升级回滚要求。

## 1. Scope / Trigger

新增平台、升级 ZLMediaKit/推理运行时、修改路径/端点、watchdog、资源门槛、进程用户或打包方式时必须读取本规范。

## 2. Signatures

Profile 使用版本化 JSON，路径通过唯一环境变量传入：

```text
AIVISION_ENGINE_PROFILE=/etc/aivision/engine-profile.json
```

```json
{
  "schema_version": 1,
  "platform_id": "macos-arm64-coreml",
  "adapter_version": "1.0.0",
  "paths": {
    "runtime_dir": "/var/run/aivision",
    "package_root": "/var/lib/aivision/packages",
    "image_root": "/var/lib/aivision/images",
    "log_root": "/var/log/aivision"
  },
  "ipc": {
    "engine_socket": "engine.sock",
    "app_socket": "app.sock"
  },
  "media": {
    "backend": "zlmediakit",
    "commit": "<full commit sha>",
    "config_file": "/etc/aivision/zlm.ini"
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
    "source": "development-estimate"
  }
}
```

相对 socket 名只能位于 `runtime_dir`。环境变量不得逐项覆盖安全关键路径或平台 ID，避免部署实际值无法审计。

## 3. Contracts

每个 Profile 必须固定并记录：

- CPU 架构、OS/BSP 最低/已验证版本；
- adapter、媒体后端、推理运行时、图像库和驱动版本；
- 服务拓扑、进程管理器、运行用户/组和目录权限；
- 配置路径、两个 UDS、日志轮转、文件描述符/进程资源限制；
- watchdog、重连、validator 解压/文件数/超时上限；
- package/image 目录容量和升级/回滚步骤；
- 不支持或降级的能力及原因。

### 3.1 macOS Profile

`macos-arm64-coreml` 只支持 Apple Silicon，交付物至少包括：

```text
deploy/macos/engine-profile.json
deploy/macos/com.aivision.engine.plist
deploy/macos/zlm.ini
engine/docs/deployment-macos.md
```

launchd 配置必须：

- 使用绝对二进制和 Profile 路径；
- 设置独立工作目录，但 Engine/算法包不得依赖 cwd；
- 定义 stdout/stderr 或统一日志接入方式；
- 使用 `KeepAlive`/退出策略，SIGTERM 下走有序停止；
- 预创建 runtime/package/image/log 目录和最小权限；
- 不在 plist 中写摄像头凭据、token 或模型密钥。

开发 Profile 可使用仓库外的临时可写目录，但不得覆盖生产 Profile。

### 3.2 升级与回滚

升级顺序固定为：校验新二进制/配置 -> 停止服务 -> 原子切换版本 -> 启动 -> 查询 profile/health -> 失败时切回旧版本。算法包版本切换仍由运行时包管理状态机负责，不能与 Engine 二进制升级混为一笔事务。

## 4. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| Profile 缺字段、未知 schema version | Engine 启动失败 |
| 当前 arch/OS 不满足 | `PACKAGE_INCOMPATIBLE`/平台启动失败 |
| 两个 socket 路径相同或逃逸 runtime dir | 启动失败 |
| `allocatable + reserved > total` | 启动失败 |
| ZLM commit/config 与构建报告不一致 | 启动失败或明确 degraded，不能静默 |
| 目录不可写或权限过宽 | 启动前检查失败 |
| launchd 收到 SIGTERM | 停止入流、排空、join，在超时内退出 |

## 5. Good / Base / Bad Cases

- Good：生产 Profile 固定 ZLM full SHA、Core ML 最低 OS 和两个独立 UDS。
- Base：开发 Profile 使用 `$TMPDIR` 下目录，但 schema 和安全检查与生产一致。
- Bad：运行时从一组未记录的环境变量拼接平台 ID、路径和 timeout。

## 6. Tests Required

- Profile parser 的缺字段、未知字段、范围、路径逃逸和版本兼容测试。
- 当前机器 capability 与 Profile 匹配/不匹配测试。
- launchd plist lint、首次目录创建、重启、SIGTERM 和遗留 socket 测试。
- 新版本健康检查失败后的二进制/配置回滚演练。
- 构建报告中的媒体/运行时版本与 QueryProfile 输出一致性测试。

## 7. Wrong vs Correct

```xml
<!-- Wrong: 相对路径和 secret 写入 plist -->
<string>./build/aivision-engine</string>
<key>CAMERA_PASSWORD</key><string>secret</string>

<!-- Correct: 只传版本化 Profile 的绝对路径 -->
<string>/opt/aivision/bin/aivision-engine</string>
<key>AIVISION_ENGINE_PROFILE</key>
<string>/etc/aivision/engine-profile.json</string>
```
