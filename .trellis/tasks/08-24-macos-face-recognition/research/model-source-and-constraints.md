# Research: antelopev2 model source and current repository constraints

## Local model inventory

Source directory:

```text
/home/nikoniko/work/tentcoo/rknn_model_zoo/examples/insightface/antelopev2
```

Files found during planning:

```text
genderage.onnx
2d106det.onnx
1k3d68.onnx
glintr100.onnx
scrfd_10g_bnkps.onnx
```

The confirmed MVP uses only `scrfd_10g_bnkps.onnx` and `glintr100.onnx`. The other three files are not part of the recognition pipeline.

## Existing macOS package evidence

The existing package at `algo-packages/macos/arm64/yolov8n` demonstrates:

- Apple Silicon macOS / Core ML target with minimum macOS 14.0;
- Core ML model loading in Objective-C++;
- NV12/CVPixelBuffer frame handling and an actual package runner;
- package-root `.env`/model path conventions and Core ML conversion evidence;
- vendored SDK and package CMake structure.

The new package must reuse these repository patterns while adding separate SCRFD/GLINTR runners and recognition-specific result handling.

## Current blockers exposed by repository inspection

The current implementation is object-detection-only in the following areas:

- `sdk/include/aivision/result.h` defines only `AV_RESULT_ALARM` and `AV_RESULT_SELF_TEST`.
- `sdk/cmake/validate_package.cmake` requires `alarm_type_id` and accepts only `object_detection`.
- `engine/src/core/algo/algo_sandbox.cpp` requires `alarm_type_id`, requires legacy manifest file fields, and rejects non-object-detection types.
- `.trellis/spec/engine/manifest-schema.md` records `face_recognition` as reserved and not implemented.

These are contract work items, not reasons to bypass validators in the new package.

## Verification still required during implementation

- Confirm the actual ONNX input/output names, shapes and tensor layouts with ONNX tooling.
- Confirm `coremltools 9.0` conversion works for both models on the target Apple Silicon environment.
- Confirm model redistribution/license terms before packaging binaries or weights.
- Compare ONNX and Core ML outputs on fixed inputs and record hashes/metrics.
