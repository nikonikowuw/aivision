#include "aivision/core/logging/log_context.hpp"

namespace aivision::logging {

namespace {

thread_local std::vector<LogContextSnapshot> tl_context_stack;

// 辅助合并两个快照（右侧覆盖左侧有效非空值）
void merge_snapshot(LogContextSnapshot& target, const LogContextSnapshot& src) {
    if (!src.platform_id.empty()) target.platform_id = src.platform_id;
    if (!src.device_id.empty()) target.device_id = src.device_id;
    if (!src.camera_id.empty()) target.camera_id = src.camera_id;
    if (!src.task_id.empty()) target.task_id = src.task_id;
    if (!src.instance_id.empty()) target.instance_id = src.instance_id;
    if (!src.instance_run_id.empty()) target.instance_run_id = src.instance_run_id;
    if (!src.algorithm_id.empty()) target.algorithm_id = src.algorithm_id;
    if (!src.package_version.empty()) target.package_version = src.package_version;
    if (src.frame_id >= 0) target.frame_id = src.frame_id;
    if (src.revision >= 0) target.revision = src.revision;
    if (src.retry_count >= 0) target.retry_count = src.retry_count;
    if (src.duration_ms >= 0.0) target.duration_ms = src.duration_ms;
}

} // namespace

void LogContext::push(const LogContextSnapshot& snapshot) noexcept {
    try {
        tl_context_stack.push_back(snapshot);
    } catch (...) {
        // 异常安全保护
    }
}

void LogContext::pop() noexcept {
    if (!tl_context_stack.empty()) {
        tl_context_stack.pop_back();
    }
}

LogContextSnapshot LogContext::current() noexcept {
    LogContextSnapshot merged;
    try {
        for (const auto& item : tl_context_stack) {
            merge_snapshot(merged, item);
        }
    } catch (...) {
        // 异常安全保护
    }
    return merged;
}

void LogContext::clear() noexcept {
    tl_context_stack.clear();
}

} // namespace aivision::logging
