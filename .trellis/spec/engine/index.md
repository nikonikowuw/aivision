# C++ 媒体推理引擎与算法包规范

> 本目录是 `engine/`、`sdk/`、`algo-packages/` 的实现权威。任务 PRD/Design 说明为什么做；这里说明代码必须如何写、边界如何验证。发生冲突时，先回到规划修订决策，再同步本目录，禁止在实现中私自选择。

## 1. 规范索引

| 规范 | 权威范围 | 状态 |
| --- | --- | --- |
| [目录结构与构建边界](./directory-structure.md) | CMake target、`media_api/media_zlm`、SDK ABI/toolkit 分离、可搬运性 | 生效 |
| [C ABI 与帧生命周期](./abi-guidelines.md) | 152B 帧描述符、`frame_token`、枚举、色彩、版本演进 | 生效，ABI v1 冻结前仍需代码评审 |
| [算法包 C ABI 接口](./algo-package-spec.md) | 单导出、Library/Instance、self-test、结果回调、线程与所有权 | 生效，ABI v1 冻结前仍需代码评审 |
| [清单、配置与结果 Schema](./manifest-schema.md) | manifest、入口文件 SHA、离散 FPS 档位、配置和结果 JSON | 生效 |
| [平台适配、媒体与调度](./platform-guidelines.md) | 平台接口、ZLM 生命周期、图像原语、队列、watchdog、资源和指标 | 生效 |
| [运行时与跨进程契约](./runtime-guidelines.md) | 图片 catalog、双 UDS、DesiredState revision、升级回滚 | 生效 |
| [错误、日志与可观测性](./error-observability-guidelines.md) | EngineError、稳定错误码、日志字段、指标与脱敏 | 生效 |
| [质量与算法包工程化](./quality-guidelines.md) | sanitizer、fixture、validator、runner、E2E 门禁 | 生效 |
| [部署 Profile](./deployment-profile.md) | Profile Schema、macOS launchd、目录权限、版本组合和升级回滚 | 生效 |

## 2. 核心边界

1. **依赖倒 V**：`engine -> sdk <- algo-packages`；Engine 不编译或静态链接具体算法源码。
2. **核心不依赖具体媒体/硬件**：`engine_core` 只依赖 `media_api` 与 `platform_api`；ZLMediaKit 位于 `media_zlm`，平台 Framework 位于 `platform_*`。
3. **SDK 分层**：`sdk/include/argus/*.h` 是纯 C ABI；平台 C++/Objective-C++ 辅助实现位于 `sdk/toolkit/`，不能污染 ABI 头。
4. **推理归插件**：模型加载、推理运行时和实例推理上下文归算法包；平台只提供能力档案、帧、图像原语、资源和指标。
5. **帧双句柄**：`opaque` 是只读平台句柄；`frame_token` 只供 `av_frame_ops retain/release`，二者不得混用。
6. **两种结果**：正常实例按帧最多回调一个检测批次；Engine 将批次 fan-out 为零/多条目标级告警。安装 self-test 实例必须返回恰好一条 self-test 结果，零检测仍可成功。
7. **状态所有权**：Go 持久化 DesiredState/config revision；Engine 执行并报告 applied revision，不维护第二份业务配置真相。
8. **进程隔离安装**：主进程通过 `posix_spawn/exec` 启动独立 validator；禁止多线程进程 fork 后直接加载 Core ML/插件。
9. **图片单一管理方**：Engine image 模块拥有 catalog、原子写入和幂等删除；算法只提交 ROI，Go 只持有 ID/受限相对路径。
10. **部署可审计**：每个 `platform_id` 必须固定媒体、运行时、目录、socket、watchdog、资源与进程管理组合。

## 3. 开发前 Gate

开始实现前逐项确认：

- [ ] 已读取目标规范和 task 的 PRD/Design/Implement；未决策项没有被当成默认实现。
- [ ] 源码包含标准 Doxygen 文件头，业务代码、复杂状态机与并发队列包含清晰的中文注释。
- [ ] 新 CMake target 符合 [目录边界矩阵](./directory-structure.md)。
- [ ] ABI 变更同时更新 C 头、offset 断言、双编译器测试、版本与 vendored SDK。
- [ ] 新 manifest/Proto/JSON 字段只有一个解析与校验所有者，没有多层复制契约。
- [ ] 新平台私有类型只出现在平台实现或 toolkit 平台目录。
- [ ] 媒体回调的所有权、队列容量、delegate 移除和 shutdown 顺序已设计。
- [ ] 配置更新区分 desired/applied revision，并先通过资源候选账本。
- [ ] 安装/解压/加载逻辑运行在独立 validator，已有大小、路径、类型和超时上限。
- [ ] 测试使用固定 fixture/fake clock，不依赖公共摄像头或真实 sleep。
- [ ] 对应部署 Profile 和能力档案字段已同步。

## 4. Quality Check

最低验证命令：

```bash
make -C engine configure
make -C engine build
make -C engine test
make -C engine asan
make -C engine tsan
make -C engine lint
bash algo-packages/scripts/check-consistency.sh
```

涉及真实 macOS 包时再运行：

```bash
make -C algo-packages/macos/arm64/yolov8n build
make -C algo-packages/macos/arm64/yolov8n run
make -C algo-packages/macos/arm64/yolov8n benchmark
make -C algo-packages/macos/arm64/yolov8n package
make -C engine e2e
```

报告完成前必须给出：执行过的命令、结果、跳过项和跳过原因。仅构建成功不能替代 ABI、sanitizer、可搬运性、媒体停止流程和 E2E 验证。

## 5. 规范更新规则

- 新的跨层签名、字段、错误或环境变量必须按“Scope、Signatures、Contracts、Error Matrix、Cases、Tests、Wrong/Correct”深度更新相应 spec。
- 已实现的稳定契约进入 spec；一次性进度、风险与里程碑留在 task 文档。
- task 发现 spec 冲突时，先修规划和 spec，再实现；不得让代码成为第三套事实来源。
- 所有 Engine spec 使用中文；代码标识、命令、字段名保持项目语言。
