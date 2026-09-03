# 执行计划：face_recognition 跨层分析与验证

## 1. 静态代码审计与数据契约对齐

- [x] 核对算法包 `postprocessor.cpp` 输出 JSON 键名与 `uds_server.cpp` `handle_face_recognition_result` 白名单字段（`schema_version`, `frame_id`, `pts_ns`, `algorithm_type`, `persons`, `bbox`, `confidence`, `quality_score`, `landmarks`, `embedding`）全部一致（PASS）。
- [x] 核对 Embedding 结构体（`model`, `dimension=512`, `dtype=float32`, `normalized=true`, `encoding=base64`, `byte_order=little_endian`, `data`）在 C++ 与 Go 之间的对应关系（PASS）。
- [x] 核对人脸框几何表示转换（算法包 `[x, y, w, h]` -> Engine 内部 `[xmin, ymin, xmax, ymax]` -> Protobuf `FaceBBox` -> Go `BBoxJSON`）对齐（PASS）。
- [x] 审计 `FramePool`、`frame_ops->retain` 与 `frame_ops->release` 在正常路径、队列满丢弃路径、异常抛出路径下的配对情况（PASS，无内存泄漏）。
- [x] 审计 `FaceGallery` 读写并发与换库生命周期（PASS，RCU 零锁原子替换）。

## 2. 已有测试集回归执行

- [x] 执行算法包核心测试：
  ```bash
  make -C algo-packages/macos/arm64/face_recognition test
  ```
  （3/3 测试通过）
- [x] 执行 Engine 核心测试（包含 C ABI、底库、UDS 协议）：
  ```bash
  make -C engine test
  ```
  （101/101 测试通过，覆盖 FaceGallery 与 UDS 人脸全流程）
- [x] 执行 Go 后端人脸抓拍与识别相关单元测试：
  ```bash
  cd argus && go test -v -run "TestReportAdapter_AcceptFace" ./internal/service/...
  ```
  （全部通过，覆盖单调 Upsert 与多快照增量追加）

## 3. 跨层契约一致性测试与断言验证

- [x] 验证算法包真实输出在 Engine `handle_face_recognition_result` 解析校验器中的通过性（PASS）。
- [x] 验证 512 维向量 Base64 解码、精度一致性（单精度浮点）与模长验证范围（[0.98, 1.02]）（PASS）。
- [x] 验证 `event_id` 生成规则 `${run_id}/${track_id}` 在 Go 后端的幂等与单调更新语义（PASS）。
- [x] 验证异常输入场景下（畸形 Base64、超范围坐标、NaN/Inf、负数 track_id）Engine 的拒绝行为与资源安全（PASS）。

## 4. 报告交付与检查

- [x] 编写子任务交付报告 `analysis.md`，包含四层流转图、字段矩阵、生命周期分析与测试验证结果。
- [x] 每条结论标注 L1/L2/L3/L4 证据分级，验证结果标记为 PASS / FAIL / BLOCKED。
- [x] 运行 `engine/scripts/check-boundary.sh` 确保无边界与符号破坏（PASS）。
- [x] 更新 `implement.md` 完成项并提交评审。

## 风险与回滚

- 本任务为只读审计与契约验证任务，不引入架构或接口变更。
- 若为测试需要添加的轻量级单测，在验证完成后可保留或通过 git 恢复。
- 严禁修改公共 C ABI 头文件及 Go 数据库模型。
