#include "aivision/algo.h"

#include <cstdint>
#include <cstdlib>

#ifndef YOLOV8N_PACKAGE_ROOT
#error "YOLOV8N_PACKAGE_ROOT must be defined"
#endif

namespace {

void result_callback(const av_algo_result*, void*) noexcept {}

void require_condition(bool condition) {
    if (!condition) std::abort();
}

} // namespace

int main() {
    const av_algo_abi* abi = av_algo_get_abi(AV_ALGO_API_VERSION);
    require_condition(abi != nullptr);

    av_algo_library_args library_args{};
    library_args.size = sizeof(library_args);
    library_args.api_version = AV_ALGO_API_VERSION;
    library_args.package_root = YOLOV8N_PACKAGE_ROOT;
    library_args.platform_id = "macos-arm64-coreml";
    av_algo_library library = nullptr;
    require_condition(abi->library_open(&library_args, &library) == AV_OK);
    require_condition(library != nullptr);

    av_algo_instance_args args{};
    args.size = sizeof(args);
    args.api_version = AV_ALGO_API_VERSION;
    args.mode = AV_INSTANCE_NORMAL;
    args.instance_id = "abi-test";
    args.instance_run_id = "abi-test-run";
    args.on_result = result_callback;

    av_algo_instance instance = reinterpret_cast<av_algo_instance>(static_cast<uintptr_t>(1));
    require_condition(abi->instance_create(library, &args, &instance) == AV_ERR_CONFIG_INVALID);
    require_condition(instance == nullptr);

    const char empty_object[] = "{}";
    args.config_json = empty_object;
    args.config_json_len = sizeof(empty_object) - 1;
    instance = reinterpret_cast<av_algo_instance>(static_cast<uintptr_t>(1));
    require_condition(abi->instance_create(library, &args, &instance) == AV_ERR_CONFIG_INVALID);
    require_condition(instance == nullptr);

    const char valid_config[] =
        "{\"confidence_threshold\":0.5,\"iou_threshold\":0.45}";
    args.config_json = valid_config;
    args.config_json_len = sizeof(valid_config) - 1;
    instance = nullptr;
    require_condition(abi->instance_create(library, &args, &instance) == AV_OK);
    require_condition(instance != nullptr);
    require_condition(abi->instance_destroy(instance) == AV_OK);

    args.mode = AV_INSTANCE_INSTALL_SELF_TEST;
    args.config_json = nullptr;
    args.config_json_len = 0;
    instance = nullptr;
    require_condition(abi->instance_create(library, &args, &instance) == AV_OK);
    require_condition(instance != nullptr);
    require_condition(abi->instance_destroy(instance) == AV_OK);

    require_condition(abi->library_close(library) == AV_OK);
    return 0;
}
