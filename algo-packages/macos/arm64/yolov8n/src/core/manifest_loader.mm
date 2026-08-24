#import <Foundation/Foundation.h>

#include "manifest_loader.hpp"

#include <algorithm>
#include <filesystem>
#include <string>
#include <vector>

namespace fs = std::filesystem;

namespace yolov8n {
namespace {

bool safe_relative_path(const fs::path& path) {
    const std::string text = path.generic_string();
    return !text.empty() && text != "." && !path.is_absolute() &&
           text == path.lexically_normal().generic_string() &&
           text.find('\\') == std::string::npos;
}

bool is_within_root(const fs::path& root, const fs::path& candidate) {
    const std::string relative = candidate.lexically_relative(root).generic_string();
    return !relative.empty() && relative != ".." && !relative.starts_with("../");
}

std::string foundation_error(NSError* error) {
    if (!error) return "unknown Foundation error";
    NSString* description = error.localizedDescription;
    return description.UTF8String ? description.UTF8String : "unknown Foundation error";
}

fs::path find_model_package(const fs::path& model_file) {
    fs::path current = model_file;
    while (!current.empty() && current != current.root_path()) {
        if (current.extension() == ".mlpackage") return current;
        const fs::path parent = current.parent_path();
        if (parent == current) break;
        current = parent;
    }
    return {};
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
            NSDictionary* manifest = static_cast<NSDictionary*>(raw_manifest);
            id raw_files = [manifest objectForKey:@"files"];
            if (![raw_files isKindOfClass:[NSArray class]]) {
                error = "manifest.files must be an array";
                return false;
            }

            std::vector<fs::path> candidates;
            uint32_t model_entry_count = 0;
            for (id raw_entry in static_cast<NSArray*>(raw_files)) {
                if (![raw_entry isKindOfClass:[NSDictionary class]]) {
                    error = "manifest.files contains a non-object entry";
                    return false;
                }
                NSDictionary* entry = static_cast<NSDictionary*>(raw_entry);
                NSString* kind = [entry objectForKey:@"kind"];
                if (![kind isKindOfClass:[NSString class]] || ![kind isEqualToString:@"model"]) continue;

                ++model_entry_count;
                NSString* relative_ns_path = [entry objectForKey:@"path"];
                if (![relative_ns_path isKindOfClass:[NSString class]] || !relative_ns_path.UTF8String) {
                    error = "manifest model path is invalid";
                    return false;
                }
                const fs::path relative_path = fs::path(relative_ns_path.UTF8String);
                if (!safe_relative_path(relative_path)) {
                    error = "manifest model path is unsafe";
                    return false;
                }

                const fs::path model_file = root / relative_path;
                if (fs::is_symlink(model_file) || !fs::is_regular_file(model_file)) {
                    error = "manifest model file is missing or not regular: " + relative_path.generic_string();
                    return false;
                }
                const fs::path model_package = find_model_package(model_file);
                if (model_package.empty() || fs::is_symlink(model_package) || !fs::is_directory(model_package)) {
                    error = "manifest model entry is not inside an .mlpackage directory";
                    return false;
                }
                const fs::path canonical_package = fs::weakly_canonical(model_package);
                if (!is_within_root(root, canonical_package)) {
                    error = "manifest model package escapes package root";
                    return false;
                }
                candidates.push_back(canonical_package);
            }

            if (model_entry_count != 1 || candidates.size() != 1) {
                error = model_entry_count == 0
                    ? "manifest must declare exactly one model file"
                    : "manifest must declare exactly one model entry";
                return false;
            }
            model_path = candidates.front().string();
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

} // namespace yolov8n
