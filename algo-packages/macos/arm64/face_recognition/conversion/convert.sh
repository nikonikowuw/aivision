#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PACKAGE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

if [ -f "/Users/zhang/.venv/bin/python3" ]; then
    PYTHON_BIN="/Users/zhang/.venv/bin/python3"
else
    PYTHON_BIN="${PYTHON_BIN:-python3}"
fi

echo "[convert.sh] Converting models to Core ML .mlpackage format with ImageType input (640x384 surveillance pipeline)..."

"${PYTHON_BIN}" - "${PACKAGE_DIR}" << 'EOF'
import os
import sys
import torch
import onnx2torch
import coremltools as ct
import numpy as np

pkg_dir = os.path.abspath(sys.argv[1]) if len(sys.argv) > 1 else os.getcwd()
weights_dir = os.path.join(pkg_dir, "weights")
model_dir = os.path.join(pkg_dir, "model")

os.makedirs(model_dir, exist_ok=True)

# 0. YOLOv8n / YOLO26n (384x640)
# yolo26n.mlpackage is already exported with coremltools 9.0 with ImageType input
yolo_pt = os.path.join(weights_dir, "yolo26n.pt")
yolo_pkg = os.path.join(model_dir, "yolo26n.mlpackage")
yolov8_pkg = os.path.join(model_dir, "yolov8n.mlpackage")
if os.path.exists(yolo_pkg) and not os.path.exists(yolov8_pkg):
    import shutil
    shutil.copytree(yolo_pkg, yolov8_pkg)
    print("Copied yolo26n.mlpackage to yolov8n.mlpackage")

# 1. SCRFD 10G (384x640 for surveillance stream, 640x640 for static registration)
scrfd_onnx = os.path.join(weights_dir, "scrfd_10g_bnkps.onnx")
if os.path.exists(scrfd_onnx):
    print("Converting SCRFD 10G (384x640 FP16 ImageType for Neural Engine)...")
    scrfd_torch = onnx2torch.convert(scrfd_onnx).eval()

    dummy_scrfd_stream = torch.randn(1, 3, 384, 640)
    traced_scrfd_stream = torch.jit.trace(scrfd_torch, dummy_scrfd_stream)
    scrfd_image_stream = ct.ImageType(
        name='input_1',
        shape=(1, 3, 384, 640),
        scale=1.0 / 128.0,
        bias=[-127.5 / 128.0, -127.5 / 128.0, -127.5 / 128.0],
        color_layout=ct.colorlayout.RGB
    )
    scrfd_mlmodel_stream = ct.convert(
        traced_scrfd_stream,
        inputs=[scrfd_image_stream],
        compute_precision=ct.precision.FLOAT16,
        minimum_deployment_target=ct.target.macOS14
    )
    out_stream_path = os.path.join(model_dir, "scrfd_10g_bnkps.mlpackage")
    if os.path.exists(out_stream_path):
        import shutil
        shutil.rmtree(out_stream_path)
    scrfd_mlmodel_stream.save(out_stream_path)
    print("SCRFD 10G (384x640 ImageType) converted successfully.")

    print("Converting SCRFD 10G (640x640 FP16 ImageType for static face registration)...")
    dummy_scrfd_reg = torch.randn(1, 3, 640, 640)
    traced_scrfd_reg = torch.jit.trace(scrfd_torch, dummy_scrfd_reg)
    scrfd_image_reg = ct.ImageType(
        name='input_1',
        shape=(1, 3, 640, 640),
        scale=1.0 / 128.0,
        bias=[-127.5 / 128.0, -127.5 / 128.0, -127.5 / 128.0],
        color_layout=ct.colorlayout.RGB
    )
    scrfd_mlmodel_reg = ct.convert(
        traced_scrfd_reg,
        inputs=[scrfd_image_reg],
        compute_precision=ct.precision.FLOAT16,
        minimum_deployment_target=ct.target.macOS14
    )
    out_reg_path = os.path.join(model_dir, "scrfd_10g_640x640.mlpackage")
    if os.path.exists(out_reg_path):
        import shutil
        shutil.rmtree(out_reg_path)
    scrfd_mlmodel_reg.save(out_reg_path)
    print("SCRFD 10G (640x640 ImageType) converted successfully.")

# 2. GLINTR100 & AdaFace (112x112)
adaface_onnx = os.path.join(weights_dir, "adaface_ir101.onnx")
if os.path.exists(adaface_onnx):
    print("Converting AdaFace IR101 (112x112 FP16 ImageType for Neural Engine)...")
    ada_torch = onnx2torch.convert(adaface_onnx).eval()
    dummy_ada = torch.randn(1, 3, 112, 112)
    traced_ada = torch.jit.trace(ada_torch, dummy_ada)
    ada_image_input = ct.ImageType(
        name='input_1',
        shape=(1, 3, 112, 112),
        scale=1.0 / 127.5,
        bias=[-1.0, -1.0, -1.0],
        color_layout=ct.colorlayout.RGB
    )
    ada_mlmodel = ct.convert(
        traced_ada,
        inputs=[ada_image_input],
        compute_precision=ct.precision.FLOAT16,
        minimum_deployment_target=ct.target.macOS14
    )
    ada_out_path = os.path.join(model_dir, "adaface_ir101.mlpackage")
    if os.path.exists(ada_out_path):
        import shutil
        shutil.rmtree(ada_out_path)
    ada_mlmodel.save(ada_out_path)
    print("AdaFace IR101 (112x112 ImageType) converted successfully.")

glintr_onnx = os.path.join(weights_dir, "glintr100.onnx")
if os.path.exists(glintr_onnx):
    print("Converting GLINTR100 (112x112 FP16 ImageType for Neural Engine)...")
    glintr_torch = onnx2torch.convert(glintr_onnx).eval()
    dummy_glintr = torch.randn(1, 3, 112, 112)
    traced_glintr = torch.jit.trace(glintr_torch, dummy_glintr)
    glintr_image_input = ct.ImageType(
        name='input_1',
        shape=(1, 3, 112, 112),
        scale=1.0 / 127.5,
        bias=[-1.0, -1.0, -1.0],
        color_layout=ct.colorlayout.RGB
    )
    glintr_mlmodel = ct.convert(
        traced_glintr,
        inputs=[glintr_image_input],
        compute_precision=ct.precision.FLOAT16,
        minimum_deployment_target=ct.target.macOS14
    )
    glintr_out_path = os.path.join(model_dir, "glintr100.mlpackage")
    if os.path.exists(glintr_out_path):
        import shutil
        shutil.rmtree(glintr_out_path)
    glintr_mlmodel.save(glintr_out_path)
    print("GLINTR100 (112x112 ImageType) converted successfully.")
EOF

echo "[convert.sh] Done."
