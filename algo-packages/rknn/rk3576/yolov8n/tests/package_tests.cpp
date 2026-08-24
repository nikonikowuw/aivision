#include "aivision/algo.h"
#include <cassert>
#include <iostream>

void test_package_lifecycle() {
    const av_algo_abi* abi = av_algo_get_abi(AV_ALGO_API_VERSION);
    assert(abi != nullptr);

    av_algo_library_args lib_args{};
    lib_args.size = sizeof(lib_args);
    lib_args.api_version = AV_ALGO_API_VERSION;
    lib_args.package_root = ".";
    lib_args.platform_id = "rk3576-rknn";

    av_algo_library lib = nullptr;
    int ret = abi->library_open(&lib_args, &lib);
    assert(ret == AV_OK);
    assert(lib != nullptr);

    av_algo_library_info info{};
    info.size = sizeof(info);
    ret = abi->library_query(lib, &info);
    assert(ret == AV_OK);
    assert(std::string(info.algorithm_id) == "yolov8n");
    assert(std::string(info.alarm_type_id) == "object_detect");

    abi->library_close(lib);
    std::cout << "[PASS] test_package_lifecycle" << std::endl;
}

int main() {
    test_package_lifecycle();
    std::cout << "All Package tests passed!" << std::endl;
    return 0;
}
