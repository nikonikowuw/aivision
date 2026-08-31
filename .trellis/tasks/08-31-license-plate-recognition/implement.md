# 车牌识别 MVP 实施计划

## 1. 执行原则

- 按“契约 -> Engine 通路 -> 算法包 -> Go 持久化/API -> 前端 -> E2E”顺序实施。
- 每阶段先写失败测试或契约 fixture，再修改实现。
- 只实现 PRD 的通行记录 MVP；发现名单、告警、道闸或自动清理需求时退回规划，不在实现中顺带加入。
- 现有未提交改动不得回退；与本任务冲突时基于最新工作区状态调整。

## 2. 阶段一：契约与规范

- [ ] 更新 `.trellis/spec/engine/manifest-schema.md`：新增 `license_plate_recognition` 条件字段规则和 `plates` schema，保留 `face_recognition/persons`。
- [ ] 更新 `.trellis/spec/engine/algo-package-spec.md`：明确 `AV_RESULT_RECOGNITION` 按算法类型选择 payload parser、图片 ROI 和回调规则。
- [ ] 更新 `.trellis/spec/engine/runtime-guidelines.md`：加入 `ReportPlateObservation`、双图片引用和幂等语义。
- [ ] 更新 `.trellis/spec/backend/` 对应数据库/质量规范，记录 `plate_observations` 的幂等键、软删除和图片读取约束。
- [ ] 扩展 `sdk/cmake/validate_package.cmake` 与 Engine manifest parser 的允许类型和 `alarm_type_id` 条件规则。
- [ ] 增加 manifest 正反例、ABI metadata 和旧类型回归测试。

验证：

```bash
bash algo-packages/scripts/check-consistency.sh
make -C engine test
```

回滚点：本阶段只改变契约、validator 和测试；若兼容性失败，撤销新类型支持，不修改 ABI 结构布局和已有枚举值。

## 3. 阶段二：Proto 与 Engine 上报通路

- [ ] 在 `engine/proto/argus/v1/app.proto` 增加 `PlateObservation`、请求/响应和 `ReportPlateObservation` RPC。
- [ ] 重新生成 C++/Go protobuf 代码并更新 descriptor smoke test。
- [ ] 在 Engine 增加车牌 recognition JSON parser，严格校验字段、有限值、bbox、数组和大小。
- [ ] 根据 `algorithm_type` 路由 `persons` 与 `plates` parser，增加错误类型错配测试。
- [ ] 为每个车牌观测生成稳定全局 event ID，复制结构化字段和图片请求。
- [ ] 扩展异步上报批次，生成一张全景图和可选车牌裁剪图，复用 image catalog 和帧引用管理。
- [ ] 实现 C++ UDS client 的 `ReportPlateObservation`，处理 ACK、失败和重连。
- [ ] 覆盖队列满、RPC 阻塞、停机、图片写入失败、重复事件和 orphan 对账测试。

验证：

```bash
make -C engine configure
make -C engine build
make -C engine test
make -C engine asan
make -C engine tsan
make -C engine lint
```

风险文件：

- `sdk/include/argus/result.h`
- `sdk/toolkit/include/argus/utils/json.hpp`
- `engine/proto/argus/v1/*.proto`
- `engine/src/core/ipc/uds_server.cpp`
- `engine/src/core/ipc/uds_client.cpp`
- `engine/src/core/algo/algo_sandbox.cpp`

回滚点：Proto 字段号发布后不得复用；未发布前可以整体撤销 RPC。图片 worker 的改动必须保持既有告警路径测试通过。

## 4. 阶段三：macOS/Core ML 算法包

- [ ] 创建 `algo-packages/macos/arm64/license_plate_recognition/`，沿用现有 macOS 包目录和 CMake helper。
- [ ] 确定可再分发的车牌检测/OCR模型来源，记录许可证、SHA-256、输入输出 shape 和转换证据。
- [ ] 编写 Core ML 转换脚本和固定模型契约验证；模型文件不允许依赖仓库外绝对路径。
- [ ] 实现 NV12/CVPixelBuffer 到原图 RGB、640x640 letterbox 和坐标反变换。
- [ ] 实现车牌/车辆检测后处理、NMS、关联和原图裁剪/透视矫正。
- [ ] 实现 OCR、颜色/类型分类、文本规范化、track 投票和去重窗口。
- [ ] 实现 `AV_RESULT_RECOGNITION` JSON 和全景/裁剪 ROI 请求。
- [ ] 实现 `.env` 默认配置、`config.schema.json` 和原子热更新。
- [ ] 实现真实 self-test、runner、benchmark、可视化结果和 package target。
- [ ] 添加白天、夜间、倾斜、模糊、蓝/黄/绿牌、无牌和多目标 fixtures；记录来源和许可。

验证：

