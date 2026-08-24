# 引擎运行时与跨进程契约

> 本规范定义图片目录、UDS gRPC 端点、期望状态对账、配置持久化权威、算法包升级和失败恢复。

## 1. Scope / Trigger

修改图片生命周期、Proto、UDS 路径、状态 revision、配置更新、包升级/回滚或进程重启恢复时必须读取本规范。

Go 服务是任务期望状态和算法配置的持久化权威；C++ Engine 是媒体、运行实例和图片物理文件的执行权威。两端不得各自维护可独立修改的第二份业务真相。

## 2. 图片模块契约

### 2.1 持久目录与元数据

Image 模块独占：

```text
<image_root>/
├── .tmp/
├── <yyyy-mm-dd>/<image_id>.jpg
└── catalog/                  # image_id -> relative_path/status 的持久索引
```

目录实现技术可替换，但 catalog 必须在进程重启后恢复以下字段：`image_id`、`relative_path`、`created_at_ns`、`event_id`、`report_status`。其他模块禁止直接写图片根目录或修改 catalog。

### 2.2 原子写入

固定顺序：

```text
create exclusive .tmp/<uuid>.jpg.part
-> write_all
-> fsync(file)
-> close
-> rename(temp, final)        # 同一文件系统
-> fsync(final parent dir)
-> commit catalog entry
```

任一步失败必须删除可安全删除的临时文件，不得发布 catalog entry。启动时清理无 catalog 引用的 `.part` 文件。

对外只暴露 `image_id` 和规范化相对路径；禁止绝对路径、fd 或图片二进制进入常规 gRPC message。

### 2.3 删除与孤儿

`DeleteImages` 按 ID 批量删除并逐项返回：

```text
DeleteImageResult {image_id, status: deleted|already_absent|failed, error?}
```

`deleted` 和 `already_absent` 均为成功。路径必须从 catalog 获取并在打开/删除前验证仍位于 `image_root`；禁止接受对端提供的路径。

告警上报 ACK 后 catalog 标记 `reported`。写图成功但未收到 ACK 的条目是 orphan candidate；重连后通过 `ReportOrphanImages` 报告，由 Go 返回已引用/可删除 ID，再执行清理。Engine 不得仅按时间单方面删除业务可能已引用的图片。

Go 也可主动发起对账：通过 `ReconcileImages` 推送权威保留 ID 集合，Engine 删除不在保留集合中、且非 `unreported` 的孤儿图片并逐项返回结果；两个方向互补（Engine 主动上报走 `ReportOrphanImages`，Go 主动对账走 `ReconcileImages`）。

## 3. UDS 与 Proto 签名

### 3.1 两个明确端点

| Socket | Server | Client | Service |
| --- | --- | --- | --- |
| `engine.sock` | C++ Engine | Go | `EngineService` 控制命令/查询 |
| `app.sock` | Go | C++ Engine | `ControlPlaneService` 期望状态 + `ReportService` 上报 |

生产默认位于 `/var/run/aivision/`；macOS 开发 Profile 可位于仓库外的可写 runtime dir。两个服务不能绑定同一个 UDS 文件。

### 3.2 Service 划分

```text
EngineService (Go -> C++)
  ApplyDesiredState
  UpsertTask
  SetInstanceState
  UpdateInstanceConfig
  InstallPackage / UpgradePackage / RollbackPackage / UninstallPackage
  DeleteImages / ReconcileImages
  QueryProfile / QueryMetrics

ControlPlaneService (C++ -> Go)
  GetDesiredState

ReportService (C++ -> Go)
  ReportAlarm
  ReportTaskState
  ReportInstanceState
  ReportMetrics
  ReportOrphanImages
```

`person.proto` 预留 `PersonService`（当前 `SyncPersons` handler 返回 `UNIMPLEMENTED`；`FeatureService` 为后续扩展）。视频帧、解码图片和张量不得跨进程；未来人员注册图的有界 JPEG 例外必须在独立任务中定义大小限制与安全校验，特征向量等张量字段不得写死进 person 契约。

跨进程响应统一错误契约：transport 错误用 gRPC status 表达；业务失败在响应内携带稳定字符串 `code`（空串=成功，如 `STALE_REVISION`）与仅诊断用途的 `error_message`，客户端不得解析 message 文本（见 error-observability-guidelines §2）。

### 3.3 期望状态与 revision

```text
DesiredState {
  device_id
  revision: uint64
  tasks[]
  instances[]
  active_package_versions[]
}
```

