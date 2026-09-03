# Model Conversion & Validation Evidence

## Source Weights

- `scrfd_10g_bnkps.onnx`: 16,923,827 bytes
- `glintr100.onnx`: 260,665,334 bytes
- `yolov8n.pt`: 6,233,485 bytes
- Source location: `tentcoo/rknn_model_zoo/examples/insightface/antelopev2` + `ultralytics/yolov8n`

## Converted Core ML Models (640x384 Surveillance Optimization & ImageType Hardware Preprocessing)

Converted with `coremltools` (macOS 14 deployment target, float16 compute precision, native `ImageType` input):

- `model/yolov8n.mlpackage` / `model/yolo26n.mlpackage`:
  - Input: `images` [1, 3, 384, 640] (`ImageType` ColorSpace RGB, hardware normalized with `scale = 1/255.0, bias = 0`)
  - Output: `var_1801` [1, 300, 6] or `var_911` (float16 / float32)

- `model/scrfd_10g_bnkps.mlpackage` (Surveillance Streaming Pipeline):
  - Input: `input_1` [1, 3, 384, 640] (`ImageType` ColorSpace RGB, hardware normalized with `scale = 1/128.0, bias = [-127.5/128.0, -127.5/128.0, -127.5/128.0]`)
  - Outputs (9 heads, anchor counts reduced by 40% for 16:9 surveillance):
    - `score_8` [7680, 1] (score stride 8, down from 12800)
    - `score_16` [1920, 1] (score stride 16, down from 3200)
    - `score_32` [480, 1] (score stride 32, down from 800)
    - `bbox_8` [7680, 4] (bbox stride 8, down from 12800)
    - `bbox_16` [1920, 4] (bbox stride 16, down from 3200)
    - `bbox_32` [480, 4] (bbox stride 32, down from 800)
    - `kps_8` [7680, 10] (5 landmarks stride 8, down from 12800)
    - `kps_16` [1920, 10] (5 landmarks stride 16, down from 3200)
    - `kps_32` [480, 10] (5 landmarks stride 32, down from 800)

- `model/scrfd_10g_640x640.mlpackage` (Static Face Registration Pipeline):
  - Input: `input_1` [1, 3, 640, 640] (`ImageType` ColorSpace RGB, hardware normalized with `scale = 1/128.0, bias = [-127.5/128.0, -127.5/128.0, -127.5/128.0]`)
  - Outputs (9 heads, 1:1 square ratio for ID photos, selfies, and passports):
    - `score_8` [12800, 1]
    - `score_16` [3200, 1]
    - `score_32` [800, 1]
    - `bbox_8` [12800, 4]
    - `bbox_16` [3200, 4]
    - `bbox_32` [800, 4]
    - `kps_8` [12800, 10]
    - `kps_16` [3200, 10]
    - `kps_32` [800, 10]

- `model/glintr100.mlpackage`:
  - Input: `input_1` [1, 3, 112, 112] (`ImageType` ColorSpace RGB, hardware normalized with `scale = 1/127.5, bias = [-1.0, -1.0, -1.0]`)
  - Output: `var_2160` [1, 512] (float32)

- `model/adaface_ir101.mlpackage`:
  - Input: `input_1` [1, 3, 112, 112] (`ImageType` ColorSpace RGB, hardware normalized with `scale = 1/127.5, bias = [-1.0, -1.0, -1.0]`)
  - Output: `var_2547` [1, 512] (float32, AdaFace IR-101 WebFace12M Backbone, FP16 for ANE)
