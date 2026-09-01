#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PACKAGE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

PYTHON_BIN="${PYTHON_BIN:-/Users/zhang/.venv/bin/python3}"

echo "[convert.sh] Converting ONNX models to Core ML .mlpackage format (640x384 detection & PP-OCRv4 recognition)..."

"${PYTHON_BIN}" - << 'EOF'
import os
import sys
import urllib.request
import onnx
from onnx import numpy_helper
import numpy as np
import torch
import onnx2torch
import coremltools as ct

script_dir = os.path.dirname(os.path.abspath(__file__)) if '__file__' in locals() else os.getcwd()
pkg_dir = os.path.abspath(os.path.join(script_dir, ".."))
weights_dir = os.path.join(pkg_dir, "weights")
model_dir = os.path.join(pkg_dir, "model")

os.makedirs(weights_dir, exist_ok=True)
os.makedirs(model_dir, exist_ok=True)

# 1. Plate Detector (Adapted to 640x384 for standard 16:9 IPC camera streams)
detect_onnx = os.path.join(weights_dir, "plate_detect.onnx")
detect_384_onnx = os.path.join(weights_dir, "plate_detect_384x640.onnx")

if os.path.exists(detect_onnx):
    print("Adapting and converting plate_detect.onnx to 640x384 (FP16 Core ML)...")
    model = onnx.load(detect_onnx)
    model.graph.input[0].type.tensor_type.shape.dim[2].dim_value = 384
    model.graph.input[0].type.tensor_type.shape.dim[3].dim_value = 640
    model.graph.output[0].type.tensor_type.shape.dim[1].dim_value = 15120

    def make_grid(ny, nx):
        yv, xv = np.meshgrid(np.arange(ny), np.arange(nx), indexing='ij')
        grid = np.stack((xv, yv), axis=-1).reshape(1, 1, ny, nx, 2).astype(np.float32)
        return np.repeat(grid, 3, axis=1)

    def make_anchors(ny, nx, anchors):
        a = np.array(anchors, dtype=np.float32).reshape(1, 3, 1, 1, 2)
        return np.tile(a, (1, 1, ny, nx, 1))

    anchors_lvl0 = [[4.0, 5.0], [8.0, 10.0], [13.0, 16.0]]
    anchors_lvl1 = [[23.0, 29.0], [43.0, 55.0], [73.0, 105.0]]
    anchors_lvl2 = [[146.0, 217.0], [231.0, 300.0], [335.0, 433.0]]

    grid0 = make_grid(48, 80)
    grid0_s = grid0 * 8.0
    anchors0 = make_anchors(48, 80, anchors_lvl0)
    zeros0 = np.zeros((1, 3, 48, 80, 15), dtype=np.float32)

    grid1 = make_grid(24, 40)
    grid1_s = grid1 * 16.0
    anchors1 = make_anchors(24, 40, anchors_lvl1)
    zeros1 = np.zeros((1, 3, 24, 40, 15), dtype=np.float32)

    grid2 = make_grid(12, 20)
    grid2_s = grid2 * 32.0
    anchors2 = make_anchors(12, 20, anchors_lvl2)
    zeros2 = np.zeros((1, 3, 12, 20, 15), dtype=np.float32)

    value_map = {
        '/model.21/Constant': np.array([1, 3, 15, 48, 80], dtype=np.int64),
        '/model.21/Constant_13': zeros0,
        '/model.21/Constant_20': grid0,
        '/model.21/Constant_28': anchors0,
        '/model.21/Constant_33': anchors0,
        '/model.21/Constant_34': grid0_s,
        '/model.21/Constant_39': anchors0,
        '/model.21/Constant_40': grid0_s,
        '/model.21/Constant_45': anchors0,
        '/model.21/Constant_46': grid0_s,
        '/model.21/Constant_51': anchors0,
        '/model.21/Constant_52': grid0_s,
        '/model.21/Constant_62': np.array([1, 3, 15, 24, 40], dtype=np.int64),
        '/model.21/Constant_75': zeros1,
        '/model.21/Constant_82': grid1,
        '/model.21/Constant_90': anchors1,
        '/model.21/Constant_95': anchors1,
        '/model.21/Constant_96': grid1_s,
        '/model.21/Constant_101': anchors1,
        '/model.21/Constant_102': grid1_s,
        '/model.21/Constant_107': anchors1,
        '/model.21/Constant_108': grid1_s,
        '/model.21/Constant_113': anchors1,
        '/model.21/Constant_114': grid1_s,
        '/model.21/Constant_124': np.array([1, 3, 15, 12, 20], dtype=np.int64),
        '/model.21/Constant_137': zeros2,
        '/model.21/Constant_144': grid2,
        '/model.21/Constant_152': anchors2,
        '/model.21/Constant_157': anchors2,
        '/model.21/Constant_158': grid2_s,
        '/model.21/Constant_163': anchors2,
        '/model.21/Constant_164': grid2_s,
        '/model.21/Constant_169': anchors2,
        '/model.21/Constant_170': grid2_s,
        '/model.21/Constant_175': anchors2,
        '/model.21/Constant_176': grid2_s,
    }

    for node in model.graph.node:
        if node.name in value_map:
            val = value_map[node.name]
            for attr in node.attribute:
                if attr.name == 'value':
                    attr.t.CopyFrom(numpy_helper.from_array(val))
        elif node.op_type == 'Constant':
            for attr in node.attribute:
                if attr.name == 'value':
                    t = numpy_helper.to_array(attr.t)
                    if t.shape == (5,) and list(t[-2:]) == [80, 80]:
                        new_t = np.array([t[0], t[1], t[2], 48, 80], dtype=t.dtype)
                        attr.t.CopyFrom(numpy_helper.from_array(new_t))
                    elif t.shape == (4,) and list(t[-2:]) == [80, 80]:
                        new_t = np.array([t[0], t[1], 48, 80], dtype=t.dtype)
                        attr.t.CopyFrom(numpy_helper.from_array(new_t))
                    elif t.shape == (5,) and list(t[-2:]) == [40, 40]:
                        new_t = np.array([t[0], t[1], t[2], 24, 40], dtype=t.dtype)
                        attr.t.CopyFrom(numpy_helper.from_array(new_t))
                    elif t.shape == (4,) and list(t[-2:]) == [40, 40]:
                        new_t = np.array([t[0], t[1], 24, 40], dtype=t.dtype)
                        attr.t.CopyFrom(numpy_helper.from_array(new_t))
                    elif t.shape == (5,) and list(t[-2:]) == [20, 20]:
                        new_t = np.array([t[0], t[1], t[2], 12, 20], dtype=t.dtype)
                        attr.t.CopyFrom(numpy_helper.from_array(new_t))
                    elif t.shape == (4,) and list(t[-2:]) == [20, 20]:
                        new_t = np.array([t[0], t[1], 12, 20], dtype=t.dtype)
                        attr.t.CopyFrom(numpy_helper.from_array(new_t))

    onnx.save(model, detect_384_onnx)
    detect_torch = onnx2torch.convert(detect_384_onnx).eval()
    dummy_detect = torch.randn(1, 3, 384, 640)
    traced_detect = torch.jit.trace(detect_torch, dummy_detect)
    detect_mlmodel = ct.convert(
        traced_detect,
        inputs=[ct.TensorType(name='input', shape=(1, 3, 384, 640), dtype=np.float32)],
        compute_precision=ct.precision.FLOAT16,
        minimum_deployment_target=ct.target.macOS14
    )
    detect_mlmodel.save(os.path.join(model_dir, "plate_detect.mlpackage"))
    print("Plate Detector (640x384) converted.")

