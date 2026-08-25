/**
 * @file validator_main.cpp
 * @brief 独立沙箱算法包校验可执行文件主入口（用于进程隔离验证算法包）
 */

#include <iostream>
#include <nlohmann/json.hpp>

#include "aivision/core/algo_sandbox.hpp"
#include "aivision/core/logging/logger.hpp"


#if defined(AIVISION_PLATFORM_MACOS)
extern "C" bool aivision_validator_create_test_frame(const char* package_root,
                                                       const char* test_image_file,
                                                       av_frame_desc* out_frame,
                                                       void** owner);
extern "C" void aivision_validator_release_test_frame(void* owner);
#endif

int main(int argc, char* argv[]) {
    // 0. 初始化结构化日志系统 (输出至 stderr)
    aivision::logging::Logger::initialize(aivision::logging::Level::Info);

    // 命令行参数解析：校验包路径与可选安装基准目录
    if (argc < 2) {
        LOG_ERROR("validator", "validator.args_invalid", "Missing package path argument");
        nlohmann::json err_res;
        err_res["success"] = false;
        err_res["error_code"] = "VALIDATOR_ARGS_INVALID";
        err_res["error_stage"] = "args_parse";
        err_res["error_message"] = "Usage: package_validator <package_dir_or_zip> [install_base_dir]";
        std::cout << err_res.dump() << std::endl;
        aivision::logging::Logger::shutdown();
        return 1;
    }
    const std::string package_path = argv[1];
    const std::string install_base = argc >= 3 ? argv[2] : "";

    LOG_INFO("validator", "validator.started", "Starting package validation", "",
             {{"package_path", package_path}});

    // 调用 PackageValidator 执行七步沙箱校验与解压安装
#if defined(AIVISION_PLATFORM_MACOS)
    const auto result = aivision::core::PackageValidator::validate_and_extract(
        package_path, install_base, aivision_validator_create_test_frame, aivision_validator_release_test_frame);
#else
    const auto result = aivision::core::PackageValidator::validate_and_extract(package_path, install_base);
#endif

    nlohmann::json response;
    response["success"] = result.success;
    response["error_code"] = result.error_code;
    response["error_stage"] = result.error_stage;
    response["error_message"] = result.error_message;

    if (!result.success) {
        LOG_ERROR("validator", "validator.validation_failed", result.error_message, result.error_code,
                  {{"error_stage", result.error_stage}});
        // stdout 严格输出单行 JSON 机器契约
        std::cout << response.dump() << std::endl;
        aivision::logging::Logger::shutdown();
        return 2;
    }

    response["manifest"] = {
        {"algorithm_id", result.manifest.algorithm_id},
        {"version", result.manifest.version}
    };
    response["package_sha256"] = result.package_sha256;

    LOG_INFO("validator", "validator.validated", "Package validated successfully", "",
             {{"algorithm_id", result.manifest.algorithm_id}, {"package_version", result.manifest.version}});

    // stdout 严格输出单行 JSON 机器契约
    std::cout << response.dump() << std::endl;

    aivision::logging::Logger::shutdown();
    return 0;
}
