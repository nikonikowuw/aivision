#pragma once

/**
 * @file algo_manager.hpp
 * @brief 算法实例全局注册与生命周期管理中心（单例）
 * 
 * 维护系统内全部活跃的 AlgorithmInstance 对象，提供按 ID 检索、增删、
 * 检查算法包版本引用情况（防止卸载在用算法包）以及全局停止能力。
 */

#include <memory>
#include <mutex>
#include <string>
#include <vector>
#include <unordered_map>

#include "aivision/core/algo_instance.hpp"

namespace aivision::core {

/**
 * @brief 算法实例全局管理器
 */
class AlgoManager {
public:
    static AlgoManager& instance();

    /**
     * @brief 注册并添加一个算法实例
     * @param instance 算法实例智能指针
     * @return bool 成功返回 true，若已存在相同 ID 则返回 false
     */
    bool add(const std::shared_ptr<AlgorithmInstance>& instance);

    /**
     * @brief 获取指定 ID 的算法实例
     * @param instance_id 实例唯一标识
     */
    std::shared_ptr<AlgorithmInstance> get(const std::string& instance_id) const;

    /**
     * @brief 移除并停止指定算法实例
     * @param instance_id 实例唯一标识
     */
    bool remove(const std::string& instance_id);

    /**
     * @brief 检查指定算法包版本是否正被某个活跃实例引用
     * @param algorithm_id 算法包标识
     * @param version 版本号
     */
    bool has_package_reference(const std::string& algorithm_id, const std::string& version) const;

    /**
     * @brief 获取当前所有实例 ID 列表
     */
    std::vector<std::string> instance_ids() const;

    /**
     * @brief 停止并清空所有算法实例
     */
    void stop_all();

    /**
     * @brief 获取当前管理的算法实例总数
     */
    size_t size() const;

private:
    mutable std::mutex mutex_;
    std::unordered_map<std::string, std::shared_ptr<AlgorithmInstance>> instances_;
};

} // namespace aivision::core