- revision 由 Go 单调递增并持久化。
- Engine 保存最近成功应用的 revision；小于等于当前 revision 的重复请求幂等成功，内容冲突则返回 `STALE_REVISION`。
- Engine 启动或 `app.sock` 重连后主动调用 `GetDesiredState`，再走与 `ApplyDesiredState` 相同的内部 reducer 全量对账。
- 全量对账停止多余实例、创建缺失实例、修正配置和包版本；单项失败必须在响应中逐项报告，不能谎报整个 revision 已成功。

## 4. 配置更新与持久化所有权

更新事务：

```text
Go sends desired revision/config
-> Engine checks revision
-> Engine validates FPS resource candidate
-> Engine validates config Schema
-> plugin instance_update_config
-> Engine atomically swaps sampler/config in memory
-> Engine returns applied revision
-> Go persists/commits desired state only after success
```

若 Go 采用“先写 desired、后调用 Engine”，则失败必须把该 revision 标为未应用并继续向用户展示旧 applied revision，不能把未生效配置展示为已生效。具体数据库事务属于 Go 任务，跨进程语义必须保持上述区分。

Engine 重启不从本地配置文件恢复任务，而是向 Go 拉取最新 DesiredState。

## 5. 包安装、升级与回滚

安装只接受已通过独立 `package_validator` 的 staging 目录：

```text
validate in staging
-> fsync files/directories
-> atomic rename to var/packages/<algorithm_id>/<version>
-> register inactive version
```

升级状态机：

```text
validated
-> stop only instances using algorithm_id
-> flush and destroy those instances
-> activate new version
-> recreate with previous applied configs
-> success: retain one rollback version
-> failure: reactivate old version and recreate instances
```

- 摄像头拉流、解码和其他算法实例不得中断。
- 有任务 desired state 引用 `algorithm_id` 时禁止卸载。
- 同一 `algorithm_id + platform_id` 同时只能有一个 active version。
- 失败回滚也失败时进入明确 `degraded` 状态并逐项上报，禁止假装恢复成功。

## 6. 上报与断线语义

`ReportAlarm` 使用 Engine 生成的全局 event ID，Go 必须幂等。RPC 失败时图片保持 `unreported` 并成为 orphan candidate；任务继续本地分析。

当前规范不承诺跨 Engine 进程崩溃的告警消息可靠投递。若产品要求告警零丢失，必须单独引入有容量、fsync 和磁盘水位策略的 durable outbox，并增加对应 PRD/AC；禁止把无界内存队列描述为可靠缓存。

## 7. Validation & Error Matrix

| 条件 | 结果 |
| --- | --- |
| socket 文件已被活进程占用 | 启动失败，不能 unlink 后抢占 |
| socket 是遗留且 owner 进程不存在 | 安全清理后绑定，并记录恢复日志 |
| DesiredState revision 重复且内容一致 | 幂等成功 |
| revision 过期或同 revision 内容冲突 | `STALE_REVISION` |
| 配置任一级校验失败 | 旧 applied config/revision 保持不变 |
| 图片已不存在 | DeleteImages 返回 `already_absent` |
| 上报失败 | 图片标记 unreported，不删除 |
| 新包初始化失败 | 回滚旧版本并恢复受影响实例 |
| 包仍被 desired state 引用 | `PACKAGE_IN_USE` |

## 8. Good / Base / Bad Cases

- Good：Engine 重启后调用 `GetDesiredState(revision=...)`，用同一 reducer 恢复实例。
- Base：重复 DeleteImages 返回 `already_absent`，调用整体成功。
- Bad：C++ 把算法配置写入第二份本地 JSON，并在重启时覆盖 Go 的期望状态。

## 9. Tests Required

- 两个 UDS 端点、遗留 socket、权限、重连和 graceful shutdown 测试。
- DesiredState 重复/过期/部分失败/全量删除/重启恢复测试。
- 合法与非法配置、插件拒绝、资源拒绝及 applied/desired revision 测试。
- 图片写入各失败点、fsync/rename、catalog 恢复、双删和路径逃逸测试。
- 上报 ACK、断线 orphan、对账确认和幂等 event ID 测试。
- 升级只影响目标算法、初始化失败回滚、回滚失败 degraded 和引用卸载保护测试。
- Proto lint：任何常规 message 不得出现 frame/tensor/image bytes 字段。

## 10. Wrong vs Correct

```text
Wrong: C++ 重连后调用位于 C++ 自己服务上的 ApplyDesiredState
Correct: C++ 调用 Go 的 GetDesiredState，再调用本地共享 apply reducer
```

```cpp
// Wrong: 直接写最终文件，读者可能看到半文件
write_file(final_path, jpeg);

// Correct: 临时文件 fsync 后同文件系统 rename，并 fsync 父目录
write_fsync_close(temp_path, jpeg);
rename(temp_path.c_str(), final_path.c_str());
fsync_parent(final_path);
```
