# YOLOv8n Core ML Conversion Evidence

This file is source-only evidence. It is deliberately excluded from the distribution archive by the package target.

## Source and converter

- Source model: Ultralytics YOLOv8n, Ultralytics package `8.4.60`, COCO 80-class head.
- Converter: `coremltools 9.0`.
- Conversion input: `image`, RGB, shape `[1, 3, 640, 640]`, float32 values normalized to `[0, 1]`.
- Conversion output: `var_911`, shape `[1, 84, 8400]`, float32 (`4 box channels + 80 class channels`).
- Image preprocessing: NV12 -> BGRA using frame-declared BT.709 matrix and declared full/limited range, letterbox to `640x640`, padding value `114`.
- Postprocessing: confidence threshold, class-wise NMS, inverse letterbox mapping, normalized `[x, y, w, h]` boxes.

## Runtime configuration

- Compute units: `MLComputeUnitsAll`.
- Core ML input feature: image feature discovered from `MLModelDescription` (expected name `image`).
- Core ML output feature: multi-array discovered from `MLModelDescription` (expected name `var_911`).
- Supported output element types: Float32, Float16, Double.
- Runtime output shape assertion: exactly `[1, 84, 8400]`.
- Minimum macOS runtime declared by the manifest: `14.0`.

## Model checksums

- Core ML entry model `model/yolov8n.mlpackage/Data/com.apple.CoreML/model.mlmodel`:
  `e43e5e204266e47013500a870444fd992b1f5150f6f0f6e6c5066c47f056cd25`
- Deterministic package digest (SHA-256 over sorted `path + file SHA-256` records for every file under the `.mlpackage` directory):
  `992581a2089d25d5c6157e60bc163abba98a69260b473b2f638c06b9005ad275`

The source manifest uses `@LIBRARY_SHA256@` as a build-time placeholder. The packaging helper replaces it with the actual dylib digest before writing the distribution manifest; the distributed package never contains the placeholder.

## Build and verification

- Host: Apple Silicon macOS arm64.
- Toolchain: AppleClang 17.0.0.17000013, CMake 4.4.0, Ninja.
- Package build flags: `-Wall -Wextra -Wpedantic -Werror`, Objective-C ARC enabled.
- Verified commands:
  - `make build`
  - `make run` using a real NV12 `CVPixelBuffer` frame descriptor
  - `make benchmark` with warmup and Avg/P50/P99/FPS output
  - `make asan`
  - `make package`
  - `engine/build/package_validator <package_dir>`
  - `engine/build/package_validator <package_zip>`

The recorded model digest is an artifact identity check, not an accuracy claim. Detection accuracy is outside this task's acceptance scope.
