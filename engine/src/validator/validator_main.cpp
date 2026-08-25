/**
 * @file validator_main.cpp
 * @brief 独立沙箱算法包校验可执行文件主入口（用于进程隔离验证算法包）
 */

#include <iostream>

#include "aivision/core/algo_sandbox.hpp"


#if defined(AIVISION_PLATFORM_MACOS)
extern "C" bool aivision_validator_create_test_frame(const char* package_root,
                                                       const char* test_image_file,
                                                       av_frame_desc* out_frame,
                                                       void** owner);
extern "C" void aivision_validator_release_test_frame(void* owner);
#endif

int main(int argc, char* argv[]) {
    // 命令行参数解析：校验包路径与可选安装基准目录
    if (argc < 2) {
        std::cerr << "Usage: package_validator <package_dir_or_zip> [install_base_dir]" << std::endl;
        return 1;
    }
    const std::string package_path = argv[1];
    const std::string install_base = argc >= 3 ? argv[2] : "";

    // 调用 PackageValidator 执行七步沙箱校验与解压安装
#if defined(AIVISION_PLATFORM_MACOS)
    const auto result = aivision::core::PackageValidator::validate_and_extract(
        package_path, install_base, aivision_validator_create_test_frame, aivision_validator_release_test_frame);
#else
    const auto result = aivision::core::PackageValidator::validate_and_extract(package_path, install_base);
#endif
    if (!result.success) {
        std::cerr << "[package_validator] Error code: "
                  << (result.error_code.empty() ? "PACKAGE_VALIDATION_FAILED" : result.error_code) << std::endl;
        std::cerr << "[package_validator] ERROR at stage '" << result.error_stage << "': "
                  << result.error_message << std::endl;
        return 2;
    }

    // 输出标准化结果给父进程 UdsServer 捕获解析
    std::cout << "[package_validator] Successfully validated package: "
              << result.manifest.algorithm_id << "@" << result.manifest.version << std::endl;
    if (!result.package_sha256.empty()) {
        std::cout << "[package_validator] Package SHA-256: " << result.package_sha256 << std::endl;
    }
    return 0;
}
