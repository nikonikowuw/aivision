#include "argus/algo.h"
#include <cassert>
#include <fstream>
#include <iostream>
#include <iterator>
#include <string>
#include <vector>
#include <dlfcn.h>

#ifndef FACE_RECOGNITION_PACKAGE_ROOT
#define FACE_RECOGNITION_PACKAGE_ROOT "."
#endif

int main() {
    const char* env_path = std::getenv("FACE_RECOGNITION_DYLIB");
    std::string dylib_path = env_path ? env_path : "";
#ifdef FACE_RECOGNITION_DYLIB_TARGET
    if (dylib_path.empty()) dylib_path = FACE_RECOGNITION_DYLIB_TARGET;
#endif
    if (dylib_path.empty()) dylib_path = "lib/libface_recognition.dylib";

    void* handle = dlopen(dylib_path.c_str(), RTLD_NOW | RTLD_LOCAL);
    if (!handle) {
        dylib_path = "lib/libface_recognition.dylib";
        handle = dlopen(dylib_path.c_str(), RTLD_NOW | RTLD_LOCAL);
    }
    if (!handle) {
        dylib_path = "build/libface_recognition.dylib";
        handle = dlopen(dylib_path.c_str(), RTLD_NOW | RTLD_LOCAL);
    }
    if (!handle) {
        std::cerr << "Failed to dlopen dylib: " << dlerror() << std::endl;
        return 1;
    }

    auto get_abi = reinterpret_cast<av_algo_get_abi_fn>(dlsym(handle, AV_ALGO_GET_ABI_SYMBOL));
    assert(get_abi != nullptr);

    const av_algo_abi* abi = get_abi(AV_ALGO_API_VERSION);
    assert(abi != nullptr);
    assert(abi->api_version == AV_ALGO_API_VERSION);

    av_algo_library_args lib_args{};
    lib_args.size = sizeof(lib_args);
    lib_args.api_version = AV_ALGO_API_VERSION;
    lib_args.package_root = FACE_RECOGNITION_PACKAGE_ROOT;
    lib_args.platform_id = "macos-arm64-coreml";

    av_algo_library lib = nullptr;
    int st = abi->library_open(&lib_args, &lib);
    assert(st == AV_OK);
    (void)st;
    assert(lib != nullptr);

    av_algo_library_info info{};
    info.size = sizeof(info);
    info.api_version = AV_ALGO_API_VERSION;
    st = abi->library_query(lib, &info);
    assert(st == AV_OK);
    assert(std::string(info.algorithm_id) == "face_recognition");
    assert(std::string(info.algorithm_type) == "face_recognition");
    assert(std::string(info.alarm_type_id) == ""); // Must be empty

    // Instance tests
    std::string captured_result;
    av_algo_instance_args inst_args{};
    inst_args.size = sizeof(inst_args);
    inst_args.api_version = AV_ALGO_API_VERSION;
    inst_args.mode = AV_INSTANCE_NORMAL;
    inst_args.instance_id = "test-inst";
    inst_args.instance_run_id = "test-run";
    inst_args.result_user = &captured_result;
    inst_args.on_result = [](const av_algo_result* res, void* user) {
        if (res && res->json && user) {
            static_cast<std::string*>(user)->assign(res->json, res->json_len);
        }
    };

    av_algo_instance inst = nullptr;
    st = abi->instance_create(lib, &inst_args, &inst);
    assert(st == AV_OK);
    assert(inst != nullptr);

    // Negotiate
    av_frame_caps offered{};
    offered.size = sizeof(offered);
    offered.api_version = AV_ALGO_API_VERSION;
    offered.pixel_format_count = 1;
    offered.pixel_formats[0] = AV_PIX_NV12;
    offered.memory_type_count = 1;
    offered.memory_types[0] = AV_MEM_HOST;

    av_frame_caps accepted{};
    accepted.size = sizeof(accepted);
    accepted.api_version = AV_ALGO_API_VERSION;
    st = abi->instance_negotiate(inst, &offered, &accepted);
    assert(st == AV_OK);
    assert(accepted.pixel_format_count == 1);
    assert(accepted.pixel_formats[0] == AV_PIX_NV12);

    // Update config
    std::string new_cfg = R"({"quality_threshold": 40.0, "max_recognitions_per_track": 2})";
    st = abi->instance_update_config(inst, new_cfg.c_str(), static_cast<uint32_t>(new_cfg.size()));
    assert(st == AV_OK);

    // Set rules (ignored for recognition)
    st = abi->instance_set_rules(inst, nullptr, 0);
    assert(st == AV_OK);

    // Process a dummy 640x384 NV12 frame buffer
    std::vector<uint8_t> dummy_nv12(640 * 384 * 3 / 2, 128);
    av_frame_desc frame{};
    frame.size = sizeof(frame);
    frame.api_version = AV_ALGO_API_VERSION;
    frame.frame_id = 1;
    frame.pts_ns = 1000000000;
    frame.width = 640;
    frame.height = 384;
    frame.pixel_format = AV_PIX_NV12;
    frame.memory_type = AV_MEM_HOST;
    frame.opaque_kind = AV_OPAQUE_NONE;
    frame.opaque = dummy_nv12.data();
    frame.stride[0] = 640;
    frame.stride[1] = 640;

    st = abi->instance_process(inst, &frame);
    assert(st == AV_OK);

    // Flush
    st = abi->instance_flush(inst);
    assert(st == AV_OK);

    // Destroy
    st = abi->instance_destroy(inst);
    assert(st == AV_OK);

    // Test av_algo_extract_face
    auto extract_fn = reinterpret_cast<av_algo_extract_face_fn>(dlsym(handle, AV_ALGO_EXTRACT_FACE_SYMBOL));
    assert(extract_fn != nullptr);

    // Empty input check
    av_face_extract_input ext_in{};
    ext_in.size = sizeof(ext_in);
    ext_in.api_version = AV_ALGO_API_VERSION;
    av_face_extract_output ext_out{};
    ext_out.size = sizeof(ext_out);
    ext_out.api_version = AV_ALGO_API_VERSION;

    st = extract_fn(lib, &ext_in, &ext_out);
    assert(st == AV_OK);
    assert(ext_out.status_code == 4); // decode error / empty

    // 真实图片端到端提取：覆盖解码 → letterbox → SCRFD → 对齐 → GlintR 全链路。
    // 使用单人脸 fixture 而非 testimage.jpg：后者服务于 runner 的多目标跟踪场景，
    // 而 av_algo_extract_face 的契约是单人脸，多人脸会返回 status_code=2。
    // Release 构建会定义 NDEBUG 使 assert 失效，此段用显式判断保证校验真正执行。
    std::string image_path = std::string(FACE_RECOGNITION_PACKAGE_ROOT) + "/tests/fixtures/single_face.jpg";
    std::ifstream image_file(image_path, std::ios::binary);
    if (!image_file) {
        std::cerr << "Failed to open " << image_path << std::endl;
        return 1;
    }
    std::vector<uint8_t> image_bytes((std::istreambuf_iterator<char>(image_file)),
                                     std::istreambuf_iterator<char>());
    image_file.close();
    if (image_bytes.empty()) {
        std::cerr << "single_face.jpg is empty" << std::endl;
        return 1;
    }

    av_face_extract_input real_in{};
    real_in.size = sizeof(real_in);
    real_in.api_version = AV_ALGO_API_VERSION;
    real_in.image_bytes = image_bytes.data();
    real_in.image_bytes_len = static_cast<uint32_t>(image_bytes.size());
    real_in.min_detection_score = 0.50f;
    real_in.min_face_size = 40.0f;
    real_in.min_quality_score = 35.0f;

    av_face_extract_output real_out{};
    real_out.size = sizeof(real_out);
    real_out.api_version = AV_ALGO_API_VERSION;

    st = extract_fn(lib, &real_in, &real_out);
    if (st != AV_OK || real_out.status_code != 0) {
        std::cerr << "extract_face on single_face.jpg failed: st=" << st
                  << " status_code=" << real_out.status_code
                  << " message=" << real_out.error_message << std::endl;
        return 1;
    }
    if (real_out.embedding_dim != 512 || real_out.aligned_jpeg_len == 0) {
        std::cerr << "extract_face returned invalid payload: embedding_dim="
                  << real_out.embedding_dim
                  << " aligned_jpeg_len=" << real_out.aligned_jpeg_len << std::endl;
        return 1;
    }

    // 校验 Embedding L2 模长
    float norm_sq = 0.0f;
    for (uint32_t i = 0; i < real_out.embedding_dim; ++i) {
        norm_sq += real_out.embedding[i] * real_out.embedding[i];
    }
    float norm = std::sqrt(norm_sq);
    if (std::abs(norm - 1.0f) > 0.01f) {
        std::cerr << "embedding L2 norm is not 1.0: " << norm << std::endl;
        return 1;
    }

    abi->library_close(lib);
    dlclose(handle);

    std::cout << "[PASS] face_recognition ABI and metadata tests" << std::endl;
    return 0;
}
