/**
 * @file log_adapter.hpp
 * @brief Engine 与算法包 C ABI 日志回调及状态码映射适配器
 */
#pragma once

#include "aivision/algo.h"
#include "aivision/core/logging/logger.hpp"
#include <algorithm>
#include <string>
#include <string_view>

namespace aivision::logging {

/**
 * @brief 将 C ABI av_algo_status 状态码映射为 Engine 全局稳定错误码字符串
 */
[[nodiscard]] inline std::string_view map_abi_status_to_code(int status) noexcept {
    switch (status) {
        case AV_OK:
            return "";
        case AV_ERR_UNSUPPORTED_API:
            return "ALGO_ABI_INCOMPATIBLE";
        case AV_ERR_INVALID_ARG:
        case AV_ERR_CONFIG_INVALID:
            return "CONFIG_INVALID";
        case AV_ERR_INCOMPATIBLE_FRAME:
            return "FRAME_CAPS_INCOMPATIBLE";
        case AV_ERR_MODEL_LOAD_FAILED:
            return "ALGO_MODEL_LOAD_FAILED";
        case AV_ERR_INFERENCE_FAILED:
            return "ALGO_PROCESS_FAILED";
        case AV_ERR_OUT_OF_MEMORY:
            return "MEMORY_LIMIT_EXCEEDED";
        case AV_ERR_NOT_IMPLEMENTED:
            return "ALGO_NOT_IMPLEMENTED";
        case AV_ERR_TIMEOUT:
            return "ALGO_PROCESS_TIMEOUT";
        case AV_ERR_INTERNAL:
        default:
            return "INTERNAL_ERROR";
    }
}

/**
 * @brief 算法库级日志上下文，生命周期必须覆盖 library_open 到 library_close
 */
struct AlgoLogContext {
    std::string algorithm_id;
    std::string package_version;
    std::string platform_id;
};
/**
 * @brief SDK av_log_fn 回调的 Engine 适配器实现
 * @param user 携带的库级上下文指针 (若有)
 * @param level 算法传来的 level 数值
 * @param msg 算法日志消息指针
 * @param len 消息长度
 */
inline void sdk_algo_log_bridge(void* user, int level, const char* msg, uint32_t len) noexcept {
    try {
        if (!msg || len == 0) return;

        // 算法包日志长度不可信：以规范消息上限截断读取窗口，并在嵌入 NUL 处提前停止，
        // 防止恶意或损坏的 len 触发越界读取（算法包经 dlopen 加载，属不可信输入）。
        const uint32_t bounded_len = std::min(
            len, static_cast<uint32_t>(LogSanitizer::MAX_MESSAGE_SIZE));
        size_t actual_len = 0;
        while (actual_len < bounded_len && msg[actual_len] != '\0') {
            ++actual_len;
        }
        if (actual_len == 0) return;

        Level mapped_lvl = Level::Info;
        bool is_unknown_level = false;
        switch (level) {
            case AV_ALGO_LOG_TRACE:
            case AV_ALGO_LOG_DEBUG:
                mapped_lvl = Level::Debug;
                break;
            case AV_ALGO_LOG_INFO:
                mapped_lvl = Level::Info;
                break;
            case AV_ALGO_LOG_WARN:
                mapped_lvl = Level::Warn;
                break;
            case AV_ALGO_LOG_ERROR:
                mapped_lvl = Level::Error;
                break;
            case AV_ALGO_LOG_FATAL:
                mapped_lvl = Level::Fatal;
                break;
            default:
                mapped_lvl = Level::Warn;
                is_unknown_level = true;
                break;
        }

        if (is_unknown_level) Logger::record_unknown_algo_level();

        LogFields fields;
        if (const auto* context = static_cast<const AlgoLogContext*>(user)) {
            if (!context->algorithm_id.empty()) fields.emplace("algorithm_id", context->algorithm_id);
            if (!context->package_version.empty()) fields.emplace("package_version", context->package_version);
            if (!context->platform_id.empty()) fields.emplace("platform_id", context->platform_id);
        }

        std::string_view code;
        if (is_unknown_level) {
            code = "ALGO_LOG_LEVEL_UNKNOWN";
        } else if (mapped_lvl == Level::Error) {
            code = "ALGO_LOG_ERROR";
        } else if (mapped_lvl == Level::Fatal) {
            code = "ALGO_LOG_FATAL";
        }

        Logger::log(mapped_lvl, "engine.algo_bridge",
                    is_unknown_level ? "algorithm.log_level_unknown" : "algorithm.log",
                    std::string_view(msg, actual_len), code, fields);
    } catch (...) {
        // C ABI 回调不得把 C++ 异常传播到算法动态库。
    }
}

} // namespace aivision::logging
