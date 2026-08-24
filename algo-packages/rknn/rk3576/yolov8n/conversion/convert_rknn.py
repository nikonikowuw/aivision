"""
Convert ONNX model to RKNN for RK3576 target with asymmetric INT8 quantization.
"""

import sys

def main():
    print("[convert_rknn] RKNN-Toolkit2 conversion script for RK3576 YOLOv8n.")
    print("Usage: python convert_rknn.py --onnx yolov8n.onnx --target rk3576 --output yolov8n.rknn")

if __name__ == '__main__':
    main()
