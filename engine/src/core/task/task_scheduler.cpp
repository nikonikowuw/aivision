#include "aivision/core/task_scheduler.hpp"

namespace aivision::core {

TaskScheduler& TaskScheduler::instance() {
    static TaskScheduler scheduler;
    return scheduler;
}

bool TaskScheduler::add_task(const std::shared_ptr<CameraTask>& task) {
    if (!task) return false;
    std::lock_guard<std::mutex> lock(mutex_);
    return tasks_.emplace(task->get_camera_id(), task).second;
}

std::shared_ptr<CameraTask> TaskScheduler::get_task(const std::string& camera_id) const {
    std::lock_guard<std::mutex> lock(mutex_);
    const auto it = tasks_.find(camera_id);
    return it == tasks_.end() ? nullptr : it->second;
}

av_status TaskScheduler::start_task(const std::string& camera_id) {
    const auto task = get_task(camera_id);
    return task ? task->start() : AV_ERR_INVALID_ARG;
}

bool TaskScheduler::stop_task(const std::string& camera_id) {
    std::shared_ptr<CameraTask> task;
    {
        std::lock_guard<std::mutex> lock(mutex_);
        const auto it = tasks_.find(camera_id);
        if (it == tasks_.end()) return false;
        task = std::move(it->second);
        tasks_.erase(it);
    }
    task->stop();
    return true;
}

void TaskScheduler::stop_all() {
    std::unordered_map<std::string, std::shared_ptr<CameraTask>> tasks;
    {
        std::lock_guard<std::mutex> lock(mutex_);
        tasks.swap(tasks_);
    }
    for (auto& [id, task] : tasks) {
        if (task) task->stop();
    }
}

std::vector<std::string> TaskScheduler::task_ids() const {
    std::lock_guard<std::mutex> lock(mutex_);
    std::vector<std::string> ids;
    ids.reserve(tasks_.size());
    for (const auto& item : tasks_) ids.push_back(item.first);
    return ids;
}

size_t TaskScheduler::size() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return tasks_.size();
}

} // namespace aivision::core
