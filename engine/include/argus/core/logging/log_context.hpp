/**
 * @file log_context.hpp
 * @brief 结构化日志线程局部上下文与 RAII 作用域管理
 */
#pragma once

#include "aivision/core/logging/log_record.hpp"
#include <string>
#include <vector>

namespace aivision::logging {

/**
 * @brief 线程级日志上下文管理器 (基于 thread_local 栈)
 */
class LogContext {
public:
    /**
     * @brief 推入新作用域上下文
     */
    [[nodiscard]] static bool push(const LogContextSnapshot& snapshot) noexcept;

    /**
     * @brief 弹出当前作用域上下文
     */
    static void pop() noexcept;

    /**
     * @brief 获取当前线程的合并上下文快照
     */
    [[nodiscard]] static LogContextSnapshot current() noexcept;

    /**
     * @brief 清空当前线程上下文 (用于 Worker 线程复用重置)
     */
    static void clear() noexcept;
};

/**
 * @brief RAII 作用域上下文辅助类
 */
class ScopedLogContext {
public:
    explicit ScopedLogContext(const LogContextSnapshot& snapshot) noexcept
        : active_(LogContext::push(snapshot)) {}
    ~ScopedLogContext() noexcept {
        if (active_) LogContext::pop();
    }

    // 禁用拷贝与移动
    ScopedLogContext(const ScopedLogContext&) = delete;
    ScopedLogContext& operator=(const ScopedLogContext&) = delete;

private:
    bool active_{false};
};

} // namespace aivision::logging
