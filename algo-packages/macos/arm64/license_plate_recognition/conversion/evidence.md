# License Plate Recognition Model Conversion & Validation Evidence

## Source Weights & Architectures

- `plate_detect.onnx`: 5,418,022 bytes
  - Architecture: YOLOv5-plate with 4 corner landmarks
  - Input: `input` [1, 3, 384, 640] (RGB, float32, normalized with `x / 255.0`, optimized for 16:9 surveillance camera aspect ratio)
  - Output: `var_3578` [1, 15120, 15] (bbox 4 + score 1 + landmarks 8 + classes 2)
  - Anchor Levels:
    - Stride 8 (P3): 48x80 grid, 3 anchors [[4, 5], [8, 10], [13, 16]]
    - Stride 16 (P4): 24x40 grid, 3 anchors [[23, 29], [43, 55], [73, 105]]
    - Stride 32 (P5): 12x20 grid, 3 anchors [[146, 217], [231, 300], [335, 433]]
  - Source location: `zhang-jiaqi-1207/plate-detect-pt2rk` & `we0091234/Chinese_license_plate_detection_recognition`

- `plate_rec_color.onnx`: 788,330 bytes
  - Architecture: CRNN plate text OCR and 5-color classifier (`myNet_ocr_color`)
  - Color Space: **OpenCV BGR Channel Order** (B, G, R layout; NOT RGB)
  - Input: `input` [1, 3, 48, 168] (BGR, float32, normalized with `(x/255.0 - 0.588) / 0.193`)
  - Outputs:
    - `var_234` [1, 21, 78] (CTC character sequence logits across 78 Chinese/alphanumeric labels)
    - `var_199` [1, 5] (Plate color classification logits: `[0: black, 1: blue, 2: green, 3: white, 4: yellow]`)

## Critical Channel & Aspect Ratio Specifications

1. **Detection Resolution (640 × 384)**:
   - Modern IPC cameras use 16:9 streams (1080P/2K/4K).
   - Standard 640x640 letterbox wastes 43.75% of compute on top/bottom padding.
   - 640x384 reduces candidate boxes from 25,200 to 15,120 (40% FLOPs reduction).
2. **Color Space Alignment ($R \leftrightarrow B$)**:
   - `plate_detect` operates on **RGB**.
   - `plate_rec_color` was trained on **BGR**.
   - `ModelInferenceManager::run_rec` maps `src_c = 2 - c` before feeding the CRNN model to ensure correct character recognition and color classification (`blue`, `yellow`, `green`, `white`, `black`).

## Converted Core ML Models

Converted with `coremltools 9.0` and `onnx2torch 1.5.15` targeting `macOS 14` with float16 compute precision for Apple Neural Engine / GPU acceleration:

- `model/plate_detect.mlpackage`:
  - Input: `input` [1, 3, 384, 640] (float32)
  - Output: `var_3578` [1, 15120, 15] (float16)
- `model/plate_rec_color.mlpackage`:
  - Input: `input` [1, 3, 48, 168] (float32, BGR layout)
  - Output 1: `var_234` [1, 21, 78] (float16)
  - Output 2: `var_199` [1, 5] (float16)
