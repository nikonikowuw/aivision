#!/usr/bin/env python3
"""
Export Ultralytics YOLOv8n to ONNX with 6 decoupled output branches
(3 scales x [box_dfl64, cls80]) for RK3576 RKNN INT8 quantization.
"""

import argparse
import sys
import torch
import torch.nn as nn

def parse_args():
    parser = argparse.ArgumentParser(description="Export YOLOv8n to RKNN-friendly ONNX")
    parser.add_argument("--weights", type=str, default="yolov8n.pt", help="PyTorch model weights path")
    parser.add_argument("--output", type=str, default="yolov8n.onnx", help="Output ONNX file path")
    parser.add_argument("--width", type=int, default=640, help="Input width")
    parser.add_argument("--height", type=int, default=384, help="Input height (16:9 surveillance standard)")
    parser.add_argument("--opset", type=int, default=12, help="ONNX opset version")
    return parser.parse_args()

class RKNNYOLOv8DetectHead(nn.Module):
    """
    Decoupled detect head that outputs raw box (64-channel DFL) and class (80-channel logits)
    tensors without Softmax / Sigmoid / Concat inside the NPU graph.
    """
    def __init__(self, original_detect):
        super().__init__()
        self.nc = original_detect.nc
        self.nl = original_detect.nl
        self.reg_max = original_detect.reg_max
        self.cv2 = original_detect.cv2
        self.cv3 = original_detect.cv3

    def forward(self, x):
        outputs = []
        for i in range(self.nl):
            # Box feature branch: [1, 64, H_i, W_i]
            box_out = self.cv2[i](x[i])
            # Cls feature branch: [1, 80, H_i, W_i] (apply sigmoid to produce class probabilities in [0, 1])
            cls_out = self.cv3[i](x[i]).sigmoid()
            outputs.extend([box_out, cls_out])
        return tuple(outputs)

def main():
    args = parse_args()
    print(f"[export_onnx] Loading Ultralytics YOLOv8 from {args.weights}...")
    try:
        from ultralytics import YOLO
    except ImportError:
        print("Error: ultralytics package not installed. Run: pip install ultralytics")
        sys.exit(1)

    yolo = YOLO(args.weights)
    model = yolo.model
    model.eval()

    # Replace head with RKNN decoupled head
    original_head = model.model[-1]
    model.model[-1] = RKNNYOLOv8DetectHead(original_head)

    dummy_input = torch.zeros(1, 3, args.height, args.width)
    output_names = [
        "box_stride8", "cls_stride8",
        "box_stride16", "cls_stride16",
        "box_stride32", "cls_stride32"
    ]

    print(f"[export_onnx] Exporting ONNX to {args.output} (input: 1x3x{args.height}x{args.width})...")
    torch.onnx.export(
        model,
        dummy_input,
        args.output,
        opset_version=args.opset,
        input_names=["images"],
        output_names=output_names,
        dynamic_axes=None
    )
    print(f"[export_onnx] Successfully exported {args.output}")

if __name__ == "__main__":
    main()