```bash
make -C algo-packages/macos/arm64/license_plate_recognition configure
make -C algo-packages/macos/arm64/license_plate_recognition build
make -C algo-packages/macos/arm64/license_plate_recognition test
make -C algo-packages/macos/arm64/license_plate_recognition asan
make -C algo-packages/macos/arm64/license_plate_recognition run
make -C algo-packages/macos/arm64/license_plate_recognition benchmark
make -C algo-packages/macos/arm64/license_plate_recognition package
bash algo-packages/scripts/check-consistency.sh
```

必须保存：模型转换输入输出、固定样例识别结果、准确率/召回率/误识率、P50/P95 延迟、持续 FPS、CPU/内存和包可搬运性证据。

回滚点：模型或许可不可验证时不提交可发布二进制，只保留明确标注的研究/转换证据；不得用 mock 结果替代真实模型验收。

## 5. 阶段四：Go IPC 与持久化

- [ ] 新增版本化 SQL migration（up/down）创建 `plate_observations` 及索引。
- [ ] 新增 `model.PlateObservation` 并加入 sqlite 测试 AutoMigrate 列表。
- [ ] 新增 repository：幂等创建、ID 查询、event 查询、分页组合过滤和图片引用查询。
- [ ] 在 report adapter 中实现车牌观测校验、时间转换、bbox JSON 序列化和幂等插入。
- [ ] 在 UDS ReportService 实现 `ReportPlateObservation` typed adapter 与稳定错误码。
- [ ] 新增 service/API：列表、详情、全景图和车牌裁剪图读取。
- [ ] 路由增加独立 RBAC 权限点；图片读取使用 catalog/配置根目录和路径规范化校验。
- [ ] 更新 Wire 装配并执行 `make wire`。
- [ ] 更新 Swagger 文档与 API 测试。

验证：

```bash
cd argus
make migrate-up
make migrate-version
make wire
make test
make vet
make build
```

重点测试：

- 重复 event ID 幂等 ACK。
- 软删除后唯一约束行为。
- 筛选组合与分页总数。
- 非法时间、bbox、车牌文本和图片路径。
- 无图片、图片不存在、路径逃逸和未授权访问。
- 服务重启后记录仍存在，MVP 不执行自动删除。

回滚点：down migration 仅用于未投产或明确数据备份后的回滚；禁止在已有业务记录时静默 drop table。

## 6. 阶段五：Vue 管理端

- [ ] 新增 `apps/web-antd/src/api/core/plate.ts`，定义列表、详情和查询类型。
- [ ] 新增车辆通行路由与菜单权限，不复用告警页面语义。
- [ ] 实现紧凑表格：时间、车牌小图、车牌号、颜色、类型、置信度、摄像头。
- [ ] 实现筛选：车牌、摄像头、时间范围、颜色、类型和最低置信度。
- [ ] 实现详情视图：全景图、裁剪图、识别字段、算法版本、time synced 状态。
- [ ] 处理加载、空列表、无裁剪图、图片失败、低置信度和无权限状态。
- [ ] 增加中文/英文/繁体 i18n key 与类型/组件单测。
- [ ] 在桌面和移动视口验证文本、图片和操作控件无重叠或溢出。

验证：

```bash
cd ui
pnpm check
pnpm test:unit
pnpm build
```

回滚点：前端路由和菜单可独立撤销，不影响已存储通行记录和后端 API。

## 7. 阶段六：端到端验收

- [ ] 安装并激活 macOS 车牌算法包，创建摄像头任务和算法实例。
- [ ] 使用固定视频验证“识别 -> 全景/裁剪落盘 -> IPC -> 数据库 -> Web 列表/详情”完整链路。
- [ ] 验证同一轨迹稳定融合和去重，不把逐帧 OCR 抖动写成多条记录。
- [ ] 验证 Engine/Go 任一侧重启、IPC 断开重连和重复 ACK 不产生重复记录。
- [ ] 验证数据和图片在重启后保留，系统没有 MVP 自动清理任务。
- [ ] 使用未授权账号验证列表、详情和图片均被拒绝。
- [ ] 记录已执行命令、结果、跳过项和原因。

最终质量门：

```bash
make -C engine test
make -C engine asan
make -C engine lint
bash algo-packages/scripts/check-consistency.sh
cd argus && make test && make vet && make build
cd ui && pnpm check && pnpm test:unit && pnpm build
```

## 8. 完成定义

只有满足以下条件才可完成任务：

- PRD 的 A-E 验收项均有代码、测试或明确的运行证据。
- 普通蓝牌、黄牌和新能源绿牌在固定代表性样本上有真实 Core ML 识别证据。
- 既有人脸识别和目标检测契约测试无回归。
- IPC 失败、重试和重复上报不会产生重复记录或泄漏帧引用。
- RBAC 和日志脱敏验证通过。
- 所有跳过的真实硬件/现场项有原因和后续验证入口，未被描述为已完成。
