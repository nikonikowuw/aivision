#pragma once

#include <string>
#include <vector>
#include <nlohmann/json.hpp>
#include "aivision/types.h"

namespace aivision::core {

using SelfTestFrameFactory = bool (*)(const char* package_root, const char* test_image_file,
                                       av_frame_desc* out_frame, void** owner);
using SelfTestFrameReleaser = void (*)(void* owner);
struct PackageManifest {
    std::string algorithm_id;
    std::string version;
    std::string platform_id;
    std::string algorithm_type;
    std::string alarm_type_id;
    std::string min_engine_version;
    uint32_t compute_units = 100;
    std::string library_name;
    nlohmann::json params_schema;
};

struct ValidationResult {
    bool success = false;
    std::string error_code;
    std::string error_stage; // e.g. "structure", "manifest", "dlopen", "self_test"
    std::string error_message;
    std::string package_sha256;
    PackageManifest manifest;
};

class PackageValidator {
public:
    // Seven-step sandbox verification
    static ValidationResult validate_and_extract(const std::string& package_zip_or_dir, const std::string& install_base_dir,
                                                 SelfTestFrameFactory frame_factory = nullptr,
                                                 SelfTestFrameReleaser frame_releaser = nullptr);
    static ValidationResult run_sandbox_validator(const std::string& validator_bin_path, const std::string& package_path, const std::string& install_base_dir);
};

} // namespace aivision::core
