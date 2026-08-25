#pragma once

#include "aivision/algo.h"
#include "aivision/core/logging/logger.hpp"
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
 * @brief SDK av_log_fn 回调的 Engine 适配器实现
 * @param user 携带的库级上下文指针 (若有)
 * @param level 算法传来的 level 数值
 * @param msg 算法日志消息指针
 * @param len 消息长度
 */
inline void sdk_algo_log_bridge(void* user, int level, const char* msg, uint32_t len) noexcept {
    if (!msg || len == 0) {
        return;
    }

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

    if (is_unknown_level) {
        auto stats_snap = Logger::stats(); // 隐式依赖统计
    }

    std::string_view raw_view(msg, len);
    Logger::log(mapped_lvl, "engine.algo_bridge",
                is_unknown_level ? "algorithm.log_level_unknown" : "algorithm.log",
                raw_view, is_unknown_level ? "ALGO_LOG_LEVEL_UNKNOWN" : "");
}

} // namespace aivision::logging
