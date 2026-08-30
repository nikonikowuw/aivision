#pragma once

/**
 * @file task_scheduler.hpp
 * @brief 摄像头任务全局调度与生命周期管理中心（单例）
 * 
 * 维护系统内全部 CameraTask 实例，负责任务的新增、查找、启停、全局停止与 ID 枚举。
 */

#include <memory>
#include <mutex>
#include <string>
#include <unordered_map>
#include <vector>

#include "argus/core/camera_task.hpp"

namespace argus::core {

/**
 * @brief 摄像头任务调度器
 */
class TaskScheduler {
public:
    static TaskScheduler& instance();

    /**
     * @brief 添加一个摄像头任务
     * @param task 摄像头任务智能指针
     * @return bool 成功返回 true，若已存在相同 camera_id 则返回 false
     */
    bool add_task(const std::shared_ptr<CameraTask>& task);

    /**
     * @brief 获取指定 ID 的摄像头任务
     * @param camera_id 摄像头唯一标识
     */
    std::shared_ptr<CameraTask> get_task(const std::string& camera_id) const;

    /**
     * @brief 启动指定的摄像头任务
     * @param camera_id 摄像头唯一标识
     */
    av_status start_task(const std::string& camera_id);

    /**
     * @brief 停止并移除指定摄像头任务
     * @param camera_id 摄像头唯一标识
     */
    bool stop_task(const std::string& camera_id);

    /**
     * @brief 停止并清理所有摄像头任务
     */
    void stop_all();

    /**
     * @brief 获取当前所有摄像头任务 ID 列表
     */
    std::vector<std::string> task_ids() const;

    /**
     * @brief 获取当前管理的摄像头任务总数
     */
    size_t size() const;

private:
    mutable std::mutex mutex_;
    std::unordered_map<std::string, std::shared_ptr<CameraTask>> tasks_;
};

} // namespace argus::core

