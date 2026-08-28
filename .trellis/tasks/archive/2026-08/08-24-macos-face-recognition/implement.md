# Implementation Plan: macOS Face Recognition

本计划只在 planning 审核通过并执行 `task.py start` 后实施。当前阶段不得修改产品代码或运行实现构建。

## Ordered Work

### 1. Reconcile the contracts first

- [ ] Read the active task artifacts and the engine ABI, manifest, directory, quality and cross-layer specs again before editing.
- [ ] Add `AV_RESULT_RECOGNITION = 3` without changing existing enum values or ABI struct layouts.
- [ ] Update manifest schema documentation and the executable SDK/Engine validators to conditionally require `alarm_type_id` only for `object_detection`.
- [ ] Update `library_query` metadata checks so `face_recognition` with an empty ABI alarm field is valid.
- [ ] Synchronize vendored SDK copies and add/update mock fixtures for both algorithm types.
- [ ] Add C/C++ ABI and manifest regression tests before relying on the new package.

Validation gate: existing Engine/SDK tests pass; old YOLOv8n manifest still validates; a minimal recognition manifest validates only after the new rule is present.

### 2. Create the self-contained package skeleton

- [ ] Copy the existing macOS YOLOv8n Core ML model into `face_recognition/model/`.
- [ ] Copy only `scrfd_10g_bnkps.onnx` and `glintr100.onnx` from the antelopev2 source directory into `weights/`; record source paths, file sizes and SHA-256.
- [ ] Add `manifest.json`, `.env`, `config.schema.json`, `testimage.jpg`, negative test asset, package CMake/Makefile and vendored SDK.
- [ ] Add package-root path resolution and three-level config precedence; ensure no `std::getenv`, CWD lookup, repository absolute path, or dependency on another package.
- [ ] Add package export-symbol controls and convention-based packaging.

Validation gate: package has no parent-repository include/model dependency; source/asset checks are reproducible from a clean copy.

### 3. Convert and verify models

- [ ] Write `conversion/convert.sh` and pinned Python requirements for `coremltools 9.0`.
- [ ] Convert SCRFD and GLINTR while preserving raw model outputs; record actual input/output names, shapes, channel order, normalization and model digests in `conversion/evidence.md`.
- [ ] Compare ONNX Runtime and Core ML outputs on fixed inputs, including SCRFD boxes/landmarks and GLINTR cosine similarity.
- [ ] Stop and revise the task design if the real model contract contradicts the confirmed 640x640 detection / 112x112 recognition flow; do not silently relabel a model or invent output shapes.

Validation gate: all three Core ML models load on Apple Silicon and model-contract checks are deterministic.

### 4. Implement preprocessing and inference

- [ ] Implement NV12/CVPixelBuffer access using the existing ABI/image operations and frame-declared color metadata.
- [ ] Produce one 640x640 letterbox buffer and retain exact scale/padding metadata.
- [ ] Implement YOLOv8n person-only decode and SCRFD 10G bnkps decode/NMS/landmark output with strict shape checks.
- [ ] Implement inverse letterbox mapping and original-frame coordinate validation.
- [ ] Implement five-point similarity sampling directly from original RGB into 112x112, then GLINTR inference, finite-value check, L2 normalization and little-endian Base64 encoding.

Validation gate: preprocess/geometry tests establish that a known point and landmark round-trip from original -> letterbox -> original within tolerance.

### 5. Implement state, association and result behavior

- [ ] Add ByteTrack state, track lifecycle, max-person limiting and per-instance track ID allocation.
- [ ] Implement center-in-person plus IoU association, one face per person, and unassociated-face discard.
- [ ] Implement strict frame ID ordering, flush reset and instance destroy cleanup.
- [ ] Implement conditional callbacks and all-embedding-failure semantics exactly as the PRD defines.
- [ ] Serialize schema version 1, normalized original coordinates, sorted persons and the embedding metadata contract.
- [ ] Implement atomic config updates and the six dynamic parameters with the confirmed precedence.

Validation gate: focused unit tests cover empty, partial, full-failure, multi-person, frame-order, flush and sorting cases without requiring Core ML.

### 6. Add package runner, self-test and evidence

- [ ] Make the runner create a real macOS NV12 `CVPixelBuffer` and `av_frame_desc`; it must not pass synthetic RGB memory to production `instance_process`.
- [ ] Implement install self-test through the real preprocess/inference/postprocess/serialize path and emit exactly one `AV_RESULT_SELF_TEST` result.
- [ ] Add dedicated multi-person/multi-face and no-valid-face fixtures with license/source/hash records.
- [ ] Add benchmark output for preprocess/inference/postprocess/end-to-end latency, P50/P99, FPS, model digest and memory.
- [ ] Add package zip and external `<archive>.zip.sha256` generation.

Validation gate: `make run`, package self-test and result decoding work on an actual Apple Silicon macOS environment.

### 7. Full quality gate

- [ ] Run `make -C engine configure`.
- [ ] Run `make -C engine build` and `make -C engine test`.
- [ ] Run `make -C engine asan`, `make -C engine tsan` and `make -C engine lint`.
- [ ] Run `bash algo-packages/scripts/check-consistency.sh`.
- [ ] Run the new package `make configure`, `make build`, `make test`, `make asan`, `make run`, `make benchmark` and `make package` on Apple Silicon.
- [ ] Run the package from a repository-external temporary directory and inspect exported symbols and linked libraries.
- [ ] Run Engine/package contract tests, including old object-detection fixtures and new recognition self-test.
- [ ] Record passed commands, skipped commands, hardware/OS/model versions, metrics, hashes and known residual risks in the task evidence.

## Risky Files and Rollback Points

- `sdk/include/aivision/result.h`: ABI enum extension; rollback before release if any existing numeric value or static assertion changes.
- `sdk/cmake/validate_package.cmake` and `engine/src/core/algo/algo_sandbox.cpp`: conditional manifest semantics; rollback if any existing object-detection fixture becomes less strict.
- Vendored SDK copies: consistency risk; regenerate/synchronize rather than hand-edit one copy only.
- `algo-packages/macos/arm64/face_recognition/src/inference/`: model output names/shapes and Core ML concurrency are runtime risks; retain conversion evidence and fail closed on mismatch.
- `algo-packages/macos/arm64/face_recognition/src/preprocess/`: coordinate and color conversion risk; use fixed image/geometry fixtures before model accuracy checks.
- Result callback/serialization code: compatibility risk; test callback counts and `AV_MAX_RESULT_JSON_BYTES` before integration.

## Pre-start Review Checklist

- [ ] User has approved the final planning summary after these artifacts are written.
- [ ] `prd.md`, `design.md` and `implement.md` contain no unresolved product or scope decision.
- [ ] All model source/license and actual Core ML shape questions are explicitly marked as implementation verification, not silently assumed.
- [ ] No product code has been edited during planning.
- [ ] Only after approval: run `python3 ./.trellis/scripts/task.py start 08-24-macos-face-recognition` and enter implementation.
