#include "aivision/algo.h"
#include <cassert>
#include <iostream>
#include <cstring>

void test_abi_get() {
    const av_algo_abi* abi = av_algo_get_abi(AV_ALGO_API_VERSION);
    assert(abi != nullptr);
    assert(abi->size >= sizeof(av_algo_abi));
    assert(abi->library_open != nullptr);
    assert(abi->library_query != nullptr);
    assert(abi->library_close != nullptr);
    assert(abi->instance_create != nullptr);
    assert(abi->instance_negotiate != nullptr);
    assert(abi->instance_update_config != nullptr);
    assert(abi->instance_set_rules != nullptr);
    assert(abi->instance_process != nullptr);
    assert(abi->instance_flush != nullptr);
    assert(abi->instance_destroy != nullptr);
    assert(abi->last_error != nullptr);

    assert(av_algo_get_abi(9999) == nullptr);
    std::cout << "[PASS] test_abi_get" << std::endl;
}

int main() {
    test_abi_get();
    std::cout << "All ABI tests passed!" << std::endl;
    return 0;
}
