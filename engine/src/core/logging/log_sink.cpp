/**
 * @file log_sink.cpp
 * @brief stderr 与内存日志 sink 的线程安全实现
 */
#include "aivision/core/logging/log_sink.hpp"
#include <cerrno>
#include <cstdio>
#include <unistd.h>

namespace aivision::logging {

bool StderrSink::write_line(std::string_view line) noexcept {
    if (line.empty()) {
        return true;
    }
    // 使用 POSIX write 系统调用保证原子性与无缓冲直接交付
    const char* ptr = line.data();
    size_t remaining = line.size();
    while (remaining > 0) {
        ssize_t written = ::write(STDERR_FILENO, ptr, remaining);
        if (written < 0) {
            if (errno == EINTR) {
                continue;
            }
            return false;
        }
        if (written == 0) {
            return false;
        }
        ptr += written;
        remaining -= static_cast<size_t>(written);
    }
    return true;
}

void StderrSink::flush() noexcept {
    // write(2) 已直接提交到 journald/pipe；对 stderr 调用 fsync 可能阻塞或返回 EINVAL。
}

bool MemorySink::write_line(std::string_view line) noexcept {
    try {
        std::lock_guard<std::mutex> lock(mutex_);
        lines_.emplace_back(line);
        return true;
    } catch (...) {
        return false;
    }
}

void MemorySink::flush() noexcept {
    // 内存 sink 无需持久化刷新
}

std::vector<std::string> MemorySink::get_lines() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return lines_;
}

void MemorySink::clear() {
    std::lock_guard<std::mutex> lock(mutex_);
    lines_.clear();
}

size_t MemorySink::size() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return lines_.size();
}

} // namespace aivision::logging
