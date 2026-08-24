#pragma once

#include <string>

namespace yolov8n {

bool resolve_manifest_model_path(const std::string& package_root,
                                 std::string& model_path,
                                 std::string& error) noexcept;

} // namespace yolov8n
