#pragma once

#include <filesystem>
#include <string>
#include <vector>

namespace argus::utils {

struct PackageLibraryEntry {
    std::filesystem::path path;
    std::string relative_path;
};

inline bool resolve_conventional_package_library(const std::filesystem::path& package_root,
                                                 const std::string& algorithm_id,
                                                 PackageLibraryEntry& entry,
                                                 std::string& error) {
    entry = {};
    error.clear();
    if (algorithm_id.size() < 3 || algorithm_id.size() > 32) {
        error = "algorithm_id is invalid";
        return false;
    }
    for (const unsigned char ch : algorithm_id) {
        if (!((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-')) {
            error = "algorithm_id is invalid";
            return false;
        }
    }
    const std::string library_stem = "lib" + algorithm_id;
    std::vector<std::filesystem::path> candidates;
    for (const char* suffix : {".dylib", ".so"}) {
        const auto candidate = package_root / "lib" / (library_stem + suffix);
        std::error_code status_error;
        const auto status = std::filesystem::symlink_status(candidate, status_error);
        if (status_error && status_error != std::errc::no_such_file_or_directory) {
            error = "cannot inspect conventional library entry: " + status_error.message();
            return false;
        }
        if (status_error == std::errc::no_such_file_or_directory || !std::filesystem::exists(status)) continue;
        if (std::filesystem::is_symlink(status) || !std::filesystem::is_regular_file(status)) {
            error = "conventional library entry is not a regular file";
            return false;
        }
        candidates.push_back(candidate);
    }
    if (candidates.size() != 1) {
        error = "package must contain exactly one conventional library entry: lib/lib" +
                algorithm_id + ".{dylib,so}";
        return false;
    }
    entry.path = candidates.front();
    entry.relative_path = entry.path.lexically_relative(package_root).generic_string();
    return true;
}

} // namespace argus::utils
