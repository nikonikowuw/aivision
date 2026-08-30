/**
 * @file task_scheduler.cpp
 * @brief 摄像头任务调度器单例实现
 */

#include "argus/core/task_scheduler.hpp"

namespace argus::core {

TaskScheduler& TaskScheduler::instance() {
    // 线程安全的 Meyers 单例
    static TaskScheduler scheduler;
    return scheduler;
}

bool TaskScheduler::add_task(const std::shared_ptr<CameraTask>& task) {
    if (!task) return false;
    std::lock_guard<std::mutex> lock(mutex_);
    // emplace 插入，若 camera_id 已存在则返回 false，保证单个摄像头任务的唯一性
    return tasks_.emplace(task->get_camera_id(), task).second;
}

std::shared_ptr<CameraTask> TaskScheduler::get_task(const std::string& camera_id) const {
    std::lock_guard<std::mutex> lock(mutex_);
    const auto it = tasks_.find(camera_id);
    return it == tasks_.end() ? nullptr : it->second;
}

av_status TaskScheduler::start_task(const std::string& camera_id) {
    // 查找并启动指定摄像头的拉流与解码任务
    const auto task = get_task(camera_id);
    return task ? task->start() : AV_ERR_INVALID_ARG;
}

bool TaskScheduler::stop_task(const std::string& camera_id) {
    std::shared_ptr<CameraTask> task;
    {
        std::lock_guard<std::mutex> lock(mutex_);
        const auto it = tasks_.find(camera_id);
        if (it == tasks_.end()) return false;
        // 先从哈希表中移除以缩小临界区
        task = std::move(it->second);
        tasks_.erase(it);
    }
    // 在锁外调用 stop()，停止工作线程与资源释放
    task->stop();
    return true;
}

void TaskScheduler::stop_all() {
    std::unordered_map<std::string, std::shared_ptr<CameraTask>> tasks;
    {
        std::lock_guard<std::mutex> lock(mutex_);
        tasks.swap(tasks_);
    }
    // 停止调度器管理的所有摄像头任务
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

} // namespace argus::core
