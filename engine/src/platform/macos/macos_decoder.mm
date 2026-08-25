/**
 * @file macos_decoder.mm
 * @brief macOS VideoToolbox 硬件视频解码器实现 (H.264 / H.265)
 * 
 * 包含 NALU 分割与 AVCC 封装、SPS/PPS/VPS 动态解析与 VTDecompressionSession 建立、
 * CVPixelBuffer 异步接收与 av_frame_desc 结构体组装。
 */

#import "aivision/platform/macos_platform.hpp"

#include "aivision/core/color_vui.hpp"

#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <VideoToolbox/VideoToolbox.h>


#include <algorithm>
#include <chrono>
#include <cstdint>
#include <cstring>
#include <deque>
#include <mutex>
#include <string>
#include <vector>

namespace aivision::platform {
namespace {

struct NalUnit {
    std::vector<uint8_t> bytes;
};

size_t start_code_size(const uint8_t* data, size_t size, size_t offset) {
    if (offset + 3 <= size && data[offset] == 0 && data[offset + 1] == 0 && data[offset + 2] == 1) return 3;
    if (offset + 4 <= size && data[offset] == 0 && data[offset + 1] == 0 && data[offset + 2] == 0 && data[offset + 3] == 1) return 4;
    return 0;
}

std::vector<NalUnit> split_nals(const uint8_t* data, size_t size) {
    std::vector<NalUnit> result;
    if (!data || size == 0) return result;
    size_t prefix = start_code_size(data, size, 0);
    if (prefix != 0) {
        size_t nal_start = prefix;
        while (nal_start < size) {
            size_t nal_end = nal_start;
            size_t next_prefix = 0;
            while (nal_end < size) {
                next_prefix = start_code_size(data, size, nal_end);
                if (next_prefix > 0) break;
                ++nal_end;
            }
            if (nal_end > nal_start) {
                result.push_back({std::vector<uint8_t>(data + nal_start, data + nal_end)});
            }
            nal_start = nal_end + next_prefix;
        }
        return result;
    }

    // Try AVCC 4-byte length prefix splitting
    size_t offset = 0;
    while (offset + 4 <= size) {
        const uint32_t length = (static_cast<uint32_t>(data[offset]) << 24) |
                                (static_cast<uint32_t>(data[offset + 1]) << 16) |
                                (static_cast<uint32_t>(data[offset + 2]) << 8) |
                                data[offset + 3];
        offset += 4;
        if (length == 0 || length > size - offset) {
            result.clear();
            result.push_back({std::vector<uint8_t>(data, data + size)});
            return result;
        }
        result.push_back({std::vector<uint8_t>(data + offset, data + offset + length)});
        offset += length;
    }
    if (offset != size || result.empty()) {
        result.clear();
        result.push_back({std::vector<uint8_t>(data, data + size)});
    }
    return result;
}

} // namespace

class MacosDecoder final : public IDecoder {
public:
    explicit MacosDecoder(std::string codec) : codec_(std::move(codec)) {}

    ~MacosDecoder() override {
        flush();
        std::lock_guard<std::mutex> lock(mutex_);
        destroy_session_locked();
        release_format_locked();
    }

    // 向 VideoToolbox 解码会话发送压缩数据包（将 NALU 封装为 AVCC 格式并构建 CMSampleBuffer）
    av_status send_packet(const uint8_t* data, size_t size, int64_t pts_us, bool) override {
        if (!data || size == 0) return AV_ERR_INVALID_ARG;

        VTDecompressionSessionRef session = nullptr;
        CMSampleBufferRef sample = nullptr;
        {
            std::lock_guard<std::mutex> lock(mutex_);
            const auto nals = split_nals(data, size);
            if (nals.empty()) return AV_ERR_INVALID_ARG;
            // 提取 SPS/PPS/VPS 并更新 CMVideoFormatDescription
            update_parameter_sets_locked(nals);
            if (!session_ && !create_session_locked()) return AV_ERR_RETRY;

            // 组装 AVCC 大端 4 字节长度前缀格式
            std::vector<uint8_t> avcc;
            for (const auto& nal : nals) {
                const uint32_t length = static_cast<uint32_t>(nal.bytes.size());
                avcc.push_back(static_cast<uint8_t>(length >> 24));
                avcc.push_back(static_cast<uint8_t>(length >> 16));
                avcc.push_back(static_cast<uint8_t>(length >> 8));
                avcc.push_back(static_cast<uint8_t>(length));
                avcc.insert(avcc.end(), nal.bytes.begin(), nal.bytes.end());
            }

            CMBlockBufferRef block = nullptr;
            if (CMBlockBufferCreateWithMemoryBlock(kCFAllocatorDefault, nullptr, avcc.size(),
                                                    kCFAllocatorDefault, nullptr, 0, avcc.size(), 0, &block) != kCMBlockBufferNoErr ||
                CMBlockBufferReplaceDataBytes(avcc.data(), block, 0, avcc.size()) != kCMBlockBufferNoErr) {
                if (block) CFRelease(block);
                return AV_ERR_OUT_OF_MEMORY;
            }
            const CMTime timestamp = CMTimeMake(pts_us, 1000000);
            CMSampleTimingInfo timing{CMTimeMake(1, 30), timestamp, timestamp};
            const size_t sample_size = avcc.size();
            const OSStatus sample_status = CMSampleBufferCreateReady(kCFAllocatorDefault, block, format_desc_, 1,
                                                                      1, &timing, 1, &sample_size, &sample);
            CFRelease(block);
            if (sample_status != noErr || !sample) return AV_ERR_INTERNAL;

            session = session_;
            CFRetain(session);
        }

        // 调用 VTDecompressionSessionDecodeFrame 异步硬解
        VTDecodeInfoFlags flags = 0;
        const OSStatus status = VTDecompressionSessionDecodeFrame(session, sample,
                                                                   kVTDecodeFrame_EnableAsynchronousDecompression,
                                                                   nullptr, &flags);
        CFRelease(sample);
        CFRelease(session);
        if (status != noErr) return AV_ERR_INFERENCE_FAILED;
        return AV_OK;
    }

