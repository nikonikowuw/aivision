#!/usr/bin/env python3
"""
Convert decoupled YOLOv8n ONNX to RKNN format for RK3576 target
with asymmetric INT8 quantization.
"""

import argparse
import os
import sys

def parse_args():
    parser = argparse.ArgumentParser(description="Convert YOLOv8n ONNX to RKNN for RK3576")
    parser.add_argument("--onnx", type=str, required=True, help="Input ONNX model path")
    parser.add_argument("--target", type=str, default="rk3576", help="Target platform (rk3576)")
    parser.add_argument("--output", type=str, default="yolov8n.rknn", help="Output RKNN model path")
    parser.add_argument("--dataset", type=str, default="dataset.txt", help="Quantization calibration dataset path")
    parser.add_argument("--quantize", action="store_true", default=True, help="Enable INT8 quantization")
    return parser.parse_args()

def main():
    args = parse_args()

    try:
        from rknn.api import RKNN
    except ImportError:
        print("Error: rknn-toolkit2 is not installed in the current Python environment.")
        print("Please install RKNN-Toolkit2 (e.g. `pip install rknn-toolkit2`).")
        sys.exit(1)

    rknn = RKNN(verbose=False)

    print(f"--> Config RKNN for {args.target}")
    rknn.config(
        mean_values=[[0, 0, 0]],
        std_values=[[255, 255, 255]],
        target_platform=args.target,
        quantized_algorithm="normal",
        quantized_dtype="asymmetric_affine"
    )

    print(f"--> Loading ONNX model: {args.onnx}")
    ret = rknn.load_onnx(model=args.onnx)
    if ret != 0:
        print("Error: Load ONNX failed!")
        sys.exit(ret)

    print(f"--> Building RKNN model (quantize={args.quantize})...")
    ret = rknn.build(do_quantization=args.quantize, dataset=args.dataset if args.quantize else None)
    if ret != 0:
        print("Error: Build RKNN failed!")
        sys.exit(ret)

    print(f"--> Exporting RKNN model to: {args.output}")
    ret = rknn.export_rknn(args.output)
    if ret != 0:
        print("Error: Export RKNN failed!")
        sys.exit(ret)

    print(f"--> Conversion completed successfully: {args.output}")
    rknn.release()

if __name__ == "__main__":
    main()
