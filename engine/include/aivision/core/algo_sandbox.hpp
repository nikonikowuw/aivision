#pragma once

/**
 * @file algo_sandbox.hpp
 * @brief 算法包沙箱校验与解压安装器
 * 
 * 实现算法包安装时严格的七步沙箱校验：
 * 1. 结构与路径安全性（防止 ZipSlip 等路径遍历）；
 * 2. SHA-256 完整性校验；
 * 3. 清单（manifest.json）格式与平台标识兼容性校验；
 * 4. 参数配置 Schema（config.schema.json）校验；
 * 5. 符号完整性与动态库加载（dlopen）校验；
 * 6. 自检测试帧输入与单帧推理运行（self-test）；
 * 7. 原子解压落盘并生成版本记录。
 */

#include <string>
#include <vector>
#include <nlohmann/json.hpp>
#include "aivision/types.h"

namespace aivision::core {

/// 自检测试帧生成函数指针
using SelfTestFrameFactory = bool (*)(const char* package_root, const char* test_image_file,
                                       av_frame_desc* out_frame, void** owner);
/// 自检测试帧释放函数指针
using SelfTestFrameReleaser = void (*)(void* owner);

/**
 * @brief 算法包元数据清单（解析自 manifest.json）
 */
struct PackageManifest {
    std::string algorithm_id;               ///< 算法标识
    std::string version;                    ///< 版本号
    std::string platform_id;                ///< 目标平台
    std::string algorithm_type;             ///< 算法类型（如 "alarm", "recognition"）
    std::string alarm_type_id;              ///< 告警类型标识（识别类为空）
    std::string min_engine_version;         ///< 要求的最低引擎版本
    uint32_t compute_units = 100;           ///< 算力消耗评估值（默认100点）
    std::string library_name;               ///< 动态库名称
    nlohmann::json params_schema;           ///< 参数 JSON Schema 对象
};

/**
 * @brief 校验与安装输出结果
 */
struct ValidationResult {
    bool success = false;                   ///< 是否校验通过
    std::string error_code;                 ///< 机器可读的错误码
    std::string error_stage;                ///< 失败发生的阶段（如 "structure", "manifest", "dlopen", "self_test"）
    std::string error_message;              ///< 诊断日志
    std::string package_sha256;             ///< 算法包文件的 SHA-256 哈希值
    PackageManifest manifest;               ///< 成功解析出的算法包清单
};

/**
 * @brief 算法包沙箱校验工具类
 */
class PackageValidator {
public:
    /**
     * @brief 执行原地/解压沙箱校验（七步校验流水线）
     * @param package_zip_or_dir 算法包 zip 文件路径或解压目录
     * @param install_base_dir 目标安装根目录
     * @param frame_factory 测试帧生成器
     * @param frame_releaser 测试帧释放器
     */
    static ValidationResult validate_and_extract(const std::string& package_zip_or_dir, const std::string& install_base_dir,
                                                 SelfTestFrameFactory frame_factory = nullptr,
                                                 SelfTestFrameReleaser frame_releaser = nullptr);

    /**
     * @brief 通过独立子进程（posix_spawn）调用独立沙箱可执行文件进行进程隔离校验
     * @param validator_bin_path validator_main 二进制文件路径
     * @param package_path 待安装算法包路径
     * @param install_base_dir 目标安装目录
     */
    static ValidationResult run_sandbox_validator(const std::string& validator_bin_path, const std::string& package_path, const std::string& install_base_dir);
};

} // namespace aivision::core

