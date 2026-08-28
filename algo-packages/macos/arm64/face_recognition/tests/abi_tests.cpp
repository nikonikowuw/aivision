#include "aivision/algo.h"
#include <cassert>
#include <iostream>
#include <string>
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

    abi->library_close(lib);
    dlclose(handle);

    std::cout << "[PASS] face_recognition ABI and metadata tests" << std::endl;
    return 0;
}
