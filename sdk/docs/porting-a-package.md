# 算法包开发与独立可搬运指南 (Algorithm Package Porting Guide)

## 1. 算法包工程结构

算法包采用模块化子目录结构与独立可搬运设计：

```
algo-packages/<family>/<model>/<algorithm>/
├── Makefile
├── CMakeLists.txt
├── manifest.json
├── testimage.jpg
├── vendor/aivision-sdk/          # Vendored SDK 头文件与工具库
├── src/
│   ├── preprocess/               # 图像预处理
│   ├── inference/                # 平台推理运行时封装 (Core ML / RKNN / CANN)
│   ├── postprocess/              # NMS / BBox 反向映射
│   ├── core/                     # 导出 av_algo_get_abi C ABI 入口
│   └── runner/                   # 单机调试与 benchmark 入口
└── conversion/                   # 模型转换证据链
```

## 2. 独立可搬运硬判据

每个算法包内置完整的 `vendor/aivision-sdk/` 副本，在没有主仓库代码的目标机器上（如复制到 `/tmp` 或开发板），直接运行：

```bash
make configure
make build
make run
make benchmark
make package
```

即可实现零依赖编译、单机调试、性能基准压测与标准分发 Zip 包生成。
