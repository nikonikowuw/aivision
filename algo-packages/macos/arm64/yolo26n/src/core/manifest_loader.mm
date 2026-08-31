#import <Foundation/Foundation.h>

#include "manifest_loader.hpp"
#include "argus/utils/env.hpp"

#include <filesystem>
#include <string>
#include <vector>

namespace fs = std::filesystem;

namespace yolo26n {
namespace {

std::string foundation_error(NSError* error) {
    if (!error) return "unknown Foundation error";
    NSString* description = error.localizedDescription;
    return description.UTF8String ? description.UTF8String : "unknown Foundation error";
}

bool safe_relative_model_path(const std::string& value) {
    if (value.empty() || value.find('\\') != std::string::npos || value.find('\0') != std::string::npos) return false;
    const fs::path path(value);
    if (path.is_absolute()) return false;
    for (const auto& part : path) {
        if (part == "." || part == ".." || part.empty()) return false;
    }
    return true;
}

bool validate_model_path_components(const fs::path& root, const fs::path& candidate, std::string& error) {
    const fs::path relative = candidate.lexically_relative(root);
    if (relative.empty() || relative == "." || relative == ".." || relative.string().starts_with("../")) {
        error = "model path is outside package root";
        return false;
    }
    fs::path current = root;
    std::error_code ec;
    const auto root_status = fs::symlink_status(current, ec);
    if (ec || fs::is_symlink(root_status)) {
        error = "package root is not a regular directory";
        return false;
    }
    for (const auto& component : relative) {
        current /= component;
        const auto status = fs::symlink_status(current, ec);
        if (ec || fs::is_symlink(status)) {
            error = "model path contains a symbolic link";
            return false;
        }
    }
    return true;
}

bool validate_model_bundle(const fs::path& root, const fs::path& candidate,
                           fs::path& canonical_candidate, std::string& error) {
    if (!validate_model_path_components(root, candidate, error)) return false;
    std::error_code ec;
    const auto status = fs::symlink_status(candidate, ec);
    if (ec || !fs::exists(status) || fs::is_symlink(status) || !fs::is_directory(status)) {
        error = "model package is missing or not a regular directory";
        return false;
    }
    canonical_candidate = fs::weakly_canonical(candidate, ec);
    if (ec) {
        error = "cannot canonicalize model package";
        return false;
    }
    const fs::path canonical_root = fs::weakly_canonical(root, ec);
    if (ec) {
        error = "cannot canonicalize package root";
        return false;
    }
    const std::string relative = canonical_candidate.lexically_relative(canonical_root).generic_string();
    if (relative.empty() || relative == ".." || relative.rfind("../", 0) == 0) {
        error = "model package escapes package root";
        return false;
    }

    std::error_code iterator_error;
    fs::recursive_directory_iterator it(candidate, iterator_error);
    if (iterator_error) {
        error = "cannot inspect model package";
        return false;
    }
    for (const fs::recursive_directory_iterator end; it != end; it.increment(iterator_error)) {
        if (iterator_error) {
            error = "cannot inspect model package";
            return false;
        }
        const auto child_status = it->symlink_status(iterator_error);
        if (iterator_error || fs::is_symlink(child_status) ||
            (!fs::is_directory(child_status) && !fs::is_regular_file(child_status))) {
            error = "model package contains an unsafe entry";
            return false;
        }
    }

    const fs::path model_file = candidate / "Data/com.apple.CoreML/model.mlmodel";
    const auto model_file_status = fs::symlink_status(model_file, ec);
    if (ec || fs::is_symlink(model_file_status) || !fs::is_regular_file(model_file_status)) {
        error = "model package is missing Data/com.apple.CoreML/model.mlmodel";
        return false;
    }
    return true;
}

} // namespace

bool resolve_manifest_model_path(const std::string& package_root,
                                 std::string& model_path,
                                 std::string& error) noexcept {
    model_path.clear();
    error.clear();
    @autoreleasepool {
        try {
            const fs::path root = fs::weakly_canonical(fs::path(package_root));
            if (!fs::is_directory(root)) {
                error = "package root is not a directory";
                return false;
            }

            const fs::path manifest_path = root / "manifest.json";
            NSString* manifest_ns_path = [NSString stringWithUTF8String:manifest_path.string().c_str()];
            NSData* data = manifest_ns_path ? [NSData dataWithContentsOfFile:manifest_ns_path] : nil;
            if (!data) {
                error = "manifest.json is missing or unreadable";
                return false;
            }

            NSError* json_error = nil;
            id raw_manifest = [NSJSONSerialization JSONObjectWithData:data options:0 error:&json_error];
            if (![raw_manifest isKindOfClass:[NSDictionary class]]) {
                error = "manifest.json must contain a JSON object: " + foundation_error(json_error);
                return false;
            }
            const fs::path env_path = root / ".env";
            std::error_code env_status_error;
            const auto env_status = fs::symlink_status(env_path, env_status_error);
            if (env_status_error != std::errc::no_such_file_or_directory && env_status_error) {
                error = "cannot inspect package .env";
                return false;
            }
            if (!env_status_error && (!fs::exists(env_status) || fs::is_symlink(env_status) ||
                                      !fs::is_regular_file(env_status))) {
                error = ".env is not a regular file";
                return false;
            }
            const auto local_env = argus::utils::EnvReader::load_file(env_path.string());
            const auto model_path_value = local_env.find("MODEL_PATH");
            fs::path canonical_package;
            if (model_path_value != local_env.end() && !model_path_value->second.empty()) {
                if (!safe_relative_model_path(model_path_value->second) ||
                    fs::path(model_path_value->second).extension() != ".mlpackage") {
                    error = "MODEL_PATH must identify a safe relative .mlpackage path";
                    return false;
                }
                if (!validate_model_bundle(root, root / model_path_value->second,
                                            canonical_package, error)) {
                    return false;
                }
            } else {
                const fs::path model_root = root / "model";
                std::error_code iterator_error;
                const auto model_root_status = fs::symlink_status(model_root, iterator_error);
                if (iterator_error || !fs::exists(model_root_status) || fs::is_symlink(model_root_status) ||
                    !fs::is_directory(model_root_status)) {
                    error = "model directory is missing or not a regular directory";
                    return false;
                }

                std::vector<fs::path> candidates;
                fs::directory_iterator it(model_root, iterator_error);
                if (iterator_error) {
                    error = "cannot inspect model directory";
                    return false;
                }
                const fs::directory_iterator end;
                for (; it != end; it.increment(iterator_error)) {
                    if (iterator_error) {
                        error = "cannot inspect model directory";
                        return false;
                    }
                    const fs::path candidate = it->path();
                    const auto candidate_status = it->symlink_status(iterator_error);
                    if (iterator_error || fs::is_symlink(candidate_status)) {
                        error = "model directory contains a symbolic link";
                        return false;
                    }
                    if (candidate.extension() == ".mlpackage") {
                        candidates.push_back(candidate);
                    }
                }
                if (candidates.size() != 1) {
                    error = candidates.empty()
                        ? "model directory must contain exactly one .mlpackage"
                        : "model directory contains more than one .mlpackage";
                    return false;
                }
                if (!validate_model_bundle(root, candidates.front(), canonical_package, error)) return false;
            }
            model_path = canonical_package.string();
            return true;
        } catch (const std::exception& exception) {
            error = std::string("failed to resolve manifest model: ") + exception.what();
            return false;
        } catch (...) {
            error = "failed to resolve manifest model: unknown exception";
            return false;
        }
    }
}

} // namespace yolo26n
