#pragma once

#include <memory>
#include <mutex>
#include <string>
#include <unordered_map>
#include <vector>

#include "aivision/core/camera_task.hpp"

namespace aivision::core {

class TaskScheduler {
public:
    static TaskScheduler& instance();

    bool add_task(const std::shared_ptr<CameraTask>& task);
    std::shared_ptr<CameraTask> get_task(const std::string& camera_id) const;
    av_status start_task(const std::string& camera_id);
    bool stop_task(const std::string& camera_id);
    void stop_all();
    std::vector<std::string> task_ids() const;
    size_t size() const;

private:
    mutable std::mutex mutex_;
    std::unordered_map<std::string, std::shared_ptr<CameraTask>> tasks_;
};

} // namespace aivision::core
