#pragma once

/**
 * @file frame_pool.hpp
 * @brief 152B av_frame_desc 帧描述符对象池与引用计数生命周期管理
 * 
 * 维护 frame_token 与内部真实帧结构体的映射，提供纯 C ABI 的 av_frame_ops 函数表；
 * 实现对平台底层原生 surface 句柄（opaque）的引用与级联析构释放。
 */

#include <vector>
#include <mutex>
#include <atomic>
#include <unordered_map>
#include <cstdint>
#include <cstring>
#include "aivision/types.h"

namespace aivision::core {

/**
 * @brief 帧对象池与生命周期管理器（单例）
 */
class FramePool {
public:
    static FramePool& instance();

    FramePool();
    ~FramePool();

    /**
     * @brief 从对象池中借出或分配一个空的帧描述符
     */
    av_frame_desc* acquire_frame();

    /**
     * @brief 增加帧引用计数（对应 C ABI retain）
     * @param frame_token 帧唯一令牌
     */
    av_status retain_frame(void* frame_token);

    /**
     * @brief 减少帧引用计数，归零时回收回对象池并释放关联的 opaque 句柄（对应 C ABI release）
     * @param frame_token 帧唯一令牌
     */
    av_status release_frame(void* frame_token);

    /**
     * @brief 获取导出给 C ABI 的 av_frame_ops 函数指针表
     */
    const av_frame_ops* get_frame_ops() const;

    /**
     * @brief 为指定帧令牌绑定平台底层 opaque 句柄的释放回调
     * @param frame_token 帧唯一令牌
     * @param release_fn 析构释放回调
     */
    av_status set_opaque_release(void* frame_token, void (*release_fn)(void*));

    /**
     * @brief 获取当前正处于借出/活跃状态的帧总数
     */
    uint32_t active_frame_count() const;

    /**
     * @brief 重置并清理已完全归还的空闲池节点（保留活跃帧）
     */
    av_status reset();

private:
    struct InternalFrame {
        av_frame_desc desc{};
        std::atomic<int32_t> ref_count{0};
        bool in_use = false;
        std::vector<uint8_t> buffer;
        void (*opaque_release)(void*) = nullptr;
    };

    mutable std::mutex mutex_;
    std::vector<std::unique_ptr<InternalFrame>> pool_;
    std::unordered_map<void*, InternalFrame*> token_map_;
    std::atomic<uintptr_t> token_seq_{0x1000};
    av_frame_ops frame_ops_{};
};

} // namespace aivision::core

