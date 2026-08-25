# C++ Engine 结构化日志 实施计划 (Implementation Plan)

## 1. 实施阶段与步骤拆解 (Execution Breakdown)

### Phase 1: 日志基础设施与核心库集成
- [ ] **Step 1.1: 依赖集成与 CMake 配置**
  - 在 `engine/third_party` 引入固定版本的 vendored `spdlog`（仅私有静态链接，不暴露给对外头文件）。
  - 配置 `engine/CMakeLists.txt`，添加 `engine_logging` 模块构建目标与测试目标。
  - 验证：`make -C engine configure` 成功，无符号冲突与冗余暴露。
- [ ] **Step 1.2: 核心数据结构与脱敏清洗工具**
  - 实现 `LogRecord`、`Level`、`LogContextSnapshot` 及 `SourceLocation`。
  - 实现字符串清洗器（UTF-8 校验、控制字符转义、URL 凭据脱敏、敏感字段黑名单过滤、超长截断）。
  - 验证：编写针对脱敏与截断的单元测试并全部通过。
- [ ] **Step 1.3: 双有界队列与异步 Writer 引擎**
  - 实现 Normal Queue (2048) 与 High Queue (256) 的非阻塞入队机制。
  - 实现基于公平调度的后台 Writer 线程与优雅停机（最多 2s 排空超时）。
  - 实现内存/测试专用的 MemorySink 与生产用的 StderrSink。
  - 实现线程安全的 `LoggerStats` 统计。
  - 验证：编写高并发写入、队列溢出丢弃、优雅关闭超时的多线程单元测试。

### Phase 2: 上层 Facade、上下文与 SDK 桥接
- [ ] **Step 2.1: Logger Facade 与 RAII 作用域上下文**
  - 实现 `aivision::logging::Logger` 单例/Facade，包含显式 `initialize()` / `shutdown()`。
  - 实现 `ScopedLogContext`（`thread_local` 栈）及入队深拷贝快照。
  - 提供 `noexcept` 的 `LOG_DEBUG`, `LOG_INFO`, `LOG_WARN`, `LOG_ERROR`, `LOG_FATAL` 宏与类型化 API。
  - 验证：多线程 Worker 复用上下文隔离测试，确认并发无数据竞争与上下文串线。
- [ ] **Step 2.2: SDK `av_log_fn` 桥接与 C ABI 状态码映射**
  - 在 `sdk/include/aivision/algo.h` 补充 `av_algo_log_level` 枚举（向后兼容）。
  - 在 Engine 侧实现 `av_log_fn` 宿主适配器：完成级别映射（0..5）、未知级别降级与自由文本清洗。
  - 实现 C ABI 状态码到稳定 `code` 的显式映射表（如 `AV_ERR_INFERENCE_FAILED` → `ALGO_PROCESS_FAILED`）。
  - 验证：编写 SDK 桥接与错误码映射契约测试。

### Phase 3: 业务模块接入与 Validator 协议重构
- [ ] **Step 3.1: Engine 主进程改造**
  - 改造 `engine/src/app/main.cpp`，在入口显式初始化 Logger，替换原有的 `std::cout`/`std::cerr`。
  - 读取环境变量 `AIVISION_LOG_LEVEL` 设置全局阈值（非法值安全回退到 `INFO`）。
  - 验证：Engine 启动与停止日志正常输出 JSONL。
- [ ] **Step 3.2: Package Validator 进程重构**
  - 重构 `engine/src/validator/validator_main.cpp`：
    - `stdout` 仅输出单行机器结果 JSON（包含 `success`, `error_code`, `error_stage`, `manifest`, `package_sha256`）。
    - 初始化 Logger，所有调试/错误日志通过 `stderr` 输出。
  - 重构 `engine/src/core/algo/algo_sandbox.cpp` 的 `capture_command`：
    - 只捕获 `stdout` 作为机器协议解析，`stderr` 直通宿主输出。
    - 控制流彻底基于 `success` 与 `error_code`，废除文本匹配。
  - 验证：校验成功、损坏包、篡改 checksum、格式非法包的跨进程沙箱测试。

### Phase 4: 质量验证与系统集成
- [ ] **Step 4.1: 本地质量与 Sanitizer 门禁**
  - 运行全量单元测试：`make -C engine test`。
  - 运行 ASan 内存安全测试：`make -C engine asan`。
  - 运行 TSan 线程并发安全测试：`make -C engine tsan`。
- [ ] **Step 4.2: 编写 systemd 部署文档**
  - 编写配置说明：明确 `StandardError=journal`、服务标识及 `journalctl -o json` 查询方法。

---

## 2. 验证命令集 (Verification Commands)

```bash
# 1. 编译构建
make -C engine configure
make -C engine build

# 2. 单元测试与契约测试
make -C engine test

# 3. 内存与并发 Sanitizer 检查
make -C engine asan
make -C engine tsan
```
