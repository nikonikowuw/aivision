/**
 * @file frame_pool.cpp
 * @brief 帧对象池（FramePool）与引用计数管理实现
 */

#include "aivision/core/frame_pool.hpp"
#include <limits>


namespace aivision::core {

static int retain_frame_thunk(void* ctx, void* token) {
    auto* pool = static_cast<FramePool*>(ctx);
    if (!pool) pool = &FramePool::instance();
    return pool->retain_frame(token);
}

static int release_frame_thunk(void* ctx, void* token) {
    auto* pool = static_cast<FramePool*>(ctx);
    if (!pool) pool = &FramePool::instance();
    return pool->release_frame(token);
}

FramePool& FramePool::instance() {
    static FramePool inst;
    return inst;
}

FramePool::FramePool() {
    frame_ops_.size = sizeof(av_frame_ops);
    frame_ops_.api_version = AV_ALGO_API_VERSION;
    frame_ops_.ctx = this;
    frame_ops_.retain = retain_frame_thunk;
    frame_ops_.release = release_frame_thunk;
}

FramePool::~FramePool() = default;

av_frame_desc* FramePool::acquire_frame() {
    std::lock_guard<std::mutex> lock(mutex_);
    // 1. 优先从对象池复用闲置的帧描述符
    for (auto& item : pool_) {
        if (!item->in_use && item->ref_count.load() == 0) {
            item->in_use = true;
            item->ref_count.store(1);
            item->desc = {};
            item->opaque_release = nullptr;
            item->desc.size = sizeof(av_frame_desc);
            item->desc.api_version = AV_ALGO_API_VERSION;
            item->desc.frame_token = reinterpret_cast<void*>(token_seq_.fetch_add(0x10));
            token_map_[item->desc.frame_token] = item.get();
            return &item->desc;
        }
    }

    // 2. 若无空闲对象则新建并扩容对象池
    auto item = std::make_unique<InternalFrame>();
    item->in_use = true;
    item->ref_count.store(1);
    item->desc.size = sizeof(av_frame_desc);
    item->desc.api_version = AV_ALGO_API_VERSION;
    item->desc.frame_token = reinterpret_cast<void*>(token_seq_.fetch_add(0x10));

    InternalFrame* ptr = item.get();
    token_map_[ptr->desc.frame_token] = ptr;
    pool_.push_back(std::move(item));
    return &ptr->desc;
}

av_status FramePool::retain_frame(void* frame_token) {
    if (!frame_token) return AV_ERR_INVALID_ARG;
    std::lock_guard<std::mutex> lock(mutex_);
    // 查找并递增 frame_token 对应的内部引用计数
    auto it = token_map_.find(frame_token);
    if (it == token_map_.end()) return AV_ERR_INVALID_ARG;
    auto* frame = it->second;
    const int32_t refs = frame->ref_count.load();
    if (!frame->in_use || refs <= 0 || refs == std::numeric_limits<int32_t>::max()) return AV_ERR_INVALID_ARG;
    frame->ref_count.store(refs + 1);
    return AV_OK;
}

av_status FramePool::release_frame(void* frame_token) {
    if (!frame_token) return AV_ERR_INVALID_ARG;
    void (*opaque_release)(void*) = nullptr;
    void* opaque = nullptr;
    {
        std::lock_guard<std::mutex> lock(mutex_);
        auto it = token_map_.find(frame_token);
        if (it == token_map_.end()) return AV_ERR_INVALID_ARG;
        auto* frame = it->second;
        const int32_t refs = frame->ref_count.load();
        if (!frame->in_use || refs <= 0) return AV_ERR_INVALID_ARG;
        if (refs == 1) {
            // 引用归零：归还对象池并准备释放底层原生 surface / DMA-BUF 句柄
            frame->ref_count.store(0);
            frame->in_use = false;
            if (!frame->buffer.empty()) {
                std::fill(frame->buffer.begin(), frame->buffer.end(), 0xA5);
            }
            opaque_release = frame->opaque_release;
            opaque = frame->desc.opaque;
            frame->opaque_release = nullptr;
            frame->desc.opaque = nullptr;
            frame->desc.frame_token = nullptr;
            token_map_.erase(it);
        } else {
            frame->ref_count.store(refs - 1);
        }
    }
    // 在锁外调用平台析构函数释放硬件 surface，防止阻塞帧池并发
    if (opaque_release && opaque) opaque_release(opaque);
    return AV_OK;
}

const av_frame_ops* FramePool::get_frame_ops() const {
    return &frame_ops_;
}

av_status FramePool::set_opaque_release(void* frame_token, void (*release_fn)(void*)) {
    if (!frame_token || !release_fn) return AV_ERR_INVALID_ARG;
    std::lock_guard<std::mutex> lock(mutex_);
    const auto it = token_map_.find(frame_token);
    if (it == token_map_.end() || !it->second->in_use || it->second->opaque_release) return AV_ERR_INVALID_ARG;
    it->second->opaque_release = release_fn;
    return AV_OK;
}

uint32_t FramePool::active_frame_count() const {
    std::lock_guard<std::mutex> lock(mutex_);
    uint32_t count = 0;
    for (const auto& item : pool_) {
        if (item->in_use || item->ref_count.load() > 0) count++;
    }
    return count;
}

av_status FramePool::reset() {
    std::lock_guard<std::mutex> lock(mutex_);
    for (const auto& item : pool_) {
        if (item->in_use || item->ref_count.load() != 0) {
            return AV_ERR_INVALID_ARG;
        }
    }
    token_map_.clear();
    pool_.clear();
    return AV_OK;
}

} // namespace aivision::core
