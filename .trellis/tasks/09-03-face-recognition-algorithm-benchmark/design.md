# 技术设计

## 范围

本子任务只分析当前工作区的 `algo-packages/macos/arm64/face_recognition`，允许增加 benchmark/profiling/test 资产，不改变生产推理逻辑、公共 ABI 或 Engine。

## 数据流与计时

```text
av_frame_desc
  -> preprocess
  -> SCRFD prediction/output copy
  -> decode/NMS/tracker/quality
  -> optional alignment
  -> optional GLINTR/embedding encode
  -> result serialization/callback
```

在算法包内部以 `steady_clock` 记录阶段耗时和计数；用 `os_signpost` 标记 preprocess、SCRFD、alignment、GLINTR。详细统计由编译开关控制，默认生产构建关闭。

## Benchmark 矩阵

- 当前 `testimage.jpg`：快速回归基线；
- 1080P/4K 固定内容 NV12：分辨率和内存带宽；
- synthetic NV12：0/1/4/16 人脸替代场景、stride 和边界；
- VideoToolbox CVPixelBuffer：真实平台 Surface 路径；
- 模式：detection-only、best-shot、all；
- 规模：30+300 开发基线，60+1000 稳定性。

## 内存与运行时观测

记录 RSS before/peak/after、CPU sample、模型加载耗时；使用 `/usr/bin/time -l`、`xctrace` 和 Instruments。大型 trace 只记录命令、环境、SHA-256 和摘要。

## 证据与回滚

每项结论标注 L1/L2/L3/L4。benchmark/profiling 变更可整体删除回到现有 runner；不触碰生产结果 schema 和 ABI。无法使用的工具或场景标记 BLOCKED。
