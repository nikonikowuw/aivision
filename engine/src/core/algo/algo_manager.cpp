#include "aivision/core/algo_manager.hpp"

namespace aivision::core {

AlgoManager& AlgoManager::instance() {
    static AlgoManager manager;
    return manager;
}

bool AlgoManager::add(const std::shared_ptr<AlgorithmInstance>& instance) {
    if (!instance) return false;
    std::lock_guard<std::mutex> lock(mutex_);
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
        instance = std::move(it->second);
        instances_.erase(it);
    }
    instance->stop();
    return true;
}

bool AlgoManager::has_package_reference(const std::string& algorithm_id, const std::string& version) const {
    std::lock_guard<std::mutex> lock(mutex_);
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
        instances.swap(instances_);
    }
    for (auto& [id, instance] : instances) {
        if (instance) instance->stop();
    }
}

size_t AlgoManager::size() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return instances_.size();
}

} // namespace aivision::core
