#pragma once

/**
 * @file resource_ledger.hpp
 * @brief 边缘推理算力点数与内存账本调度器
 * 
 * 在启动算法实例前执行严格的准入控制与算力/内存预扣费：
 * 1. 维护总算力单元（如 1000 点）、系统预留（如 100 点）和已分配账单；
 * 2. 依据算法包声明的 compute_units 和 target_fps 评估并发容量；
 * 3. 实时查询系统空闲内存，防止 OOM。
 */

#include <string>
#include <unordered_map>
#include <mutex>
#include <cstdint>
#include <functional>
#include "aivision/types.h"

namespace aivision::core {

/**
 * @brief 算法实例资源需求描述
 */
struct ResourceRequirement {
    std::string instance_id;       ///< 申请者实例 ID
    std::string algorithm_id;      ///< 关联的算法包 ID
    int32_t target_fps = 0;        ///< 目标抽帧率
    uint32_t compute_units = 0;    ///< 需占用的算力单元点数
    uint64_t memory_bytes = 0;     ///< 预估内存开销（字节）
};

/**
 * @brief 边缘资源账本（单例）
 */
class ResourceLedger {
public:
    static ResourceLedger& instance();

    ResourceLedger();

    /**
     * @brief 动态配置总算力、保留算力与内存安全水线
     */
    void set_limits(uint32_t total_compute_units, uint32_t reserved_compute_units, uint64_t min_free_memory_bytes);

    /**
     * @brief 设置获取当前系统可用空闲内存的回调函数
     */
    void set_free_memory_provider(std::function<uint64_t()> provider);

    /**
     * @brief 试探性检查是否能够分配指定资源（不实际扣减）
     * @param req 资源需求
     * @param out_reason 若失败输出拒绝原因
     */
    av_status can_allocate(const ResourceRequirement& req, std::string* out_reason = nullptr) const;

    /**
     * @brief 申请并扣减算力资源
     * @param req 资源需求
     * @param out_reason 若失败输出拒绝原因
     */
    av_status allocate(const ResourceRequirement& req, std::string* out_reason = nullptr);

    /**
     * @brief 释放指定实例占用的算力与资源
     * @param instance_id 实例 ID
     */
    void release(const std::string& instance_id);

    /**
     * @brief 清空所有已分配的资源账单
     */
    void clear();

    /**
     * @brief 获取当前已使用的算力点数
     */
    [[nodiscard]] uint32_t get_used_compute_units() const;

    /**
     * @brief 获取当前可供分配的剩余算力点数（扣除保留算力）
     */
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

