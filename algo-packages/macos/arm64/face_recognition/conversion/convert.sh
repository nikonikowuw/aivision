#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PACKAGE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

PYTHON_BIN="${PYTHON_BIN:-python3}"

echo "[convert.sh] Converting models to Core ML .mlpackage format (640x384 surveillance pipeline)..."

"${PYTHON_BIN}" - << 'EOF'
import os
import sys
import torch
import onnx2torch
import coremltools as ct
import numpy as np
from ultralytics import YOLO

script_dir = os.path.dirname(os.path.abspath(__file__)) if '__file__' in locals() else os.getcwd()
pkg_dir = os.path.abspath(os.path.join(script_dir, ".."))
weights_dir = os.path.join(pkg_dir, "weights")
model_dir = os.path.join(pkg_dir, "model")

os.makedirs(model_dir, exist_ok=True)

# 0. YOLOv8n (384x640)
yolo_pt = os.path.join(weights_dir, "yolov8n.pt")
if not os.path.exists(yolo_pt):
    # fallback to yolo26n weights if available
    alt_pt = os.path.abspath(os.path.join(pkg_dir, "../yolo26n/weights/yolov8n.pt"))
    if os.path.exists(alt_pt):
        yolo_pt = alt_pt

if os.path.exists(yolo_pt):
    print("Converting YOLOv8n (384x640 FP16 for Neural Engine)...")
    yolo_model = YOLO(yolo_pt)
    yolo_exported = yolo_model.export(format="coreml", imgsz=[384, 640], nms=False, half=True)
    if os.path.exists(yolo_exported) and yolo_exported != os.path.join(model_dir, "yolov8n.mlpackage"):
        os.system(f"cp -r '{yolo_exported}' '{os.path.join(model_dir, 'yolov8n.mlpackage')}'")
    print("YOLOv8n converted.")

# 1. SCRFD 10G (384x640)
scrfd_onnx = os.path.join(weights_dir, "scrfd_10g_bnkps.onnx")
if not os.path.exists(scrfd_onnx) and os.path.exists("/Users/zhang/dev/go/antelopev2/scrfd_10g_bnkps.onnx"):
    scrfd_onnx = "/Users/zhang/dev/go/antelopev2/scrfd_10g_bnkps.onnx"

if os.path.exists(scrfd_onnx):
    print("Converting SCRFD 10G (384x640 FP16 compute precision for Neural Engine acceleration)...")
    scrfd_torch = onnx2torch.convert(scrfd_onnx).eval()
    dummy_scrfd = torch.randn(1, 3, 384, 640)
    traced_scrfd = torch.jit.trace(scrfd_torch, dummy_scrfd)
    scrfd_mlmodel = ct.convert(
        traced_scrfd,
        inputs=[ct.TensorType(name='input_1', shape=(1, 3, 384, 640), dtype=np.float32)],
        compute_precision=ct.precision.FLOAT16,
        minimum_deployment_target=ct.target.macOS14
    )
    scrfd_mlmodel.save(os.path.join(model_dir, "scrfd_10g_bnkps.mlpackage"))
    print("SCRFD 10G converted.")

# 2. GLINTR100 (112x112)
glintr_onnx = os.path.join(weights_dir, "glintr100.onnx")
if not os.path.exists(glintr_onnx) and os.path.exists("/Users/zhang/dev/go/antelopev2/glintr100.onnx"):
    glintr_onnx = "/Users/zhang/dev/go/antelopev2/glintr100.onnx"

if os.path.exists(glintr_onnx):
    print("Converting GLINTR100 (112x112 FP16 compute precision for Neural Engine acceleration)...")
    glintr_torch = onnx2torch.convert(glintr_onnx).eval()
    dummy_glintr = torch.randn(1, 3, 112, 112)
    traced_glintr = torch.jit.trace(glintr_torch, dummy_glintr)
    glintr_mlmodel = ct.convert(
        traced_glintr,
        inputs=[ct.TensorType(name='input_1', shape=(1, 3, 112, 112), dtype=np.float32)],
        compute_precision=ct.precision.FLOAT16,
        minimum_deployment_target=ct.target.macOS14
    )
    glintr_mlmodel.save(os.path.join(model_dir, "glintr100.mlpackage"))
    print("GLINTR100 converted.")
EOF

echo "[convert.sh] Done."
