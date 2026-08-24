#include <gtest/gtest.h>
#include "aivision/core/resource_ledger.hpp"

TEST(ResourceLedgerTest, QuotaExceeded) {
    auto& ledger = aivision::core::ResourceLedger::instance();
    ledger.clear();
    ledger.set_limits(1000, 100, 256 * 1024 * 1024);
    ledger.set_free_memory_provider([] { return uint64_t{2} * 1024 * 1024 * 1024; });

    aivision::core::ResourceRequirement req1{
        .instance_id = "inst-1",
        .algorithm_id = "yolov8n",
        .target_fps = 25,
        .compute_units = 500,
        .memory_bytes = 100 * 1024 * 1024
    };

    EXPECT_EQ(ledger.allocate(req1), AV_OK);
    EXPECT_EQ(ledger.get_used_compute_units(), 500);
    EXPECT_EQ(ledger.get_available_compute_units(), 400); // 1000 - 100 - 500 = 400

    aivision::core::ResourceRequirement req2{
        .instance_id = "inst-2",
        .algorithm_id = "yolov8n",
        .target_fps = 25,
        .compute_units = 500,
        .memory_bytes = 100 * 1024 * 1024
    };

    std::string reason;
    EXPECT_EQ(ledger.allocate(req2, &reason), AV_ERR_OUT_OF_MEMORY);
    EXPECT_FALSE(reason.empty());

    ledger.release("inst-1");
    EXPECT_EQ(ledger.get_used_compute_units(), 0);
    EXPECT_EQ(ledger.allocate(req2), AV_OK);

    ledger.clear();
    ledger.set_free_memory_provider([] { return uint64_t{128} * 1024 * 1024; });
    std::string memory_reason;
    EXPECT_EQ(ledger.can_allocate(req1, &memory_reason), AV_ERR_OUT_OF_MEMORY);
    EXPECT_NE(memory_reason.find("MEMORY_LIMIT_EXCEEDED"), std::string::npos);

    ledger.clear();
    ledger.set_free_memory_provider([] { return uint64_t{512} * 1024 * 1024; });
    aivision::core::ResourceRequirement memory_req = req1;
    memory_req.memory_bytes = 200 * 1024 * 1024;
    EXPECT_EQ(ledger.allocate(memory_req), AV_OK);
    memory_req.instance_id = "inst-2";
    EXPECT_EQ(ledger.can_allocate(memory_req), AV_ERR_OUT_OF_MEMORY);

    // Release on failure regression test
    ledger.release("inst-1");
    EXPECT_EQ(ledger.get_used_compute_units(), 0);
}
