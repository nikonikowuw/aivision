#pragma once

#include <string>
#include <unordered_map>
#include <mutex>
#include <cstdint>
#include <functional>
#include "aivision/types.h"

namespace aivision::core {

struct ResourceRequirement {
    std::string instance_id;
    std::string algorithm_id;
    int32_t target_fps = 0;
    uint32_t compute_units = 0;
    uint64_t memory_bytes = 0;
};

class ResourceLedger {
public:
    static ResourceLedger& instance();

    ResourceLedger();
    void set_limits(uint32_t total_compute_units, uint32_t reserved_compute_units, uint64_t min_free_memory_bytes);
    void set_free_memory_provider(std::function<uint64_t()> provider);

    av_status can_allocate(const ResourceRequirement& req, std::string* out_reason = nullptr) const;
    av_status allocate(const ResourceRequirement& req, std::string* out_reason = nullptr);
    void release(const std::string& instance_id);
    void clear();

    [[nodiscard]] uint32_t get_used_compute_units() const;
    [[nodiscard]] uint32_t get_available_compute_units() const;

private:
    av_status check_allocation_locked(const ResourceRequirement& req, std::string* out_reason) const;
    uint64_t free_memory_bytes_locked() const;

    mutable std::mutex mutex_;
    uint32_t total_units_ = 1000;
    uint32_t reserved_units_ = 100;
    uint64_t min_free_mem_ = 256 * 1024 * 1024;
    std::function<uint64_t()> free_memory_provider_;
    std::unordered_map<std::string, ResourceRequirement> allocations_;
};

} // namespace aivision::core
