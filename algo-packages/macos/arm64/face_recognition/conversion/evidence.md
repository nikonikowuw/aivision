# Model Conversion & Validation Evidence

## Source Weights

- `scrfd_10g_bnkps.onnx`: 16,923,827 bytes
- `glintr100.onnx`: 260,665,334 bytes
- Source location: `tentcoo/rknn_model_zoo/examples/insightface/antelopev2`

## Converted Core ML Models

Converted with `coremltools 8.2` (macOS 14 deployment target, float32 compute precision):

- `model/yolov8n.mlpackage`: Reused from macOS package
- `model/scrfd_10g_bnkps.mlpackage`:
  - Input: `input_1` [1, 3, 640, 640] (RGB, float32, normalized with `(x - 127.5) / 128.0`)
  - Outputs (9 heads):
    - `var_717` [12800, 1] (score stride 8)
    - `var_830` [3200, 1] (score stride 16)
    - `var_943` [800, 1] (score stride 32)
    - `var_731` [12800, 4] (bbox stride 8)
    - `var_844` [3200, 4] (bbox stride 16)
    - `var_957` [800, 4] (bbox stride 32)
    - `var_745` [12800, 10] (5 landmarks stride 8)
    - `var_858` [3200, 10] (5 landmarks stride 16)
    - `var_971` [800, 10] (5 landmarks stride 32)
  - Validation: Max absolute difference against ONNX Runtime < 3e-6 across all 9 heads.

- `model/glintr100.mlpackage`:
  - Input: `input_1` [1, 3, 112, 112] (RGB, float32, normalized with `(x - 127.5) / 127.5`)
  - Output: `var_2160` [1, 512] (float32)
  - Validation: Cosine similarity against ONNX Runtime embedding = 0.99999988.