# 2. Multilingual License Plate Recognizer (PP-OCRv4-mobile)
rec_onnx = os.path.join(weights_dir, "ch_PP-OCRv4_rec_infer.onnx")
fixed_rec_onnx = os.path.join(weights_dir, "ppocr_fixed.onnx")
dict_file = os.path.join(weights_dir, "ppocr_keys_v1.txt")

if not os.path.exists(dict_file):
    print("Downloading PP-OCRv4 character dictionary...")
    urllib.request.urlretrieve(
        "https://raw.githubusercontent.com/PaddlePaddle/PaddleOCR/release/2.7/ppocr/utils/ppocr_keys_v1.txt",
        dict_file
    )

if os.path.exists(rec_onnx):
    print("Converting PP-OCRv4 recognition ONNX to Core ML (320x48 input, FP16)...")
    m = onnx.load(rec_onnx)
    m.graph.input[0].type.tensor_type.shape.dim[0].dim_value = 1
    m.graph.input[0].type.tensor_type.shape.dim[1].dim_value = 3
    m.graph.input[0].type.tensor_type.shape.dim[2].dim_value = 48
    m.graph.input[0].type.tensor_type.shape.dim[3].dim_value = 320

    m.graph.output[0].type.tensor_type.shape.dim[0].dim_value = 1
    m.graph.output[0].type.tensor_type.shape.dim[1].dim_value = 40
    m.graph.output[0].type.tensor_type.shape.dim[2].dim_value = 6625

    init_names = {i.name for i in m.graph.initializer}
    nodes_to_remove = []
    for node in m.graph.node:
        if node.op_type == "Shape":
            nodes_to_remove.append(node)

    for node in nodes_to_remove:
        m.graph.node.remove(node)

    if not any(i.name == "Constant_401" for i in m.graph.initializer):
        m.graph.initializer.append(numpy_helper.from_array(np.array([1, 40, 120], dtype=np.int64), name="Constant_401"))

    onnx.save(m, fixed_rec_onnx)

    torch_model = onnx2torch.convert(fixed_rec_onnx).eval()
    dummy = torch.randn(1, 3, 48, 320)
    traced = torch.jit.trace(torch_model, dummy)

    mlmodel = ct.convert(
        traced,
        inputs=[ct.TensorType(name="x", shape=(1, 3, 48, 320), dtype=np.float32)],
        compute_precision=ct.precision.FLOAT16,
        minimum_deployment_target=ct.target.macOS14
    )
    mlmodel.save(os.path.join(model_dir, "plate_rec_ppocr.mlpackage"))
    print("PP-OCRv4 Recognizer converted successfully.")
EOF

echo "[convert.sh] Done."
