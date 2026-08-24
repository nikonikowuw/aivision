# Engine Toolchain & Dependencies

- **OS**: macOS Sequoia (Apple Silicon arm64)
- **Compiler**: AppleClang (C++20, C11) / GCC (aarch64)
- **CMake**: >= 3.24
- **Build System**: Ninja
- **Homebrew Packages**:
  - `grpc` (1.83.0_3)
  - `protobuf` (36.0)
  - `googletest` (1.18.0)
  - `nlohmann-json`
- **Submodules**:
  - `ZLMediaKit` (with `ZLToolKit`, `media-server`, `jsoncpp`)
