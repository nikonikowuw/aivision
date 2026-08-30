/**
 * @file validator_main.cpp
 * @brief 独立沙箱算法包校验可执行文件主入口（用于进程隔离验证算法包）
 */

#include <cstdlib>
#include <exception>
#include <iostream>
#include <nlohmann/json.hpp>

#include "argus/core/algo_sandbox.hpp"
#include "argus/core/logging/logger.hpp"


#if defined(ARGUS_PLATFORM_MACOS)
extern "C" bool argus_validator_create_test_frame(const char* package_root,
                                                       const char* test_image_file,
                                                       av_frame_desc* out_frame,
                                                       void** owner);
extern "C" void argus_validator_release_test_frame(void* owner);
#endif

int main(int argc, char* argv[]) {
    // 0. 初始化结构化日志系统 (输出至 stderr)
    argus::logging::Level log_level = argus::logging::Level::Info;
    bool invalid_log_level = false;
    if (const char* value = std::getenv("ARGUS_LOG_LEVEL")) {
        const auto parsed = argus::logging::parse_level(value);
        if (parsed) {
            log_level = *parsed;
        } else {
            invalid_log_level = true;
        }
    }
    argus::logging::Logger::initialize(log_level);
    if (invalid_log_level) {
        LOG_WARN("validator", "validator.log_level_invalid", "Invalid ARGUS_LOG_LEVEL; falling back to INFO",
                 "VALIDATOR_LOG_LEVEL_INVALID");
    }

    // 命令行参数解析：校验包路径与可选安装基准目录
    if (argc < 2) {
        LOG_ERROR("validator", "validator.args_invalid", "Missing package path argument",
                  "VALIDATOR_ARGS_INVALID");
        nlohmann::json err_res;
        err_res["success"] = false;
        err_res["error_code"] = "VALIDATOR_ARGS_INVALID";
        err_res["error_stage"] = "args_parse";
        err_res["error_message"] = "Usage: package_validator <package_dir_or_zip> [install_base_dir]";
        std::cout << err_res.dump() << std::endl;
        argus::logging::Logger::shutdown();
        return 1;
    }
    const std::string package_path = argv[1];
    const std::string install_base = argc >= 3 ? argv[2] : "";

    LOG_INFO("validator", "validator.started", "Starting package validation");

    argus::core::ValidationResult result;
    try {
#if defined(ARGUS_PLATFORM_MACOS)
        result = argus::core::PackageValidator::validate_and_extract(
            package_path, install_base, argus_validator_create_test_frame, argus_validator_release_test_frame);
#else
        result = argus::core::PackageValidator::validate_and_extract(package_path, install_base);
#endif
    } catch (const std::exception& exception) {
        result.error_stage = "validator_exception";
        result.error_code = "VALIDATOR_INTERNAL_ERROR";
        result.error_message = exception.what();
    } catch (...) {
        result.error_stage = "validator_exception";
        result.error_code = "VALIDATOR_INTERNAL_ERROR";
        result.error_message = "unknown validator exception";
    }

    nlohmann::json response;
    const std::string error_code = result.error_code.empty() ? "PACKAGE_VALIDATION_FAILED" : result.error_code;
    const std::string error_stage = result.error_stage.empty() ? "validation" : result.error_stage;
    const std::string error_message = result.error_message.empty() ? "package validation failed" : result.error_message;
    response["success"] = result.success;
    response["error_code"] = result.success ? "" : error_code;
    response["error_stage"] = result.success ? "" : error_stage;
    response["error_message"] = result.success ? "" : error_message;

    if (!result.success) {
        LOG_ERROR("validator", "validator.validation_failed", error_message, error_code,
                  {{"error_stage", error_stage}});
        // stdout 严格输出单行 JSON 机器契约
        std::cout << response.dump() << std::endl;
        argus::logging::Logger::shutdown();
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

    argus::logging::Logger::shutdown();
    return 0;
}
