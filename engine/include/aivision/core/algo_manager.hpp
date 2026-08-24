#pragma once

#include <memory>
#include <mutex>
#include <string>
#include <vector>
#include <unordered_map>

#include "aivision/core/algo_instance.hpp"

namespace aivision::core {

class AlgoManager {
public:
    static AlgoManager& instance();

    bool add(const std::shared_ptr<AlgorithmInstance>& instance);
    std::shared_ptr<AlgorithmInstance> get(const std::string& instance_id) const;
    bool remove(const std::string& instance_id);
    bool has_package_reference(const std::string& algorithm_id, const std::string& version) const;
    std::vector<std::string> instance_ids() const;
    void stop_all();
    size_t size() const;

private:
    mutable std::mutex mutex_;
    std::unordered_map<std::string, std::shared_ptr<AlgorithmInstance>> instances_;
};

} // namespace aivision::core