    av_status receive_frame(av_frame_desc* out_frame) override {
        if (!out_frame) return AV_ERR_INVALID_ARG;
        std::lock_guard<std::mutex> lock(mutex_);
        if (pending_.empty()) return AV_ERR_RETRY;
        PendingFrame pending = pending_.front();
        pending_.pop_front();

        *out_frame = {};
        out_frame->size = sizeof(av_frame_desc);
        out_frame->api_version = AV_ALGO_API_VERSION;
        out_frame->frame_id = next_frame_id_++;
        out_frame->pts_ns = pending.pts_us * 1000;
        out_frame->wall_time_ns = pending.wall_time_ns;
        out_frame->platform_tag = 0x4D41434F;
        out_frame->opaque = pending.buffer;
        out_frame->opaque_kind = AV_OPAQUE_CVPIXELBUFFER;
        out_frame->memory_type = AV_MEM_PLATFORM_SURFACE;
        out_frame->pixel_format = AV_PIX_NV12;
        out_frame->layout = AV_LAYOUT_PLATFORM_NATIVE;
        out_frame->width = static_cast<uint32_t>(CVPixelBufferGetWidth(pending.buffer));
        out_frame->height = static_cast<uint32_t>(CVPixelBufferGetHeight(pending.buffer));
        out_frame->alloc_width = out_frame->width;
        out_frame->alloc_height = out_frame->height;
        const bool is_planar = CVPixelBufferIsPlanar(pending.buffer);
        out_frame->plane_count = is_planar ? CVPixelBufferGetPlaneCount(pending.buffer) : 1;
        for (uint32_t plane = 0; plane < out_frame->plane_count && plane < 4; ++plane) {
            out_frame->stride[plane] = static_cast<int32_t>(is_planar
                ? CVPixelBufferGetBytesPerRowOfPlane(pending.buffer, plane)
                : CVPixelBufferGetBytesPerRow(pending.buffer));
        }
        out_frame->color_primaries = color_info_.primaries;
        out_frame->color_transfer = color_info_.transfer;
        out_frame->color_matrix = color_info_.matrix;
        out_frame->color_range = color_info_.range;
        out_frame->time_synced = 0;
        return AV_OK;
    }

    void flush() override {
        VTDecompressionSessionRef session = nullptr;
        {
            std::lock_guard<std::mutex> lock(mutex_);
            session = session_;
            if (session) CFRetain(session);
        }
        if (session) {
            VTDecompressionSessionFinishDelayedFrames(session);
            VTDecompressionSessionWaitForAsynchronousFrames(session);
            CFRelease(session);
        }
        std::lock_guard<std::mutex> lock(mutex_);
        clear_pending_locked();
    }

    void reset() override {
        flush();
        std::lock_guard<std::mutex> lock(mutex_);
        destroy_session_locked();
        release_format_locked();
        sps_.clear();
        pps_.clear();
        vps_.clear();
        color_info_ = {};
    }

private:
    struct PendingFrame {
        CVPixelBufferRef buffer = nullptr;
        int64_t pts_us = 0;
        int64_t wall_time_ns = 0;
    };

    static void output_callback(void* refcon, void*, OSStatus status, VTDecodeInfoFlags,
                                CVImageBufferRef image_buffer, CMTime pts, CMTime) {
        if (status != noErr || !refcon || !image_buffer) return;
        auto* decoder = static_cast<MacosDecoder*>(refcon);
        CVPixelBufferRef pixel_buffer = static_cast<CVPixelBufferRef>(image_buffer);
        CVPixelBufferRetain(pixel_buffer);
        const CMTime scaled = CMTimeConvertScale(pts, 1000000, kCMTimeRoundingMethod_RoundHalfAwayFromZero);
        const int64_t pts_us = CMTIME_IS_VALID(scaled) ? scaled.value : 0;
        const int64_t wall_time_ns = std::chrono::duration_cast<std::chrono::nanoseconds>(
            std::chrono::system_clock::now().time_since_epoch()).count();
        {
            std::lock_guard<std::mutex> lock(decoder->mutex_);
            if (decoder->pending_.size() >= 32) {
                CVPixelBufferRelease(decoder->pending_.front().buffer);
                decoder->pending_.pop_front();
            }
            decoder->pending_.push_back({pixel_buffer, pts_us, wall_time_ns});
        }
    }

