/**
 * @file algo_manager.cpp
 * @brief 算法实例全局管理器单例实现
 */

#include "aivision/core/algo_manager.hpp"

namespace aivision::core {

AlgoManager& AlgoManager::instance() {
    // 线程安全的 Meyers 单例模式
    static AlgoManager manager;
    return manager;
}

bool AlgoManager::add(const std::shared_ptr<AlgorithmInstance>& instance) {
    if (!instance) return false;
    std::lock_guard<std::mutex> lock(mutex_);
    // emplace 插入，若 instance_id 已存在则返回 false，保证 ID 唯一性
    return instances_.emplace(instance->get_instance_id(), instance).second;
}

std::shared_ptr<AlgorithmInstance> AlgoManager::get(const std::string& instance_id) const {
    std::lock_guard<std::mutex> lock(mutex_);
    const auto it = instances_.find(instance_id);
    return it == instances_.end() ? nullptr : it->second;
}

bool AlgoManager::remove(const std::string& instance_id) {
    std::shared_ptr<AlgorithmInstance> instance;
    {
        std::lock_guard<std::mutex> lock(mutex_);
        const auto it = instances_.find(instance_id);
        if (it == instances_.end()) return false;
        // 先从哈希表中移出，减少持有锁的时间
        instance = std::move(it->second);
        instances_.erase(it);
    }
    // 在锁外调用 stop()，防止与实例内部工作线程发生死锁
    instance->stop();
    return true;
}

bool AlgoManager::has_package_reference(const std::string& algorithm_id, const std::string& version) const {
    std::lock_guard<std::mutex> lock(mutex_);
    // 遍历所有实例，检查是否有正在运行的实例依赖指定的算法包版本（用于算法卸载时的引用保护）
    for (const auto& [id, instance] : instances_) {
        if (instance && instance->get_algorithm_id() == algorithm_id && instance->get_version() == version) return true;
    }
    return false;
}

std::vector<std::string> AlgoManager::instance_ids() const {
    std::lock_guard<std::mutex> lock(mutex_);
    std::vector<std::string> ids;
    ids.reserve(instances_.size());
    for (const auto& [id, instance] : instances_) ids.push_back(id);
    return ids;
}

void AlgoManager::stop_all() {
    std::unordered_map<std::string, std::shared_ptr<AlgorithmInstance>> instances;
    {
        std::lock_guard<std::mutex> lock(mutex_);
        // 原子交换清空当前容器
        instances.swap(instances_);
    }
    // 在锁外逐个优雅停止所有算法实例
    for (auto& [id, instance] : instances) {
        if (instance) instance->stop();
    }
}

size_t AlgoManager::size() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return instances_.size();
}

} // namespace aivision::core
