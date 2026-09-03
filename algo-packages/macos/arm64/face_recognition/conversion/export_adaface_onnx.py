#!/usr/bin/env python3
"""
Export AdaFace (IR-101 / IR-50) PyTorch Checkpoint to ONNX for Argus Face Recognition.
Features:
- Opset version: 12 (default)
- Input: [batch_size, 3, 112, 112] (FP32)
- Output: [batch_size, 512] (FP32 Embedding)
- Standard InsightFace/GlintR100 naming and shape compatibility
"""

import argparse
import os
import sys
from collections import namedtuple
import numpy as np
import onnx
import torch
import torch.nn as nn
import torch.nn.functional as F

# ==================== IR-101 / IR-50 骨干网络定义 ====================
class Flatten(nn.Module):
    def forward(self, input):
        return input.view(input.size(0), -1)

class Bottleneck_IR(nn.Module):
    """Modified ResNet Block with Pre-Activation for Face Recognition (InsightFace / AdaFace)."""
    def __init__(self, in_channel, depth, stride):
        super().__init__()
        if in_channel == depth:
            self.shortcut_layer = nn.MaxPool2d(1, stride)
        else:
            self.shortcut_layer = nn.Sequential(
                nn.Conv2d(in_channel, depth, (1, 1), stride, bias=False),
                nn.BatchNorm2d(depth)
            )
        self.res_layer = nn.Sequential(
            nn.BatchNorm2d(in_channel),
            nn.Conv2d(in_channel, depth, (3, 3), (1, 1), 1, bias=False),
            nn.BatchNorm2d(depth),
            nn.PReLU(depth),
            nn.Conv2d(depth, depth, (3, 3), stride, 1, bias=False),
            nn.BatchNorm2d(depth)
        )

    def forward(self, x):
        return self.res_layer(x) + self.shortcut_layer(x)

class Bottleneck(namedtuple("Block", ["in_channel", "depth", "stride"])):
    """A named tuple describing a ResNet block."""

def get_block(in_channel, depth, num_units, stride=2):
    return [Bottleneck(in_channel, depth, stride)] + [Bottleneck(depth, depth, 1) for _ in range(num_units - 1)]

def get_blocks(num_layers):
    if num_layers in (100, 101):
        blocks = [
            get_block(in_channel=64, depth=64, num_units=3),
            get_block(in_channel=64, depth=128, num_units=13),
            get_block(in_channel=128, depth=256, num_units=30),
            get_block(in_channel=256, depth=512, num_units=3)
        ]
    elif num_layers == 50:
        blocks = [
            get_block(in_channel=64, depth=64, num_units=3),
            get_block(in_channel=64, depth=128, num_units=4),
            get_block(in_channel=128, depth=256, num_units=14),
            get_block(in_channel=256, depth=512, num_units=3)
        ]
    else:
        raise ValueError(f"Unsupported num_layers: {num_layers}")
    return blocks

class AdaFaceBackbone(nn.Module):
    def __init__(self, input_size=(112, 112), num_layers=101, output_dim=512):
        super().__init__()
        assert input_size[0] in [112, 224]
        blocks = get_blocks(num_layers)
        
        self.input_layer = nn.Sequential(
            nn.Conv2d(3, 64, (3, 3), 1, 1, bias=False),
            nn.BatchNorm2d(64),
            nn.PReLU(64)
        )
        
        modules = []
        for block in blocks:
            for bottleneck in block:
                modules.append(Bottleneck_IR(bottleneck.in_channel, bottleneck.depth, bottleneck.stride))
        self.body = nn.Sequential(*modules)
        
        self.output_layer = nn.Sequential(
            nn.BatchNorm2d(512),
            nn.Dropout(0.4),
            Flatten(),
            nn.Linear(512 * 7 * 7, output_dim),
            nn.BatchNorm1d(output_dim, affine=False)
        )

    def forward(self, x):
        x = self.input_layer(x)
        x = self.body(x)
        x = self.output_layer(x)
        return x

def parse_args():
    parser = argparse.ArgumentParser(description="Export AdaFace PyTorch Checkpoint to ONNX (Argus Pipeline)")
    parser.add_argument("--ckpt", type=str, required=True, help="Path to .ckpt or .pt checkpoint")
    parser.add_argument("--output", type=str, required=True, help="Output ONNX file path")
    parser.add_argument("--layers", type=int, default=101, choices=[50, 100, 101], help="Number of layers (default: 101)")
    parser.add_argument("--opset", type=int, default=12, help="ONNX opset version (default: 12)")
    parser.add_argument("--input-name", type=str, default="input.1", help="ONNX input tensor name (default: input.1)")
    parser.add_argument("--output-name", type=str, default="output", help="ONNX output tensor name (default: output)")
    return parser.parse_args()

def main():
    args = parse_args()
    ckpt_path = os.path.expanduser(args.ckpt)
    output_path = os.path.expanduser(args.output)

    if not os.path.exists(ckpt_path):
        print(f"Error: Checkpoint file not found: {ckpt_path}")
        sys.exit(1)

    print(f"🐳 [1/4] Loading checkpoint from: {ckpt_path}")
    ckpt = torch.load(ckpt_path, map_location="cpu")
    state_dict = ckpt["state_dict"] if isinstance(ckpt, dict) and "state_dict" in ckpt else ckpt

    # 提取 backbone 权重并剥离 'model.' 前缀与 'head.' 分类头
    clean_sd = {}
    for k, v in state_dict.items():
        if k.startswith("head."):
            continue
        clean_key = k[6:] if k.startswith("model.") else k
        clean_sd[clean_key] = v

    print(f"🐳 [2/4] Building AdaFace IR-{args.layers} Backbone...")
    model = AdaFaceBackbone(input_size=(112, 112), num_layers=args.layers, output_dim=512)
    missing, unexpected = model.load_state_dict(clean_sd, strict=True)
    model.eval()
    print("      ✓ Strict weight loading succeeded (0 missing, 0 unexpected).")

    dummy_input = torch.randn(1, 3, 112, 112, dtype=torch.float32)

    print(f"🐳 [3/4] Exporting to ONNX (opset={args.opset})...")
    os.makedirs(os.path.dirname(os.path.abspath(output_path)), exist_ok=True)
    torch.onnx.export(
        model,
        dummy_input,
        output_path,
        export_params=True,
        opset_version=args.opset,
        do_constant_folding=True,
        input_names=[args.input_name],
        output_names=[args.output_name],
        dynamic_axes={
            args.input_name: {0: "batch_size"},
            args.output_name: {0: "batch_size"}
        }
    )

    print("🐳 [4/4] Validating ONNX structure...")
    onnx_model = onnx.load(output_path)
    onnx.checker.check_model(onnx_model)
    file_size_mb = os.path.getsize(output_path) / (1024 * 1024)
    print(f"✨ Successfully exported to: {output_path} ({file_size_mb:.2f} MB, opset={args.opset})")

if __name__ == "__main__":
    main()
