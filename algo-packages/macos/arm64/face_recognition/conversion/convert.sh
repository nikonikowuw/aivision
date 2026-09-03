#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PACKAGE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

PYTHON_BIN="${PYTHON_BIN:-python3}"

echo "[convert.sh] Converting models to Core ML .mlpackage format (640x384 surveillance pipeline)..."

"${PYTHON_BIN}" - "${PACKAGE_DIR}" << 'EOF'
import os
import sys
import torch
import onnx2torch
import coremltools as ct
import numpy as np
from ultralytics import YOLO

pkg_dir = os.path.abspath(sys.argv[1]) if len(sys.argv) > 1 else os.getcwd()
weights_dir = os.path.join(pkg_dir, "weights")
model_dir = os.path.join(pkg_dir, "model")

os.makedirs(model_dir, exist_ok=True)

# 0. YOLOv8n (384x640)
yolo_pt = os.path.join(weights_dir, "yolo26n.pt")
if not os.path.exists(yolo_pt):
    # fallback to yolo26n weights if available
    alt_pt = os.path.abspath(os.path.join(pkg_dir, "../weights/yolo26n.pt"))
    if os.path.exists(alt_pt):
        yolo_pt = alt_pt

if os.path.exists(yolo_pt):
    print("Converting YOLOv8n (384x640 FP16 for Neural Engine)...")
    yolo_model = YOLO(yolo_pt)
    yolo_exported = yolo_model.export(format="coreml", imgsz=[384, 640], nms=False, half=True)
    if os.path.exists(yolo_exported) and yolo_exported != os.path.join(model_dir, "yolov8n.mlpackage"):
        os.system(f"cp -r '{yolo_exported}' '{os.path.join(model_dir, 'yolov8n.mlpackage')}'")
    print("YOLOv8n converted.")

# 1. SCRFD 10G (384x640 for surveillance stream, 640x640 for static registration)
scrfd_onnx = os.path.join(weights_dir, "scrfd_10g_bnkps.onnx")
if not os.path.exists(scrfd_onnx):
    for candidate in [
        "weights/scrfd_10g_bnkps.onnx",
    ]:
        if os.path.exists(candidate):
            scrfd_onnx = candidate
            break

if os.path.exists(scrfd_onnx):
    print("Converting SCRFD 10G (384x640 FP16 for surveillance stream pipeline)...")
    scrfd_torch = onnx2torch.convert(scrfd_onnx).eval()
    dummy_scrfd_stream = torch.randn(1, 3, 384, 640)
    traced_scrfd_stream = torch.jit.trace(scrfd_torch, dummy_scrfd_stream)
    scrfd_mlmodel_stream = ct.convert(
        traced_scrfd_stream,
        inputs=[ct.TensorType(name='input_1', shape=(1, 3, 384, 640), dtype=np.float32)],
        compute_precision=ct.precision.FLOAT16,
        minimum_deployment_target=ct.target.macOS14
    )
    scrfd_mlmodel_stream.save(os.path.join(model_dir, "scrfd_10g_bnkps.mlpackage"))
    print("SCRFD 10G (384x640) converted.")

    print("Converting SCRFD 10G (640x640 FP16 for static face registration)...")
    dummy_scrfd_reg = torch.randn(1, 3, 640, 640)
    traced_scrfd_reg = torch.jit.trace(scrfd_torch, dummy_scrfd_reg)
    scrfd_mlmodel_reg = ct.convert(
        traced_scrfd_reg,
        inputs=[ct.TensorType(name='input_1', shape=(1, 3, 640, 640), dtype=np.float32)],
        compute_precision=ct.precision.FLOAT16,
        minimum_deployment_target=ct.target.macOS14
    )
    scrfd_mlmodel_reg.save(os.path.join(model_dir, "scrfd_10g_640x640.mlpackage"))
    print("SCRFD 10G (640x640) converted.")

# 2. GLINTR100 / AdaFace (112x112)
glintr_onnx = os.path.join(weights_dir, "glintr100.onnx")
adaface_onnx = os.path.join(weights_dir, "adaface_ir101.onnx")

rec_onnx = None
rec_out_name = "glintr100.mlpackage"
if os.path.exists(adaface_onnx):
    rec_onnx = adaface_onnx
    rec_out_name = "adaface_ir101.mlpackage"
elif os.path.exists(glintr_onnx):
    rec_onnx = glintr_onnx
    rec_out_name = "glintr100.mlpackage"

if rec_onnx:
    print(f"Converting Face Recognition Backbone ({rec_onnx} -> {rec_out_name}, 112x112 FP16 for Neural Engine)...")
    rec_torch = onnx2torch.convert(rec_onnx).eval()
    dummy_rec = torch.randn(1, 3, 112, 112)
    traced_rec = torch.jit.trace(rec_torch, dummy_rec)
    rec_mlmodel = ct.convert(
        traced_rec,
        inputs=[ct.TensorType(name='input_1', shape=(1, 3, 112, 112), dtype=np.float32)],
        compute_precision=ct.precision.FLOAT16,
        minimum_deployment_target=ct.target.macOS14
    )
    rec_mlmodel.save(os.path.join(model_dir, rec_out_name))
    print(f"{rec_out_name} converted.")
EOF

echo "[convert.sh] Done."
