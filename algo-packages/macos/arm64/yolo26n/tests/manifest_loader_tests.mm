#include "core/manifest_loader.hpp"

#include <chrono>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <string>

namespace fs = std::filesystem;

namespace {

void require_condition(bool condition) {
    if (!condition) std::abort();
}

void write_file(const fs::path& path, const std::string& content) {
    std::ofstream output(path);
    require_condition(static_cast<bool>(output));
    output << content;
    require_condition(static_cast<bool>(output));
}

} // namespace

int main() {
    const auto suffix = std::chrono::steady_clock::now().time_since_epoch().count();
    const fs::path root = fs::temp_directory_path() / ("argus-yolov8n-loader-" + std::to_string(suffix));
    std::error_code cleanup_error;
    fs::remove_all(root, cleanup_error);
    require_condition(!cleanup_error);

    const fs::path model_package = root / "weights/custom.mlpackage";
    const fs::path model_file = model_package / "Data/com.apple.CoreML/model.mlmodel";
    fs::create_directories(model_file.parent_path());
    write_file(root / "manifest.json", "{}\n");
    write_file(root / ".env", "MODEL_PATH=weights/custom.mlpackage\n");
    write_file(model_file, "model\n");

    std::string model_path;
    std::string error;
    require_condition(yolov8n::resolve_manifest_model_path(root.string(), model_path, error));
    require_condition(model_path == fs::weakly_canonical(model_package).string());

    const fs::path linked_model_root = root / "linked";
    std::error_code symlink_error;
    fs::create_symlink(root / "weights", linked_model_root, symlink_error);
    require_condition(!symlink_error);
    write_file(root / ".env", "MODEL_PATH=linked/custom.mlpackage\n");
    require_condition(!yolov8n::resolve_manifest_model_path(root.string(), model_path, error));
    fs::remove(linked_model_root, cleanup_error);
    require_condition(!cleanup_error);

    const fs::path outside_env = root.parent_path() / (root.filename().string() + "-outside.env");
    write_file(outside_env, "MODEL_PATH=weights/custom.mlpackage\n");
    fs::remove(root / ".env", cleanup_error);
    require_condition(!cleanup_error);
    symlink_error.clear();
    fs::create_symlink(outside_env, root / ".env", symlink_error);
    require_condition(!symlink_error);
    require_condition(!yolov8n::resolve_manifest_model_path(root.string(), model_path, error));
    fs::remove(root / ".env", cleanup_error);
    require_condition(!cleanup_error);
    write_file(root / ".env", "MODEL_PATH=weights/custom.mlpackage\n");

    const fs::path outside_model = root.parent_path() / (root.filename().string() + "-outside.mlmodel");
    write_file(outside_model, "model\n");
    fs::remove(model_file, cleanup_error);
    require_condition(!cleanup_error);
    symlink_error.clear();
    fs::create_symlink(outside_model, model_file, symlink_error);
    require_condition(!symlink_error);
    require_condition(!yolov8n::resolve_manifest_model_path(root.string(), model_path, error));

    fs::remove_all(root, cleanup_error);
    require_condition(!cleanup_error);
    fs::remove(outside_model, cleanup_error);
    require_condition(!cleanup_error);
    fs::remove(outside_env, cleanup_error);
    require_condition(!cleanup_error);
    return 0;
}
