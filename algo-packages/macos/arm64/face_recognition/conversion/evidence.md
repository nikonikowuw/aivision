# Model Conversion & Validation Evidence

## Source Weights

- `scrfd_10g_bnkps.onnx`: 16,923,827 bytes
- `glintr100.onnx`: 260,665,334 bytes
- `yolov8n.pt`: 6,233,485 bytes
- Source location: `tentcoo/rknn_model_zoo/examples/insightface/antelopev2` + `ultralytics/yolov8n`

## Converted Core ML Models (640x384 Surveillance Optimization)

Converted with `coremltools 9.0` (macOS 14 deployment target, float16 compute precision):

- `model/yolov8n.mlpackage`:
  - Input: `image` [1, 3, 384, 640] (RGB, float32, normalized with `x / 255.0`)
  - Output: `var_911` [1, 84, 5040] (float32)

- `model/scrfd_10g_bnkps.mlpackage`:
  - Input: `input_1` [1, 3, 384, 640] (RGB, float32, normalized with `(x - 127.5) / 128.0`)
  - Outputs (9 heads, anchor counts reduced by 40%):
    - `score_8` [7680, 1] (score stride 8, down from 12800)
    - `score_16` [1920, 1] (score stride 16, down from 3200)
    - `score_32` [480, 1] (score stride 32, down from 800)
    - `bbox_8` [7680, 4] (bbox stride 8, down from 12800)
    - `bbox_16` [1920, 4] (bbox stride 16, down from 3200)
    - `bbox_32` [480, 4] (bbox stride 32, down from 800)
    - `kps_8` [7680, 10] (5 landmarks stride 8, down from 12800)
    - `kps_16` [1920, 10] (5 landmarks stride 16, down from 3200)
    - `kps_32` [480, 10] (5 landmarks stride 32, down from 800)

- `model/glintr100.mlpackage`:
  - Input: `input_1` [1, 3, 112, 112] (RGB, float32, normalized with `(x - 127.5) / 127.5`)
  - Output: `var_2160` [1, 512] (float32)