    void update_parameter_sets_locked(const std::vector<NalUnit>& nals) {
        bool params_changed = false;
        for (const auto& nal : nals) {
            if (nal.bytes.empty()) continue;
            const uint8_t type = codec_ == "H265" || codec_ == "HEVC"
                ? static_cast<uint8_t>((nal.bytes[0] >> 1) & 0x3F)
                : static_cast<uint8_t>(nal.bytes[0] & 0x1F);
            if (codec_ == "H265" || codec_ == "HEVC") {
                if (type == 32 && vps_ != nal.bytes) {
                    vps_ = nal.bytes;
                    params_changed = true;
                } else if (type == 33 && sps_ != nal.bytes) {
                    sps_ = nal.bytes;
                    color_info_ = core::ColorVUIParser::parse_h265_sps(sps_.data(), sps_.size());
                    params_changed = true;
                } else if (type == 34 && pps_ != nal.bytes) {
                    pps_ = nal.bytes;
                    params_changed = true;
                }
            } else {
                if (type == 7 && sps_ != nal.bytes) {
                    sps_ = nal.bytes;
                    color_info_ = core::ColorVUIParser::parse_h264_sps(sps_.data(), sps_.size());
                    params_changed = true;
                } else if (type == 8 && pps_ != nal.bytes) {
                    pps_ = nal.bytes;
                    params_changed = true;
                }
            }
        }
        if (params_changed && session_) {
            destroy_session_locked();
            release_format_locked();
        }
    }

    bool create_session_locked() {
        if (sps_.empty() || pps_.empty() || (codec_ == "H265" || codec_ == "HEVC") && vps_.empty()) return false;
        release_format_locked();
        const uint8_t* parameter_sets[3] = {vps_.data(), sps_.data(), pps_.data()};
        size_t parameter_sizes[3] = {vps_.size(), sps_.size(), pps_.size()};
        OSStatus status = noErr;
        if (codec_ == "H265" || codec_ == "HEVC") {
            status = CMVideoFormatDescriptionCreateFromHEVCParameterSets(
                kCFAllocatorDefault, 3, parameter_sets, parameter_sizes, 4, nullptr, &format_desc_);
        } else {
            const uint8_t* h264_sets[2] = {sps_.data(), pps_.data()};
            size_t h264_sizes[2] = {sps_.size(), pps_.size()};
            status = CMVideoFormatDescriptionCreateFromH264ParameterSets(
                kCFAllocatorDefault, 2, h264_sets, h264_sizes, 4, &format_desc_);
        }
        if (status != noErr || !format_desc_) return false;

        VTDecompressionOutputCallbackRecord callback_record{};
        callback_record.decompressionOutputCallback = output_callback;
        callback_record.decompressionOutputRefCon = this;
        NSDictionary* attributes = @{
            (id)kCVPixelBufferPixelFormatTypeKey: @(kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange),
            (id)kCVPixelBufferOpenGLCompatibilityKey: @NO
        };
        status = VTDecompressionSessionCreate(kCFAllocatorDefault, format_desc_, nullptr,
                                               (__bridge CFDictionaryRef)attributes,
                                               &callback_record, &session_);
        if (status != noErr || !session_) {
            release_format_locked();
            return false;
        }
        return true;
    }

    void clear_pending_locked() {
        for (auto& frame : pending_) CVPixelBufferRelease(frame.buffer);
        pending_.clear();
    }

    void destroy_session_locked() {
        if (!session_) return;
        VTDecompressionSessionInvalidate(session_);
        CFRelease(session_);
        session_ = nullptr;
    }

    void release_format_locked() {
        if (format_desc_) {
            CFRelease(format_desc_);
            format_desc_ = nullptr;
        }
    }

    std::string codec_;
    std::mutex mutex_;
    VTDecompressionSessionRef session_ = nullptr;
    CMVideoFormatDescriptionRef format_desc_ = nullptr;
    std::vector<uint8_t> vps_;
    core::ColorVUIInfo color_info_;
    std::vector<uint8_t> sps_;
    std::vector<uint8_t> pps_;
    std::deque<PendingFrame> pending_;
    uint64_t next_frame_id_ = 1;
};

std::unique_ptr<IDecoder> MacosPlatformAdapter::create_decoder(const std::string& codec_type) {
    return std::make_unique<MacosDecoder>(codec_type);
}

} // namespace aivision::platform
