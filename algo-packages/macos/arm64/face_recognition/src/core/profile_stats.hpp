#pragma once

#include <cstdint>
#include <string>
#include <vector>
#include <chrono>
#include <sstream>
#include <iomanip>

#if ENABLE_PROFILING
#import <os/signpost.h>
#endif

namespace face_recognition {

/**
 * @brief 帧级分阶段耗时与关键计数指标 (毫秒 / 整数计数)
 */
struct FrameProfileRecord {
    uint64_t frame_id = 0;

    // 分阶段耗时 (ms)
    double nv12_conversion_ms = 0.0;
    double letterbox_ms = 0.0;
    double scrfd_infer_ms = 0.0;
    double scrfd_copy_ms = 0.0;
    double decode_nms_ms = 0.0;
    double tracker_quality_ms = 0.0;
    double alignment_ms = 0.0;
    double glintr_infer_ms = 0.0;
    double glintr_copy_ms = 0.0;
    double embedding_encode_ms = 0.0;
    double serialization_ms = 0.0;
    double total_ms = 0.0;

    // 帧级计数
    uint32_t detected_faces = 0;
    uint32_t tracks = 0;
    uint32_t embedding_calls = 0;
    uint32_t image_requests = 0;

    std::string to_json() const {
        std::ostringstream ss;
        ss << std::fixed << std::setprecision(4);
        ss << "{"
           << "\"frame_id\":" << frame_id << ","
           << "\"stages_ms\":{"
           << "\"nv12_conversion\":" << nv12_conversion_ms << ","
           << "\"letterbox\":" << letterbox_ms << ","
           << "\"scrfd_infer\":" << scrfd_infer_ms << ","
           << "\"scrfd_copy\":" << scrfd_copy_ms << ","
           << "\"decode_nms\":" << decode_nms_ms << ","
           << "\"tracker_quality\":" << tracker_quality_ms << ","
           << "\"alignment\":" << alignment_ms << ","
           << "\"glintr_infer\":" << glintr_infer_ms << ","
           << "\"glintr_copy\":" << glintr_copy_ms << ","
           << "\"embedding_encode\":" << embedding_encode_ms << ","
           << "\"serialization\":" << serialization_ms << ","
           << "\"total\":" << total_ms
           << "},"
           << "\"counts\":{"
           << "\"detected_faces\":" << detected_faces << ","
           << "\"tracks\":" << tracks << ","
           << "\"embedding_calls\":" << embedding_calls << ","
           << "\"image_requests\":" << image_requests
           << "}"
           << "}";
        return ss.str();
    }
};

#if ENABLE_PROFILING

inline thread_local FrameProfileRecord* g_active_profile_record = nullptr;

inline void set_active_profile_record(FrameProfileRecord* record) {
    g_active_profile_record = record;
}

inline FrameProfileRecord* get_active_profile_record() {
    return g_active_profile_record;
}

inline os_log_t get_signpost_log() {
    static os_log_t log = os_log_create("argus.face_recognition", "pipeline");
    return log;
}

#define ARGUS_SIGNPOST_BEGIN(name) \
    do { \
        if (__builtin_available(macOS 10.14, *)) { \
            os_signpost_interval_begin(face_recognition::get_signpost_log(), OS_SIGNPOST_ID_EXCLUSIVE, name); \
        } \
    } while (0)

#define ARGUS_SIGNPOST_END(name) \
    do { \
        if (__builtin_available(macOS 10.14, *)) { \
            os_signpost_interval_end(face_recognition::get_signpost_log(), OS_SIGNPOST_ID_EXCLUSIVE, name); \
        } \
    } while (0)

#else

inline void set_active_profile_record(FrameProfileRecord*) {}
inline FrameProfileRecord* get_active_profile_record() { return nullptr; }

#define ARGUS_SIGNPOST_BEGIN(name) do {} while (0)
#define ARGUS_SIGNPOST_END(name) do {} while (0)

#endif

} // namespace face_recognition
