"""
Export Ultralytics YOLOv8n to ONNX with 3 raw output branches (strides 8, 16, 32)
without decode head for RKNN INT8 quantization.
"""

import sys

def main():
    print("[export_onnx] Airockchip 3-branch YOLOv8n ONNX export script template.")
    print("Usage: python export_onnx.py --weights yolov8n.pt --output yolov8n.onnx")

if __name__ == '__main__':
    main()
