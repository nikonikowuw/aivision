#include "aivision/core/resource_ledger.hpp"

#include <limits>
#include <utility>
#if defined(__APPLE__)
#include <mach/mach.h>
#else
#include <unistd.h>
#endif

namespace aivision::core {

ResourceLedger& ResourceLedger::instance() {
    static ResourceLedger inst;
    return inst;
}

ResourceLedger::ResourceLedger()
    : free_memory_provider_([]() -> uint64_t {
#if defined(__APPLE__)
        vm_statistics64_data_t statistics{};
        mach_msg_type_number_t count = HOST_VM_INFO64_COUNT;
        if (host_statistics64(mach_host_self(), HOST_VM_INFO64,
                              reinterpret_cast<host_info64_t>(&statistics), &count) != KERN_SUCCESS) {
            return 0;
        }
        return static_cast<uint64_t>(statistics.free_count + statistics.inactive_count + statistics.speculative_count) * vm_page_size;
#else
        const long pages = ::sysconf(_SC_AVPHYS_PAGES);
        const long page_size = ::sysconf(_SC_PAGESIZE);
        if (pages <= 0 || page_size <= 0) return 0;
        return static_cast<uint64_t>(pages) * static_cast<uint64_t>(page_size);
#endif
    }) {}

void ResourceLedger::set_limits(uint32_t total_compute_units, uint32_t reserved_compute_units,
                                uint64_t min_free_memory_bytes) {
    std::lock_guard<std::mutex> lock(mutex_);
    total_units_ = total_compute_units;
    reserved_units_ = reserved_compute_units;
    min_free_mem_ = min_free_memory_bytes;
}

void ResourceLedger::set_free_memory_provider(std::function<uint64_t()> provider) {
    std::lock_guard<std::mutex> lock(mutex_);
    free_memory_provider_ = std::move(provider);
}

uint64_t ResourceLedger::free_memory_bytes_locked() const {
    if (!free_memory_provider_) return 0;
    try {
        return free_memory_provider_();
    } catch (...) {
        return 0;
    }
}

av_status ResourceLedger::check_allocation_locked(const ResourceRequirement& req,
                                                   std::string* out_reason) const {
    if (req.instance_id.empty() || req.target_fps < 0) {
        if (out_reason) *out_reason = "INVALID_ARG: instance_id and target_fps are invalid";
        return AV_ERR_INVALID_ARG;
    }
    uint64_t current_units = 0;
    for (const auto& [id, item] : allocations_) {
        if (id != req.instance_id) {
            if (current_units > std::numeric_limits<uint64_t>::max() - item.compute_units) {
                current_units = std::numeric_limits<uint64_t>::max();
                break;
            }
            current_units += item.compute_units;
        }
    }

    const uint64_t max_allocatable = total_units_ > reserved_units_
        ? static_cast<uint64_t>(total_units_ - reserved_units_) : 0;
    if (req.compute_units > max_allocatable || current_units > max_allocatable - req.compute_units) {
        if (out_reason) {
            *out_reason = "RESOURCE_LIMIT_EXCEEDED: compute units total=" + std::to_string(total_units_) +
                          " reserved=" + std::to_string(reserved_units_) +
                          " used=" + std::to_string(current_units) +
                          " requested=" + std::to_string(req.compute_units);
        }
        return AV_ERR_OUT_OF_MEMORY;
    }

    uint64_t current_memory = 0;
    for (const auto& [id, item] : allocations_) {
        if (id != req.instance_id) {
            if (current_memory > std::numeric_limits<uint64_t>::max() - item.memory_bytes) {
                current_memory = std::numeric_limits<uint64_t>::max();
                break;
            }
            current_memory += item.memory_bytes;
        }
    }
    const uint64_t free_memory = free_memory_bytes_locked();
    const bool allocation_overflow = current_memory > std::numeric_limits<uint64_t>::max() - req.memory_bytes;
    const uint64_t requested_total = allocation_overflow
        ? std::numeric_limits<uint64_t>::max() : current_memory + req.memory_bytes;
    const bool memory_exceeded = free_memory < min_free_mem_ ||
        requested_total > free_memory - min_free_mem_;
    if (allocation_overflow || memory_exceeded) {
        if (out_reason) {
            *out_reason = "MEMORY_LIMIT_EXCEEDED: free=" + std::to_string(free_memory) +
                          " minimum=" + std::to_string(min_free_mem_) +
                          " used=" + std::to_string(current_memory) +
                          " requested=" + std::to_string(req.memory_bytes);
        }
        return AV_ERR_OUT_OF_MEMORY;
    }
    return AV_OK;
}

av_status ResourceLedger::can_allocate(const ResourceRequirement& req, std::string* out_reason) const {
    std::lock_guard<std::mutex> lock(mutex_);
    return check_allocation_locked(req, out_reason);
}

av_status ResourceLedger::allocate(const ResourceRequirement& req, std::string* out_reason) {
    std::lock_guard<std::mutex> lock(mutex_);
    const av_status status = check_allocation_locked(req, out_reason);
    if (status != AV_OK) return status;
    allocations_[req.instance_id] = req;
    return AV_OK;
}

void ResourceLedger::release(const std::string& instance_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    allocations_.erase(instance_id);
}

void ResourceLedger::clear() {
    std::lock_guard<std::mutex> lock(mutex_);
    allocations_.clear();
}

uint32_t ResourceLedger::get_used_compute_units() const {
    std::lock_guard<std::mutex> lock(mutex_);
    uint64_t current_used = 0;
    for (const auto& [id, item] : allocations_) {
        if (current_used > std::numeric_limits<uint64_t>::max() - item.compute_units) {
            current_used = std::numeric_limits<uint64_t>::max();
            break;
        }
        current_used += item.compute_units;
    }
    return current_used > std::numeric_limits<uint32_t>::max()
        ? std::numeric_limits<uint32_t>::max() : static_cast<uint32_t>(current_used);
}

uint32_t ResourceLedger::get_available_compute_units() const {
    std::lock_guard<std::mutex> lock(mutex_);
    uint64_t current_used = 0;
    for (const auto& [id, item] : allocations_) {
        if (current_used > std::numeric_limits<uint64_t>::max() - item.compute_units) {
            current_used = std::numeric_limits<uint64_t>::max();
            break;
        }
        current_used += item.compute_units;
    }
    const uint64_t max_allocatable = total_units_ > reserved_units_
        ? static_cast<uint64_t>(total_units_ - reserved_units_) : 0;
    return static_cast<uint32_t>(max_allocatable > current_used ? max_allocatable - current_used : 0);
}

} // namespace aivision::core
