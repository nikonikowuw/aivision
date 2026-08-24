#pragma once

#include <vector>
#include <mutex>
#include <atomic>
#include <unordered_map>
#include <cstdint>
#include <cstring>
#include "aivision/types.h"

namespace aivision::core {

class FramePool {
public:
    static FramePool& instance();

    FramePool();
    ~FramePool();

    // Allocate / borrow frame handle
    av_frame_desc* acquire_frame();
    av_status retain_frame(void* frame_token);
    av_status release_frame(void* frame_token);

    // Get ops function table
    const av_frame_ops* get_frame_ops() const;

    // Associates platform opaque ownership with the token's final release.
    av_status set_opaque_release(void* frame_token, void (*release_fn)(void*));

    // Poison / sanity check
    uint32_t active_frame_count() const;
    // Only clears fully released frames; active tokens are preserved.
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
