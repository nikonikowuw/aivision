#include "argus/algo.h"
#include "argus/utils/profiler.hpp"
#include "../preprocess/preprocessor.hpp"
#include "core/profile_stats.hpp"
#include <iostream>
#include <fstream>
#include <vector>
#include <string>
#include <chrono>
#include <cmath>
#include <cstring>
#include <algorithm>
#include <filesystem>
#include <dlfcn.h>
#import <CoreVideo/CoreVideo.h>
#import <Accelerate/Accelerate.h>
#import <Foundation/Foundation.h>
#import <mach/mach.h>
#import <IOSurface/IOSurface.h>
#define STB_IMAGE_IMPLEMENTATION
#include "stb_image.h"
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
#define STB_IMAGE_WRITE_IMPLEMENTATION
#include "stb_image_write.h"
#pragma clang diagnostic pop

namespace {

struct DetectedPersonInfo {
    int64_t track_id = 0;
    float person_bbox[4] = {0}; // x, y, w, h
    bool has_face = false;
    float face_bbox[4] = {0}; // x, y, w, h
    std::vector<std::pair<float, float>> landmarks; // 5 landmarks (x, y)
};

std::vector<DetectedPersonInfo> parse_recognition_persons(const std::string& json_str) {
    std::vector<DetectedPersonInfo> list;

    auto extract_float_after = [&](size_t search_pos, std::string_view key, float& out_val) -> size_t {
        size_t kp = json_str.find(key, search_pos);
        if (kp == std::string::npos) return std::string::npos;
        size_t colon = json_str.find(':', kp);
        if (colon == std::string::npos) return std::string::npos;
        size_t start = colon + 1;
        while (start < json_str.size() && (json_str[start] == ' ' || json_str[start] == '\t' || json_str[start] == '\n' || json_str[start] == '\r')) start++;
        size_t end = start;
        while (end < json_str.size() && ((json_str[end] >= '0' && json_str[end] <= '9') || json_str[end] == '.' || json_str[end] == '-')) end++;
        if (end > start) {
            try {
                out_val = std::stof(json_str.substr(start, end - start));
                return end;
            } catch (...) {}
        }
        return std::string::npos;
    };

    auto extract_array_4 = [&](size_t search_pos, std::string_view key, float arr[4]) -> size_t {
        size_t kp = json_str.find(key, search_pos);
        if (kp == std::string::npos) return std::string::npos;
        size_t open_bracket = json_str.find('[', kp);
        if (open_bracket == std::string::npos) return std::string::npos;
        size_t close_bracket = json_str.find(']', open_bracket);
        if (close_bracket == std::string::npos) return std::string::npos;

        std::string inner = json_str.substr(open_bracket + 1, close_bracket - open_bracket - 1);
        std::stringstream ss(inner);
        std::string item;
        int idx = 0;
        while (std::getline(ss, item, ',') && idx < 4) {
            try {
                arr[idx++] = std::stof(item);
            } catch (...) {}
        }
        return close_bracket;
    };

    size_t pos = 0;
    size_t person_start = json_str.find("\"track_id\"", pos);
    while (person_start != std::string::npos) {
        DetectedPersonInfo p{};
        float tid = 0;
        extract_float_after(person_start - 1, "\"track_id\"", tid);
        p.track_id = static_cast<int64_t>(tid);

        extract_array_4(person_start, "\"bbox\"", p.person_bbox);

        size_t next_track = json_str.find("\"track_id\"", person_start + 10);
        size_t face_pos = json_str.find("\"face\":", person_start);

        if (face_pos != std::string::npos && (next_track == std::string::npos || face_pos < next_track)) {
            size_t open_brace = json_str.find('{', face_pos);
            size_t null_pos = json_str.find("null", face_pos);
            if (open_brace != std::string::npos && (null_pos == std::string::npos || open_brace < null_pos) && (next_track == std::string::npos || open_brace < next_track)) {
                p.has_face = true;
                extract_array_4(open_brace, "\"bbox\"", p.face_bbox);

                size_t lm_pos = json_str.find("\"landmarks\":", open_brace);
                if (lm_pos != std::string::npos && (next_track == std::string::npos || lm_pos < next_track)) {
                    size_t lm_start = json_str.find('[', lm_pos);
                    size_t pair_start = json_str.find('[', lm_start + 1);
                    while (pair_start != std::string::npos && (next_track == std::string::npos || pair_start < next_track) && p.landmarks.size() < 5) {
                        size_t pair_end = json_str.find(']', pair_start);
                        if (pair_end == std::string::npos) break;
                        std::string sub = json_str.substr(pair_start + 1, pair_end - pair_start - 1);
                        size_t comma = sub.find(',');
                        if (comma != std::string::npos) {
                            try {
                                float lx = std::stof(sub.substr(0, comma));
                                float ly = std::stof(sub.substr(comma + 1));
                                p.landmarks.emplace_back(lx, ly);
                            } catch (...) {}
                        }
                        pair_start = json_str.find('[', pair_end + 1);
                        if (pair_start != std::string::npos && json_str[pair_start - 1] == ']') break;
                    }
                }
            }
        }

        list.push_back(p);
        pos = (next_track != std::string::npos) ? next_track : json_str.size();
        person_start = json_str.find("\"track_id\"", pos);
    }
    return list;
}

void draw_rect(uint8_t* img, int w, int h, int rx, int ry, int rw, int rh, uint8_t r, uint8_t g, uint8_t b, int thickness = 2) {
    for (int t = 0; t < thickness; ++t) {
        int x0 = std::clamp(rx - t, 0, w - 1);
        int x1 = std::clamp(rx + rw + t, 0, w - 1);
        int y0 = std::clamp(ry - t, 0, h - 1);
        int y1 = std::clamp(ry + rh + t, 0, h - 1);

        for (int x = x0; x <= x1; ++x) {
            img[(y0 * w + x) * 3 + 0] = r;
            img[(y0 * w + x) * 3 + 1] = g;
            img[(y0 * w + x) * 3 + 2] = b;
            img[(y1 * w + x) * 3 + 0] = r;
            img[(y1 * w + x) * 3 + 1] = g;
            img[(y1 * w + x) * 3 + 2] = b;
        }
        for (int y = y0; y <= y1; ++y) {
            img[(y * w + x0) * 3 + 0] = r;
            img[(y * w + x0) * 3 + 1] = g;
            img[(y * w + x0) * 3 + 2] = b;
            img[(y * w + x1) * 3 + 0] = r;
            img[(y * w + x1) * 3 + 1] = g;
            img[(y * w + x1) * 3 + 2] = b;
        }
    }
}

void draw_circle(uint8_t* img, int w, int h, int cx, int cy, int radius, uint8_t r, uint8_t g, uint8_t b) {
    for (int dy = -radius; dy <= radius; ++dy) {
        for (int dx = -radius; dx <= radius; ++dx) {
            if (dx * dx + dy * dy <= radius * radius) {
                int px = std::clamp(cx + dx, 0, w - 1);
                int py = std::clamp(cy + dy, 0, h - 1);
                img[(py * w + px) * 3 + 0] = r;
                img[(py * w + px) * 3 + 1] = g;
                img[(py * w + px) * 3 + 2] = b;
            }
        }
    }
}

std::vector<uint8_t> load_image_rgb(const std::string& path, int& out_w, int& out_h) {
    int channels = 0;
    uint8_t* stbi_data = stbi_load(path.c_str(), &out_w, &out_h, &channels, 3);
    if (stbi_data) {
        std::vector<uint8_t> rgb(stbi_data, stbi_data + (out_w * out_h * 3));
        stbi_image_free(stbi_data);
        return rgb;
    }

    // 若 stbi 不支持格式（如 WebP, HEIC, TIFF 等），使用 macOS CGImageSource 原生解码
    @autoreleasepool {
        NSString* ns_path = [NSString stringWithUTF8String:path.c_str()];
        NSData* data = [NSData dataWithContentsOfFile:ns_path];
        if (!data) return {};

        CGImageSourceRef source = CGImageSourceCreateWithData((__bridge CFDataRef)data, nullptr);
        if (!source) return {};

        CGImageRef image = CGImageSourceCreateImageAtIndex(source, 0, nullptr);
        CFRelease(source);
        if (!image) return {};

        size_t width = CGImageGetWidth(image);
        size_t height = CGImageGetHeight(image);
        out_w = static_cast<int>(width);
        out_h = static_cast<int>(height);

        std::vector<uint8_t> rgba(width * height * 4);
        CGColorSpaceRef color_space = CGColorSpaceCreateDeviceRGB();
        CGContextRef context = CGBitmapContextCreate(
            rgba.data(), width, height, 8, width * 4, color_space,
            static_cast<CGBitmapInfo>(static_cast<uint32_t>(kCGImageAlphaPremultipliedLast) | static_cast<uint32_t>(kCGBitmapByteOrder32Big))
        );
        CGColorSpaceRelease(color_space);
        if (!context) {
            CGImageRelease(image);
            return {};
        }

        CGContextDrawImage(context, CGRectMake(0, 0, width, height), image);
        CGContextRelease(context);
        CGImageRelease(image);

        std::vector<uint8_t> rgb(width * height * 3);
        for (size_t i = 0; i < width * height; ++i) {
            rgb[i * 3 + 0] = rgba[i * 4 + 0];
            rgb[i * 3 + 1] = rgba[i * 4 + 1];
            rgb[i * 3 + 2] = rgba[i * 4 + 2];
        }
        return rgb;
    }
}

bool render_results_to_image(const std::string& input_path, const std::string& output_path, const std::string& json_str) {
    int w = 0, h = 0;
    std::vector<uint8_t> rgb = load_image_rgb(input_path, w, h);
    if (rgb.empty()) return false;
    uint8_t* img = rgb.data();

    face_recognition::ImageBuffer orig_rgb;
    orig_rgb.width = static_cast<uint32_t>(w);
    orig_rgb.height = static_cast<uint32_t>(h);
    orig_rgb.channels = 3;
    orig_rgb.data = rgb;

    auto persons = parse_recognition_persons(json_str);
    int face_idx = 0;
    for (const auto& p : persons) {
        if (p.has_face) {
            int fx = static_cast<int>(p.face_bbox[0] * w);
            int fy = static_cast<int>(p.face_bbox[1] * h);
            int fw = static_cast<int>(p.face_bbox[2] * w);
            int fh = static_cast<int>(p.face_bbox[3] * h);
            draw_rect(img, w, h, fx, fy, fw, fh, 0, 255, 0, 2); // 绿色框表示人脸

            // 绘制 5 点关键点
            const uint8_t colors[5][3] = {
                {255, 0, 0},     // 0: 左眼 (红)
                {0, 0, 255},     // 1: 右眼 (蓝)
                {255, 255, 0},   // 2: 鼻尖 (黄)
                {255, 0, 255},   // 3: 左嘴角 (品红)
                {0, 255, 255}    // 4: 右嘴角 (青)
            };
            float lm10[10] = {0};
            for (size_t i = 0; i < p.landmarks.size() && i < 5; ++i) {
                float lx_f = p.landmarks[i].first * w;
                float ly_f = p.landmarks[i].second * h;
                lm10[i * 2 + 0] = lx_f;
                lm10[i * 2 + 1] = ly_f;
                int lx = static_cast<int>(lx_f);
                int ly = static_cast<int>(ly_f);
                draw_circle(img, w, h, lx, ly, 4, colors[i][0], colors[i][1], colors[i][2]);
            }

            // 对齐裁切并保存 112x112 人脸图供对比调试
            if (p.landmarks.size() == 5) {
                face_recognition::ImageBuffer face_112;
                std::string align_err;
                if (face_recognition::Preprocessor::align_face_112x112(orig_rgb, lm10, face_112, align_err)) {
                    std::string face_crop_name = (persons.size() == 1) ? "aligned_face_stream.jpg" : ("aligned_face_stream_track_" + std::to_string(p.track_id) + ".jpg");
                    stbi_write_jpg(face_crop_name.c_str(), 112, 112, 3, face_112.data.data(), 95);
                    std::cout << "[Visual] Saved 112x112 stream aligned face crop for track " << p.track_id << " to " << face_crop_name << std::endl;
                    if (persons.size() > 1 && face_idx == 0) {
                        stbi_write_jpg("aligned_face_stream.jpg", 112, 112, 3, face_112.data.data(), 95);
                    }
                }
            }
            face_idx++;
        }
    }

    int ret = stbi_write_jpg(output_path.c_str(), w, h, 3, img, 92);
    return ret != 0;
}

CVPixelBufferRef create_nv12_pixel_buffer(const uint8_t* rgb, int width, int height) {
    NSDictionary* options = @{
        (id)kCVPixelBufferCGImageCompatibilityKey: @YES,
        (id)kCVPixelBufferCGBitmapContextCompatibilityKey: @YES
    };
    CVPixelBufferRef pixel_buffer = nullptr;
    CVReturn status = CVPixelBufferCreate(
        kCFAllocatorDefault,
        width,
        height,
        kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange,
        (__bridge CFDictionaryRef)options,
        &pixel_buffer
    );
    if (status != kCVReturnSuccess || !pixel_buffer) return nullptr;

    CVPixelBufferLockBaseAddress(pixel_buffer, 0);
    uint8_t* y_dst = static_cast<uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(pixel_buffer, 0));
    uint8_t* uv_dst = static_cast<uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(pixel_buffer, 1));
    size_t y_stride = CVPixelBufferGetBytesPerRowOfPlane(pixel_buffer, 0);
    size_t uv_stride = CVPixelBufferGetBytesPerRowOfPlane(pixel_buffer, 1);

    // RGB to ARGB
    std::vector<uint8_t> argb(width * height * 4);
    for (int i = 0; i < width * height; ++i) {
        argb[i * 4 + 0] = 255;
        argb[i * 4 + 1] = rgb[i * 3 + 0];
        argb[i * 4 + 2] = rgb[i * 3 + 1];
        argb[i * 4 + 3] = rgb[i * 3 + 2];
    }

    vImage_Buffer src_argb = {
        .data = argb.data(),
        .height = static_cast<vImagePixelCount>(height),
        .width = static_cast<vImagePixelCount>(width),
        .rowBytes = static_cast<size_t>(width * 4)
    };
    vImage_Buffer dst_y = {
        .data = y_dst,
        .height = static_cast<vImagePixelCount>(height),
        .width = static_cast<vImagePixelCount>(width),
        .rowBytes = y_stride
    };
    vImage_Buffer dst_uv = {
        .data = uv_dst,
        .height = static_cast<vImagePixelCount>(height / 2),
        .width = static_cast<vImagePixelCount>(width / 2),
        .rowBytes = uv_stride
    };

    vImage_ARGBToYpCbCr matrix;
    vImage_YpCbCrPixelRange pixel_range = {16, 128, 235, 240, 255, 0, 255, 1};
    vImageConvert_ARGBToYpCbCr_GenerateConversion(
        kvImage_ARGBToYpCbCrMatrix_ITU_R_709_2,
        &pixel_range,
        &matrix,
        kvImageARGB8888,
        kvImage420Yp8_CbCr8,
        kvImageNoFlags
    );
    vImageConvert_ARGB8888To420Yp8_CbCr8(&src_argb, &dst_y, &dst_uv, &matrix, nullptr, kvImageNoFlags);

    CVPixelBufferUnlockBaseAddress(pixel_buffer, 0);
    return pixel_buffer;
}

inline double extract_json_double(std::string_view json, std::string_view key) {
    size_t pos = json.find(key);
    if (pos == std::string_view::npos) return 0.0;
    size_t colon = json.find(':', pos + key.size());
    if (colon == std::string_view::npos) return 0.0;
    size_t start = colon + 1;
    while (start < json.size() && (json[start] == ' ' || json[start] == '\t' || json[start] == '\n' || json[start] == '\r')) start++;
    size_t end = start;
    while (end < json.size() && ((json[end] >= '0' && json[end] <= '9') || json[end] == '.' || json[end] == '-' || json[end] == 'e' || json[end] == 'E' || json[end] == '+')) end++;
    if (end > start) {
        try {
            return std::stod(std::string(json.substr(start, end - start)));
        } catch (...) {}
    }
    return 0.0;
}

inline double get_rss_mb() {
    mach_task_basic_info info;
    mach_msg_type_number_t count = MACH_TASK_BASIC_INFO_COUNT;
    if (task_info(mach_task_self(), MACH_TASK_BASIC_INFO, (task_info_t)&info, &count) == KERN_SUCCESS) {
        return static_cast<double>(info.resident_size) / (1024.0 * 1024.0);
    }
    return 0.0;
}

inline double get_max_rss_mb() {
    mach_task_basic_info info;
    mach_msg_type_number_t count = MACH_TASK_BASIC_INFO_COUNT;
    if (task_info(mach_task_self(), MACH_TASK_BASIC_INFO, (task_info_t)&info, &count) == KERN_SUCCESS) {
        return static_cast<double>(info.resident_size_max) / (1024.0 * 1024.0);
    }
    return 0.0;
}

std::vector<uint8_t> scale_rgb(const std::vector<uint8_t>& src_rgb, int src_w, int src_h, int dst_w, int dst_h) {
    if (src_w == dst_w && src_h == dst_h) return src_rgb;

    float scale = std::min(static_cast<float>(dst_w) / static_cast<float>(src_w),
                           static_cast<float>(dst_h) / static_cast<float>(src_h));
    int scaled_w = std::max(2, static_cast<int>(std::round(src_w * scale)) & ~1);
    int scaled_h = std::max(2, static_cast<int>(std::round(src_h * scale)) & ~1);
    int pad_x = (dst_w - scaled_w) / 2;
    int pad_y = (dst_h - scaled_h) / 2;

    std::vector<uint8_t> src_argb(src_w * src_h * 4);
    for (int i = 0; i < src_w * src_h; ++i) {
        src_argb[i * 4 + 0] = 255;
        src_argb[i * 4 + 1] = src_rgb[i * 3 + 0];
        src_argb[i * 4 + 2] = src_rgb[i * 3 + 1];
        src_argb[i * 4 + 3] = src_rgb[i * 3 + 2];
    }
    std::vector<uint8_t> scaled_argb(scaled_w * scaled_h * 4);
    vImage_Buffer src_buf = { .data = src_argb.data(), .height = static_cast<vImagePixelCount>(src_h), .width = static_cast<vImagePixelCount>(src_w), .rowBytes = static_cast<size_t>(src_w * 4) };
    vImage_Buffer dst_buf = { .data = scaled_argb.data(), .height = static_cast<vImagePixelCount>(scaled_h), .width = static_cast<vImagePixelCount>(scaled_w), .rowBytes = static_cast<size_t>(scaled_w * 4) };
    vImageScale_ARGB8888(&src_buf, &dst_buf, nullptr, kvImageHighQualityResampling);

    std::vector<uint8_t> canvas(dst_w * dst_h * 3, 114);
    for (int y = 0; y < scaled_h; ++y) {
        int cy = pad_y + y;
        if (cy < 0 || cy >= dst_h) continue;
        for (int x = 0; x < scaled_w; ++x) {
            int cx = pad_x + x;
            if (cx < 0 || cx >= dst_w) continue;
            canvas[(cy * dst_w + cx) * 3 + 0] = scaled_argb[(y * scaled_w + x) * 4 + 1];
            canvas[(cy * dst_w + cx) * 3 + 1] = scaled_argb[(y * scaled_w + x) * 4 + 2];
            canvas[(cy * dst_w + cx) * 3 + 2] = scaled_argb[(y * scaled_w + x) * 4 + 3];
        }
    }
    return canvas;
}

std::vector<uint8_t> generate_synthetic_faces_rgb(const std::vector<uint8_t>& src_rgb, int src_w, int src_h, int target_w, int target_h, int num_faces) {
    std::vector<uint8_t> canvas(target_w * target_h * 3, 114);
    if (num_faces <= 0) {
        for (int y = 0; y < target_h; ++y) {
            for (int x = 0; x < target_w; ++x) {
                canvas[(y * target_w + x) * 3 + 0] = static_cast<uint8_t>((x * 255) / target_w);
                canvas[(y * target_w + x) * 3 + 1] = static_cast<uint8_t>((y * 255) / target_h);
                canvas[(y * target_w + x) * 3 + 2] = 128;
            }
        }
        return canvas;
    }

    int grid_cols = (num_faces <= 1) ? 1 : (num_faces <= 4) ? 2 : 4;
    int grid_rows = (num_faces <= 1) ? 1 : (num_faces <= 4) ? 2 : 4;
    int cell_w = target_w / grid_cols;
    int cell_h = target_h / grid_rows;

    float scale = std::min(static_cast<float>(cell_w) / static_cast<float>(src_w),
                           static_cast<float>(cell_h) / static_cast<float>(src_h));
    int patch_w = std::max(2, static_cast<int>(std::round(src_w * scale)) & ~1);
    int patch_h = std::max(2, static_cast<int>(std::round(src_h * scale)) & ~1);

    std::vector<uint8_t> patch = scale_rgb(src_rgb, src_w, src_h, patch_w, patch_h);

    int placed = 0;
    for (int r = 0; r < grid_rows && placed < num_faces; ++r) {
        for (int c = 0; c < grid_cols && placed < num_faces; ++c) {
            int ox = c * cell_w + (cell_w - patch_w) / 2;
            int oy = r * cell_h + (cell_h - patch_h) / 2;
            for (int py = 0; py < patch_h; ++py) {
                int cy = oy + py;
                if (cy < 0 || cy >= target_h) continue;
                for (int px = 0; px < patch_w; ++px) {
                    int cx = ox + px;
                    if (cx < 0 || cx >= target_w) continue;
                    for (int ch = 0; ch < 3; ++ch) {
                        canvas[(cy * target_w + cx) * 3 + ch] = patch[(py * patch_w + px) * 3 + ch];
                    }
                }
            }
            placed++;
        }
    }
    return canvas;
}

CVPixelBufferRef create_nv12_iosurface_pixel_buffer(const uint8_t* rgb, int width, int height) {
    NSDictionary* options = @{
        (id)kCVPixelBufferIOSurfacePropertiesKey: @{},
        (id)kCVPixelBufferCGImageCompatibilityKey: @YES,
        (id)kCVPixelBufferCGBitmapContextCompatibilityKey: @YES
    };
    CVPixelBufferRef pixel_buffer = nullptr;
    CVReturn status = CVPixelBufferCreate(
        kCFAllocatorDefault,
        width,
        height,
        kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange,
        (__bridge CFDictionaryRef)options,
        &pixel_buffer
    );
    if (status != kCVReturnSuccess || !pixel_buffer) return nullptr;

    CVPixelBufferLockBaseAddress(pixel_buffer, 0);
    uint8_t* y_dst = static_cast<uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(pixel_buffer, 0));
    uint8_t* uv_dst = static_cast<uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(pixel_buffer, 1));
    size_t y_stride = CVPixelBufferGetBytesPerRowOfPlane(pixel_buffer, 0);
    size_t uv_stride = CVPixelBufferGetBytesPerRowOfPlane(pixel_buffer, 1);

    std::vector<uint8_t> argb(width * height * 4);
    for (int i = 0; i < width * height; ++i) {
        argb[i * 4 + 0] = 255;
        argb[i * 4 + 1] = rgb[i * 3 + 0];
        argb[i * 4 + 2] = rgb[i * 3 + 1];
        argb[i * 4 + 3] = rgb[i * 3 + 2];
    }

    vImage_Buffer src_argb = {
        .data = argb.data(),
        .height = static_cast<vImagePixelCount>(height),
        .width = static_cast<vImagePixelCount>(width),
        .rowBytes = static_cast<size_t>(width * 4)
    };
    vImage_Buffer dst_y = {
        .data = y_dst,
        .height = static_cast<vImagePixelCount>(height),
        .width = static_cast<vImagePixelCount>(width),
        .rowBytes = y_stride
    };
    vImage_Buffer dst_uv = {
        .data = uv_dst,
        .height = static_cast<vImagePixelCount>(height / 2),
        .width = static_cast<vImagePixelCount>(width / 2),
        .rowBytes = uv_stride
    };

    vImage_ARGBToYpCbCr matrix;
    vImage_YpCbCrPixelRange pixel_range = {16, 128, 235, 240, 255, 0, 255, 1};
    vImageConvert_ARGBToYpCbCr_GenerateConversion(
        kvImage_ARGBToYpCbCrMatrix_ITU_R_709_2,
        &pixel_range,
        &matrix,
        kvImageARGB8888,
        kvImage420Yp8_CbCr8,
        kvImageNoFlags
    );
    vImageConvert_ARGB8888To420Yp8_CbCr8(&src_argb, &dst_y, &dst_uv, &matrix, nullptr, kvImageNoFlags);

    CVPixelBufferUnlockBaseAddress(pixel_buffer, 0);
    return pixel_buffer;
}

struct HostNV12Frame {
    std::vector<uint8_t> buffer;
    int32_t width = 0;
    int32_t height = 0;
    int32_t y_stride = 0;
    int32_t uv_stride = 0;
};

HostNV12Frame create_host_nv12(const uint8_t* rgb, int width, int height, int stride_mode) {
    HostNV12Frame frame;
    frame.width = width;
    frame.height = height;

    if (stride_mode == 0) {
        frame.y_stride = width;
        frame.uv_stride = width;
    } else if (stride_mode == 1) {
        frame.y_stride = (width + 63) & ~63;
        frame.uv_stride = (width + 63) & ~63;
    } else {
        frame.y_stride = width + 128;
        frame.uv_stride = width + 128;
    }

    size_t total_size = static_cast<size_t>(frame.y_stride) * height + static_cast<size_t>(frame.uv_stride) * (height / 2);
    frame.buffer.assign(total_size, 0);

    uint8_t* y_dst = frame.buffer.data();
    uint8_t* uv_dst = frame.buffer.data() + static_cast<size_t>(frame.y_stride) * height;

    std::vector<uint8_t> argb(width * height * 4);
    for (int i = 0; i < width * height; ++i) {
        argb[i * 4 + 0] = 255;
        argb[i * 4 + 1] = rgb[i * 3 + 0];
        argb[i * 4 + 2] = rgb[i * 3 + 1];
        argb[i * 4 + 3] = rgb[i * 3 + 2];
    }

    vImage_Buffer src_argb = {
        .data = argb.data(),
        .height = static_cast<vImagePixelCount>(height),
        .width = static_cast<vImagePixelCount>(width),
        .rowBytes = static_cast<size_t>(width * 4)
    };
    vImage_Buffer dst_y = {
        .data = y_dst,
        .height = static_cast<vImagePixelCount>(height),
        .width = static_cast<vImagePixelCount>(width),
        .rowBytes = static_cast<size_t>(frame.y_stride)
    };
    vImage_Buffer dst_uv = {
        .data = uv_dst,
        .height = static_cast<vImagePixelCount>(height / 2),
        .width = static_cast<vImagePixelCount>(width / 2),
        .rowBytes = static_cast<size_t>(frame.uv_stride)
    };

    vImage_ARGBToYpCbCr matrix;
    vImage_YpCbCrPixelRange pixel_range = {16, 128, 235, 240, 255, 0, 255, 1};
    vImageConvert_ARGBToYpCbCr_GenerateConversion(
        kvImage_ARGBToYpCbCrMatrix_ITU_R_709_2,
        &pixel_range,
        &matrix,
        kvImageARGB8888,
        kvImage420Yp8_CbCr8,
        kvImageNoFlags
    );
    vImageConvert_ARGB8888To420Yp8_CbCr8(&src_argb, &dst_y, &dst_uv, &matrix, nullptr, kvImageNoFlags);

    return frame;
}

struct ExtendedStats {
    double avg_ms = 0.0;
    double p50_ms = 0.0;
    double p95_ms = 0.0;
    double p99_ms = 0.0;
    double min_ms = 0.0;
    double max_ms = 0.0;
    double fps = 0.0;

    static ExtendedStats compute(std::vector<double> samples) {
        if (samples.empty()) return {};
        std::sort(samples.begin(), samples.end());
        size_t n = samples.size();
        double sum = std::accumulate(samples.begin(), samples.end(), 0.0);
        ExtendedStats st;
        st.avg_ms = sum / static_cast<double>(n);
        st.min_ms = samples.front();
        st.max_ms = samples.back();
        st.p50_ms = samples[n * 50 / 100];
        st.p95_ms = samples[std::min(n - 1, n * 95 / 100)];
        st.p99_ms = samples[std::min(n - 1, n * 99 / 100)];
        st.fps = (st.avg_ms > 0.0) ? (1000.0 / st.avg_ms) : 0.0;
        return st;
    }
};

struct ProfileReceiver {
    face_recognition::FrameProfileRecord last_record;
    bool has_record = false;
    std::string last_raw_json;
};

static void runner_algo_log(void* user, int level, const char* msg, uint32_t len) {
    if (!user || !msg || len == 0) return;
    auto* recv = static_cast<ProfileReceiver*>(user);
    std::string_view sv(msg, len);
    if (level == 0 && sv.find("\"stages_ms\"") != std::string_view::npos) {
        recv->last_raw_json.assign(msg, len);
        recv->last_record.frame_id = static_cast<uint64_t>(extract_json_double(sv, "\"frame_id\""));
        recv->last_record.nv12_conversion_ms = extract_json_double(sv, "\"nv12_conversion\"");
        recv->last_record.letterbox_ms = extract_json_double(sv, "\"letterbox\"");
        recv->last_record.scrfd_infer_ms = extract_json_double(sv, "\"scrfd_infer\"");
        recv->last_record.scrfd_copy_ms = extract_json_double(sv, "\"scrfd_copy\"");
        recv->last_record.decode_nms_ms = extract_json_double(sv, "\"decode_nms\"");
        recv->last_record.tracker_quality_ms = extract_json_double(sv, "\"tracker_quality\"");
        recv->last_record.alignment_ms = extract_json_double(sv, "\"alignment\"");
        recv->last_record.glintr_infer_ms = extract_json_double(sv, "\"glintr_infer\"");
        recv->last_record.glintr_copy_ms = extract_json_double(sv, "\"glintr_copy\"");
        recv->last_record.embedding_encode_ms = extract_json_double(sv, "\"embedding_encode\"");
        recv->last_record.serialization_ms = extract_json_double(sv, "\"serialization\"");
        recv->last_record.total_ms = extract_json_double(sv, "\"total\"");

        recv->last_record.detected_faces = static_cast<uint32_t>(extract_json_double(sv, "\"detected_faces\""));
        recv->last_record.tracks = static_cast<uint32_t>(extract_json_double(sv, "\"tracks\""));
        recv->last_record.embedding_calls = static_cast<uint32_t>(extract_json_double(sv, "\"embedding_calls\""));
        recv->last_record.image_requests = static_cast<uint32_t>(extract_json_double(sv, "\"image_requests\""));
        recv->has_record = true;
    }
}

struct BenchmarkConfig {
    std::string scenario = "fixture";
    std::string mode = "best_shot";
    int warmup = 30;
    int loops = 300;
    int width = 0;
    int height = 0;
    int num_faces = -1;
    std::string surface_type = "cvpixelbuffer";
    int stride_mode = 0;
    std::string jsonl_path;
};

int run_single_benchmark(
    const av_algo_abi* abi,
    av_algo_library lib,
    const std::vector<uint8_t>& base_rgb,
    int base_w, int base_h,
    const BenchmarkConfig& cfg,
    ProfileReceiver& prof_recv) {

    double rss_before_mb = get_rss_mb();

    int actual_w = (cfg.width > 0) ? cfg.width : base_w;
    int actual_h = (cfg.height > 0) ? cfg.height : base_h;

    std::vector<uint8_t> frame_rgb;
    if (cfg.num_faces >= 0) {
        frame_rgb = generate_synthetic_faces_rgb(base_rgb, base_w, base_h, actual_w, actual_h, cfg.num_faces);
    } else if (actual_w != base_w || actual_h != base_h) {
        frame_rgb = scale_rgb(base_rgb, base_w, base_h, actual_w, actual_h);
    } else {
        frame_rgb = base_rgb;
    }

    CVPixelBufferRef pixel_buffer = nullptr;
    HostNV12Frame host_frame;
    av_frame_desc frame{};
    frame.size = sizeof(frame);
    frame.api_version = AV_ALGO_API_VERSION;
    frame.width = actual_w;
    frame.height = actual_h;
    frame.pixel_format = AV_PIX_NV12;

    if (cfg.surface_type == "cvpixelbuffer_iosurface") {
        pixel_buffer = create_nv12_iosurface_pixel_buffer(frame_rgb.data(), actual_w, actual_h);
        if (!pixel_buffer) {
            std::cerr << "Failed to create IOSurface CVPixelBuffer\n";
            return 1;
        }
        frame.memory_type = AV_MEM_PLATFORM_SURFACE;
        frame.opaque_kind = AV_OPAQUE_CVPIXELBUFFER;
        frame.opaque = pixel_buffer;
    } else if (cfg.surface_type == "host_nv12") {
        host_frame = create_host_nv12(frame_rgb.data(), actual_w, actual_h, cfg.stride_mode);
        frame.memory_type = AV_MEM_HOST;
        frame.opaque_kind = AV_OPAQUE_NONE;
        frame.opaque = host_frame.buffer.data();
        frame.stride[0] = host_frame.y_stride;
        frame.stride[1] = host_frame.uv_stride;
    } else {
        pixel_buffer = create_nv12_pixel_buffer(frame_rgb.data(), actual_w, actual_h);
        if (!pixel_buffer) {
            std::cerr << "Failed to create CVPixelBuffer\n";
            return 1;
        }
        frame.memory_type = AV_MEM_PLATFORM_SURFACE;
        frame.opaque_kind = AV_OPAQUE_CVPIXELBUFFER;
        frame.opaque = pixel_buffer;
    }

    std::string config_json;
    if (cfg.mode == "all") {
        config_json = R"({"feature_mode":"all"})";
    } else if (cfg.mode == "detection_only") {
        config_json = R"({"feature_mode":"best_shot","max_recognitions_per_track":0,"quality_threshold":999.0})";
    } else {
        config_json = R"({"feature_mode":"best_shot","track_confirm_frames":2,"max_recognitions_per_track":3})";
    }

    av_algo_instance_args inst_args{};
    inst_args.size = sizeof(inst_args);
    inst_args.api_version = AV_ALGO_API_VERSION;
    inst_args.mode = AV_INSTANCE_NORMAL;
    inst_args.instance_id = "bench-inst";
    inst_args.instance_run_id = "bench-run";
    inst_args.config_json = config_json.c_str();
    inst_args.config_json_len = static_cast<uint32_t>(config_json.size());
    inst_args.on_result = [](const av_algo_result*, void*) {};

    av_algo_instance inst = nullptr;
    int st = abi->instance_create(lib, &inst_args, &inst);
    if (st != AV_OK) {
        std::cerr << "Instance create failed: " << st << std::endl;
        if (pixel_buffer) CVPixelBufferRelease(pixel_buffer);
        return 1;
    }

    std::cout << "Starting benchmark [" << cfg.scenario << "] (mode=" << cfg.mode
              << ", res=" << actual_w << "x" << actual_h
              << ", surf=" << cfg.surface_type
              << ", warmup=" << cfg.warmup << ", loops=" << cfg.loops << ")..." << std::endl;

    for (int i = 0; i < cfg.warmup; ++i) {
        frame.frame_id = i + 1;
        frame.pts_ns = 1000000000 + i * 40000000;
        abi->instance_process(inst, &frame);
    }
    abi->instance_flush(inst);

    double rss_start_mb = get_rss_mb();

    std::ofstream jsonl_file;
    if (!cfg.jsonl_path.empty()) {
        std::filesystem::path p(cfg.jsonl_path);
        if (p.has_parent_path()) {
            std::filesystem::create_directories(p.parent_path());
        }
        jsonl_file.open(cfg.jsonl_path);
    }

    std::vector<double> s_total, s_nv12, s_letterbox, s_scrfd_infer, s_scrfd_copy;
    std::vector<double> s_decode_nms, s_tracker_quality, s_alignment;
    std::vector<double> s_glintr_infer, s_glintr_copy, s_embedding_encode, s_serialization;
    std::vector<double> s_detected_faces, s_tracks, s_embedding_calls, s_image_requests;

    s_total.reserve(cfg.loops);

    for (int i = 0; i < cfg.loops; ++i) {
        prof_recv.has_record = false;
        frame.frame_id = i + 1 + cfg.warmup;
        frame.pts_ns = 1000000000 + frame.frame_id * 40000000;

        auto t0 = std::chrono::high_resolution_clock::now();
        int proc_st = abi->instance_process(inst, &frame);
        auto t1 = std::chrono::high_resolution_clock::now();
        double measured_total_ms = std::chrono::duration<double, std::milli>(t1 - t0).count();

        if (proc_st != AV_OK) {
            char err_buf[256] = {0};
            abi->last_error(inst, err_buf, sizeof(err_buf));
            std::cerr << "Instance process failed at loop " << i << ": " << err_buf << std::endl;
            break;
        }

        double cur_rss = get_rss_mb();
        s_total.push_back(measured_total_ms);

        if (prof_recv.has_record) {
            const auto& r = prof_recv.last_record;
            s_nv12.push_back(r.nv12_conversion_ms);
            s_letterbox.push_back(r.letterbox_ms);
            s_scrfd_infer.push_back(r.scrfd_infer_ms);
            s_scrfd_copy.push_back(r.scrfd_copy_ms);
            s_decode_nms.push_back(r.decode_nms_ms);
            s_tracker_quality.push_back(r.tracker_quality_ms);
            s_alignment.push_back(r.alignment_ms);
            s_glintr_infer.push_back(r.glintr_infer_ms);
            s_glintr_copy.push_back(r.glintr_copy_ms);
            s_embedding_encode.push_back(r.embedding_encode_ms);
            s_serialization.push_back(r.serialization_ms);
            s_detected_faces.push_back(r.detected_faces);
            s_tracks.push_back(r.tracks);
            s_embedding_calls.push_back(r.embedding_calls);
            s_image_requests.push_back(r.image_requests);

            if (jsonl_file.is_open()) {
                jsonl_file << "{\"iteration\":" << (i + 1)
                           << ",\"frame_id\":" << frame.frame_id
                           << ",\"pts_ns\":" << frame.pts_ns
                           << ",\"measured_total_ms\":" << measured_total_ms
                           << ",\"rss_mb\":" << cur_rss
                           << ",\"record\":" << r.to_json()
                           << "}\n";
            }
        } else {
            if (jsonl_file.is_open()) {
                jsonl_file << "{\"iteration\":" << (i + 1)
                           << ",\"frame_id\":" << frame.frame_id
                           << ",\"pts_ns\":" << frame.pts_ns
                           << ",\"measured_total_ms\":" << measured_total_ms
                           << ",\"rss_mb\":" << cur_rss
                           << "}\n";
            }
        }
    }

    if (jsonl_file.is_open()) {
        jsonl_file.close();
    }

    double rss_peak_mb = get_max_rss_mb();
    abi->instance_destroy(inst);
    if (pixel_buffer) CVPixelBufferRelease(pixel_buffer);
    double rss_after_mb = get_rss_mb();

    auto total_stats = ExtendedStats::compute(s_total);
    auto nv12_stats = ExtendedStats::compute(s_nv12);
    auto lb_stats = ExtendedStats::compute(s_letterbox);
    auto scrfd_infer_stats = ExtendedStats::compute(s_scrfd_infer);
    auto scrfd_copy_stats = ExtendedStats::compute(s_scrfd_copy);
    auto decode_stats = ExtendedStats::compute(s_decode_nms);
    auto tq_stats = ExtendedStats::compute(s_tracker_quality);
    auto align_stats = ExtendedStats::compute(s_alignment);
    auto glintr_infer_stats = ExtendedStats::compute(s_glintr_infer);
    auto glintr_copy_stats = ExtendedStats::compute(s_glintr_copy);
    auto encode_stats = ExtendedStats::compute(s_embedding_encode);
    auto ser_stats = ExtendedStats::compute(s_serialization);

    auto df_stats = ExtendedStats::compute(s_detected_faces);
    auto tr_stats = ExtendedStats::compute(s_tracks);
    auto emb_stats = ExtendedStats::compute(s_embedding_calls);
    auto req_stats = ExtendedStats::compute(s_image_requests);

    std::cout << "\n================================================================================\n"
              << "Benchmark Report: " << cfg.scenario << "\n"
              << "  Resolution:     " << actual_w << "x" << actual_h << "\n"
              << "  Feature Mode:   " << cfg.mode << "\n"
              << "  Surface Type:   " << cfg.surface_type << (cfg.surface_type == "host_nv12" ? (cfg.stride_mode == 0 ? " (packed)" : cfg.stride_mode == 1 ? " (aligned64)" : " (padded128)") : "") << "\n"
              << "  Synthetic Faces:" << (cfg.num_faces >= 0 ? std::to_string(cfg.num_faces) : "image direct") << "\n"
              << "  Warmup / Loops: " << cfg.warmup << " / " << cfg.loops << "\n"
              << "--------------------------------------------------------------------------------\n"
              << "  Memory RSS (MB):\n"
              << "    Before Init:  " << rss_before_mb << " MB\n"
              << "    After Warmup: " << rss_start_mb << " MB\n"
              << "    Peak:         " << rss_peak_mb << " MB\n"
              << "    After Close:  " << rss_after_mb << " MB\n"
              << "--------------------------------------------------------------------------------\n"
              << "  Total Latency:  Avg " << total_stats.avg_ms << " ms | P50 " << total_stats.p50_ms
              << " ms | P95 " << total_stats.p95_ms << " ms | P99 " << total_stats.p99_ms
              << " ms | Max " << total_stats.max_ms << " ms | " << total_stats.fps << " FPS\n";

    if (!s_scrfd_infer.empty()) {
        std::cout << "  Stage Latencies (ms):\n"
                  << "    NV12 Conversion: Avg " << nv12_stats.avg_ms << " | P50 " << nv12_stats.p50_ms << " | P95 " << nv12_stats.p95_ms << " | P99 " << nv12_stats.p99_ms << "\n"
                  << "    Letterbox (384): Avg " << lb_stats.avg_ms << " | P50 " << lb_stats.p50_ms << " | P95 " << lb_stats.p95_ms << " | P99 " << lb_stats.p99_ms << "\n"
                  << "    SCRFD Inference: Avg " << scrfd_infer_stats.avg_ms << " | P50 " << scrfd_infer_stats.p50_ms << " | P95 " << scrfd_infer_stats.p95_ms << " | P99 " << scrfd_infer_stats.p99_ms << "\n"
                  << "    SCRFD Copy (9H): Avg " << scrfd_copy_stats.avg_ms << " | P50 " << scrfd_copy_stats.p50_ms << " | P95 " << scrfd_copy_stats.p95_ms << " | P99 " << scrfd_copy_stats.p99_ms << "\n"
                  << "    Decode & NMS:    Avg " << decode_stats.avg_ms << " | P50 " << decode_stats.p50_ms << " | P95 " << decode_stats.p95_ms << " | P99 " << decode_stats.p99_ms << "\n"
                  << "    Tracker/Quality: Avg " << tq_stats.avg_ms << " | P50 " << tq_stats.p50_ms << " | P95 " << tq_stats.p95_ms << " | P99 " << tq_stats.p99_ms << "\n"
                  << "    Face Alignment:  Avg " << align_stats.avg_ms << " | P50 " << align_stats.p50_ms << " | P95 " << align_stats.p95_ms << " | P99 " << align_stats.p99_ms << "\n"
                  << "    GLINTR Inference:Avg " << glintr_infer_stats.avg_ms << " | P50 " << glintr_infer_stats.p50_ms << " | P95 " << glintr_infer_stats.p95_ms << " | P99 " << glintr_infer_stats.p99_ms << "\n"
                  << "    GLINTR Copy:     Avg " << glintr_copy_stats.avg_ms << " | P50 " << glintr_copy_stats.p50_ms << " | P95 " << glintr_copy_stats.p95_ms << " | P99 " << glintr_copy_stats.p99_ms << "\n"
                  << "    Embedding Encode:Avg " << encode_stats.avg_ms << " | P50 " << encode_stats.p50_ms << " | P95 " << encode_stats.p95_ms << " | P99 " << encode_stats.p99_ms << "\n"
                  << "    Serialization:   Avg " << ser_stats.avg_ms << " | P50 " << ser_stats.p50_ms << " | P95 " << ser_stats.p50_ms << " | P99 " << ser_stats.p99_ms << "\n"
                  << "  Frame Counts (Avg / Max):\n"
                  << "    Detected Faces:  " << df_stats.avg_ms << " / " << df_stats.max_ms << "\n"
                  << "    Track Count:     " << tr_stats.avg_ms << " / " << tr_stats.max_ms << "\n"
                  << "    Embedding Calls: " << emb_stats.avg_ms << " / " << emb_stats.max_ms << "\n"
                  << "    Image Requests:  " << req_stats.avg_ms << " / " << req_stats.max_ms << "\n";
    }
    std::cout << "================================================================================\n" << std::endl;

    if (!cfg.jsonl_path.empty()) {
        std::string summary_path = cfg.jsonl_path + ".summary.json";
        std::ofstream sum_file(summary_path);
        if (sum_file.is_open()) {
            sum_file << "{\n"
                     << "  \"scenario\": \"" << cfg.scenario << "\",\n"
                     << "  \"resolution\": \"" << actual_w << "x" << actual_h << "\",\n"
                     << "  \"mode\": \"" << cfg.mode << "\",\n"
                     << "  \"surface_type\": \"" << cfg.surface_type << "\",\n"
                     << "  \"stride_mode\": " << cfg.stride_mode << ",\n"
                     << "  \"num_faces\": " << cfg.num_faces << ",\n"
                     << "  \"warmup\": " << cfg.warmup << ",\n"
                     << "  \"loops\": " << cfg.loops << ",\n"
                     << "  \"rss\": {\n"
                     << "    \"before_init_mb\": " << rss_before_mb << ",\n"
                     << "    \"after_warmup_mb\": " << rss_start_mb << ",\n"
                     << "    \"peak_mb\": " << rss_peak_mb << ",\n"
                     << "    \"after_close_mb\": " << rss_after_mb << "\n"
                     << "  },\n"
                     << "  \"total_ms\": {\n"
                     << "    \"avg\": " << total_stats.avg_ms << ",\n"
                     << "    \"p50\": " << total_stats.p50_ms << ",\n"
                     << "    \"p95\": " << total_stats.p95_ms << ",\n"
                     << "    \"p99\": " << total_stats.p99_ms << ",\n"
                     << "    \"min\": " << total_stats.min_ms << ",\n"
                     << "    \"max\": " << total_stats.max_ms << ",\n"
                     << "    \"fps\": " << total_stats.fps << "\n"
                     << "  },\n"
                     << "  \"stages_avg_ms\": {\n"
                     << "    \"nv12_conversion\": " << nv12_stats.avg_ms << ",\n"
                     << "    \"letterbox\": " << lb_stats.avg_ms << ",\n"
                     << "    \"scrfd_infer\": " << scrfd_infer_stats.avg_ms << ",\n"
                     << "    \"scrfd_copy\": " << scrfd_copy_stats.avg_ms << ",\n"
                     << "    \"decode_nms\": " << decode_stats.avg_ms << ",\n"
                     << "    \"tracker_quality\": " << tq_stats.avg_ms << ",\n"
                     << "    \"alignment\": " << align_stats.avg_ms << ",\n"
                     << "    \"glintr_infer\": " << glintr_infer_stats.avg_ms << ",\n"
                     << "    \"glintr_copy\": " << glintr_copy_stats.avg_ms << ",\n"
                     << "    \"embedding_encode\": " << encode_stats.avg_ms << ",\n"
                     << "    \"serialization\": " << ser_stats.avg_ms << "\n"
                     << "  },\n"
                     << "  \"stages_p50_ms\": {\n"
                     << "    \"nv12_conversion\": " << nv12_stats.p50_ms << ",\n"
                     << "    \"letterbox\": " << lb_stats.p50_ms << ",\n"
                     << "    \"scrfd_infer\": " << scrfd_infer_stats.p50_ms << ",\n"
                     << "    \"scrfd_copy\": " << scrfd_copy_stats.p50_ms << ",\n"
                     << "    \"decode_nms\": " << decode_stats.p50_ms << ",\n"
                     << "    \"tracker_quality\": " << tq_stats.p50_ms << ",\n"
                     << "    \"alignment\": " << align_stats.p50_ms << ",\n"
                     << "    \"glintr_infer\": " << glintr_infer_stats.p50_ms << ",\n"
                     << "    \"glintr_copy\": " << glintr_copy_stats.p50_ms << ",\n"
                     << "    \"embedding_encode\": " << encode_stats.p50_ms << ",\n"
                     << "    \"serialization\": " << ser_stats.p50_ms << "\n"
                     << "  },\n"
                     << "  \"stages_p95_ms\": {\n"
                     << "    \"nv12_conversion\": " << nv12_stats.p95_ms << ",\n"
                     << "    \"letterbox\": " << lb_stats.p95_ms << ",\n"
                     << "    \"scrfd_infer\": " << scrfd_infer_stats.p95_ms << ",\n"
                     << "    \"scrfd_copy\": " << scrfd_copy_stats.p95_ms << ",\n"
                     << "    \"decode_nms\": " << decode_stats.p95_ms << ",\n"
                     << "    \"tracker_quality\": " << tq_stats.p95_ms << ",\n"
                     << "    \"alignment\": " << align_stats.p95_ms << ",\n"
                     << "    \"glintr_infer\": " << glintr_infer_stats.p95_ms << ",\n"
                     << "    \"glintr_copy\": " << glintr_copy_stats.p95_ms << ",\n"
                     << "    \"embedding_encode\": " << encode_stats.p95_ms << ",\n"
                     << "    \"serialization\": " << ser_stats.p95_ms << "\n"
                     << "  },\n"
                     << "  \"stages_p99_ms\": {\n"
                     << "    \"nv12_conversion\": " << nv12_stats.p99_ms << ",\n"
                     << "    \"letterbox\": " << lb_stats.p99_ms << ",\n"
                     << "    \"scrfd_infer\": " << scrfd_infer_stats.p99_ms << ",\n"
                     << "    \"scrfd_copy\": " << scrfd_copy_stats.p99_ms << ",\n"
                     << "    \"decode_nms\": " << decode_stats.p99_ms << ",\n"
                     << "    \"tracker_quality\": " << tq_stats.p99_ms << ",\n"
                     << "    \"alignment\": " << align_stats.p99_ms << ",\n"
                     << "    \"glintr_infer\": " << glintr_infer_stats.p99_ms << ",\n"
                     << "    \"glintr_copy\": " << glintr_copy_stats.p99_ms << ",\n"
                     << "    \"embedding_encode\": " << encode_stats.p99_ms << ",\n"
                     << "    \"serialization\": " << ser_stats.p99_ms << "\n"
                     << "  },\n"
                     << "  \"counts\": {\n"
                     << "    \"detected_faces_avg\": " << df_stats.avg_ms << ",\n"
                     << "    \"tracks_avg\": " << tr_stats.avg_ms << ",\n"
                     << "    \"embedding_calls_avg\": " << emb_stats.avg_ms << ",\n"
                     << "    \"image_requests_avg\": " << req_stats.avg_ms << "\n"
                     << "  }\n"
                     << "}\n";
        }
    }
    return 0;
}

} // namespace

int main(int argc, char** argv) {
    std::string mode = "run";
    if (argc > 1) mode = argv[1];

    std::string package_root = ".";
    const char* env_root = std::getenv("FACE_RECOGNITION_PACKAGE_ROOT");
    if (env_root) package_root = env_root;

    const char* env_dylib = std::getenv("FACE_RECOGNITION_DYLIB");
    std::string dylib_path = env_dylib ? env_dylib : "";
    void* handle = nullptr;
    if (!dylib_path.empty()) {
        handle = dlopen(dylib_path.c_str(), RTLD_NOW | RTLD_LOCAL);
    }
    if (!handle) {
        dylib_path = package_root + "/lib/libface_recognition.dylib";
        handle = dlopen(dylib_path.c_str(), RTLD_NOW | RTLD_LOCAL);
    }
    if (!handle) {
        dylib_path = package_root + "/build/libface_recognition.dylib";
        handle = dlopen(dylib_path.c_str(), RTLD_NOW | RTLD_LOCAL);
    }
    if (!handle) {
        dylib_path = package_root + "/build_asan/libface_recognition.dylib";
        handle = dlopen(dylib_path.c_str(), RTLD_NOW | RTLD_LOCAL);
    }
    if (!handle) {
        std::cerr << "Failed to dlopen libface_recognition.dylib: " << dlerror() << std::endl;
        return 1;
    }

    auto get_abi = reinterpret_cast<av_algo_get_abi_fn>(dlsym(handle, AV_ALGO_GET_ABI_SYMBOL));
    if (!get_abi) {
        std::cerr << "Failed to get abi symbol: " << dlerror() << std::endl;
        return 1;
    }

    const av_algo_abi* abi = get_abi(AV_ALGO_API_VERSION);
    if (!abi) {
        std::cerr << "Failed to negotiate ABI" << std::endl;
        return 1;
    }

    ProfileReceiver profile_receiver;
    av_algo_library_args lib_args{};
    lib_args.size = sizeof(lib_args);
    lib_args.api_version = AV_ALGO_API_VERSION;
    lib_args.package_root = package_root.c_str();
    lib_args.platform_id = "macos-arm64-coreml";
    lib_args.log = runner_algo_log;
    lib_args.log_user = &profile_receiver;

    av_algo_library lib = nullptr;
    int st = abi->library_open(&lib_args, &lib);
    if (st != AV_OK) {
        char err[256] = {0};
        abi->last_error(nullptr, err, sizeof(err));
        std::cerr << "Library open failed: " << err << std::endl;
        return 1;
    }

    if (mode == "extract" || mode == "register") {
        auto extract_fn = reinterpret_cast<av_algo_extract_face_fn>(dlsym(handle, AV_ALGO_EXTRACT_FACE_SYMBOL));
        if (!extract_fn) {
            std::cerr << "Failed to find symbol av_algo_extract_face in dylib" << std::endl;
            abi->library_close(lib);
            dlclose(handle);
            return 1;
        }

        std::string target_img = (argc > 2 && argv[2] && argv[2][0] != '\0') ? argv[2] : (package_root + "/testimage.jpg");
        std::ifstream file(target_img, std::ios::binary);
        if (!file) {
            target_img = package_root + "/testimage.jpg";
            file.open(target_img, std::ios::binary);
        }
        if (!file) {
            target_img = "testimage.jpg";
            file.open(target_img, std::ios::binary);
        }
        if (!file) {
            std::cerr << "Cannot open image file for face registration: " << target_img << std::endl;
            abi->library_close(lib);
            dlclose(handle);
            return 1;
        }

        std::vector<uint8_t> image_bytes((std::istreambuf_iterator<char>(file)),
                                         std::istreambuf_iterator<char>());
        file.close();

        av_face_extract_input in{};
        in.size = sizeof(in);
        in.api_version = AV_ALGO_API_VERSION;
        in.image_bytes = image_bytes.data();
        in.image_bytes_len = static_cast<uint32_t>(image_bytes.size());
        in.min_detection_score = 0.50f;
        in.min_face_size = 40.0f;
        in.min_quality_score = 35.0f;

        av_face_extract_output out{};
        out.size = sizeof(out);
        out.api_version = AV_ALGO_API_VERSION;

        auto t0 = std::chrono::high_resolution_clock::now();
        int extract_st = extract_fn(lib, &in, &out);
        auto t1 = std::chrono::high_resolution_clock::now();
        double cost_ms = std::chrono::duration<double, std::milli>(t1 - t0).count();

        if (extract_st != AV_OK) {
            std::cerr << "av_algo_extract_face returned error code: " << extract_st << std::endl;
            abi->library_close(lib);
            dlclose(handle);
            return 1;
        }

        std::cout << "\n=== Face Registration / Extraction (640x640 Pipeline) ===\n"
                  << "Input Image:    " << target_img << " (" << image_bytes.size() << " bytes)\n"
                  << "Execution Time: " << cost_ms << " ms\n"
                  << "Status Code:    " << out.status_code << " ("
                  << (out.status_code == 0 ? "SUCCESS" :
                      out.status_code == 1 ? "NO_FACE" :
                      out.status_code == 2 ? "MULTI_FACE" :
                      out.status_code == 3 ? "QUALITY_LOW" :
                      out.status_code == 4 ? "DECODE_ERROR" :
                      out.status_code == 5 ? "IMAGE_TOO_LARGE" :
                      out.status_code == 6 ? "FACE_TOO_SMALL" : "INTERNAL_ERROR")
                  << ")\n"
                  << "Error Message:  " << (out.error_message[0] ? out.error_message : "none") << "\n";

        if (out.status_code == 0) {
            float l2_norm_sq = 0.0f;
            for (uint32_t i = 0; i < out.embedding_dim; ++i) {
                l2_norm_sq += out.embedding[i] * out.embedding[i];
            }
            float l2_norm = std::sqrt(l2_norm_sq);

            std::cout << "Detection Score:" << out.detection_score << "\n"
                      << "Quality Score:  " << out.quality_score << " / 100\n"
                      << "Face BBox (norm): [" << out.bbox[0] << ", " << out.bbox[1] << ", " << out.bbox[2] << ", " << out.bbox[3] << "]\n"
                      << "Embedding Dim:  " << out.embedding_dim << "\n"
                      << "Embedding L2:   " << l2_norm << "\n"
                      << "Sample Floats:  ["
                      << out.embedding[0] << ", " << out.embedding[1] << ", "
                      << out.embedding[2] << ", ..., " << out.embedding[511] << "]\n"
                      << "Aligned JPEG:   " << out.aligned_jpeg_len << " bytes\n";

            if (out.aligned_jpeg_len > 0) {
                std::string out_face = "aligned_face_register.jpg";
                std::ofstream ofs(out_face, std::ios::binary);
                if (ofs.write(reinterpret_cast<const char*>(out.aligned_jpeg_data), out.aligned_jpeg_len)) {
                    std::cout << "[Visual] Saved 112x112 registration aligned face crop to " << out_face << "\n";
                }
            }
        }
        std::cout << "==========================================================\n" << std::endl;

        abi->library_close(lib);
        dlclose(handle);
        return (out.status_code == 0) ? 0 : 1;
    }

    std::string test_img_path = (argc > 2 && argv[2] && argv[2][0] != '\0') ? argv[2] : (package_root + "/testimage.jpg");
    int width = 0, height = 0;
    std::vector<uint8_t> img_rgb = load_image_rgb(test_img_path, width, height);
    if (img_rgb.empty()) {
        test_img_path = package_root + "/testimage.jpg";
        img_rgb = load_image_rgb(test_img_path, width, height);
    }
    if (img_rgb.empty()) {
        test_img_path = "testimage.jpg";
        img_rgb = load_image_rgb(test_img_path, width, height);
    }
    if (img_rgb.empty()) {
        std::cerr << "Failed to load test image from " << test_img_path << std::endl;
        abi->library_close(lib);
        dlclose(handle);
        return 1;
    }

    if (mode == "benchmark") {
        std::string bench_scenario = "fixture";
        std::string bench_mode = "best_shot";
        int warmup = 30;
        int loops = 300;
        std::string surface_type = "cvpixelbuffer";
        int stride_mode = 0;
        int num_faces = -1;
        std::string output_dir = "";
        std::string custom_jsonl = "";

        for (int i = 2; i < argc; ++i) {
            std::string arg = argv[i];
            if (arg == "--scenario" && i + 1 < argc) bench_scenario = argv[++i];
            else if (arg == "--mode" && i + 1 < argc) bench_mode = argv[++i];
            else if (arg == "--warmup" && i + 1 < argc) warmup = std::stoi(argv[++i]);
            else if (arg == "--loops" && i + 1 < argc) loops = std::stoi(argv[++i]);
            else if (arg == "--surface" && i + 1 < argc) surface_type = argv[++i];
            else if (arg == "--stride-mode" && i + 1 < argc) {
                std::string sm = argv[++i];
                if (sm == "aligned64" || sm == "1") stride_mode = 1;
                else if (sm == "padded128" || sm == "2") stride_mode = 2;
                else stride_mode = 0;
            }
            else if (arg == "--faces" && i + 1 < argc) num_faces = std::stoi(argv[++i]);
            else if (arg == "--output-dir" && i + 1 < argc) output_dir = argv[++i];
            else if (arg == "--jsonl" && i + 1 < argc) custom_jsonl = argv[++i];
        }

        if (output_dir.empty()) {
            const char* env_out = std::getenv("BENCHMARK_OUTPUT_DIR");
            output_dir = env_out ? env_out : (package_root + "/benchmark_out");
        }

        auto make_jsonl_path = [&](const std::string& name) -> std::string {
            if (!custom_jsonl.empty()) return custom_jsonl;
            return output_dir + "/" + name + ".jsonl";
        };

        if (bench_scenario == "all") {
            std::vector<BenchmarkConfig> matrix = {
                {"fixture_best_shot", "best_shot", 30, 300, width, height, -1, "cvpixelbuffer", 0, make_jsonl_path("fixture_best_shot")},
                {"fixture_all", "all", 30, 300, width, height, -1, "cvpixelbuffer", 0, make_jsonl_path("fixture_all")},
                {"fixture_detection_only", "detection_only", 30, 300, width, height, -1, "cvpixelbuffer", 0, make_jsonl_path("fixture_detection_only")},

                {"1080p_best_shot", "best_shot", 30, 300, 1920, 1080, -1, "cvpixelbuffer", 0, make_jsonl_path("1080p_best_shot")},
                {"1080p_all", "all", 30, 300, 1920, 1080, -1, "cvpixelbuffer", 0, make_jsonl_path("1080p_all")},
                {"1080p_detection_only", "detection_only", 30, 300, 1920, 1080, -1, "cvpixelbuffer", 0, make_jsonl_path("1080p_detection_only")},

                {"4k_best_shot", "best_shot", 30, 300, 3840, 2160, -1, "cvpixelbuffer", 0, make_jsonl_path("4k_best_shot")},
                {"4k_all", "all", 30, 300, 3840, 2160, -1, "cvpixelbuffer", 0, make_jsonl_path("4k_all")},
                {"4k_detection_only", "detection_only", 30, 300, 3840, 2160, -1, "cvpixelbuffer", 0, make_jsonl_path("4k_detection_only")},

                {"stride_packed", "best_shot", 30, 300, 1920, 1080, -1, "host_nv12", 0, make_jsonl_path("stride_packed")},
                {"stride_aligned64", "best_shot", 30, 300, 1920, 1080, -1, "host_nv12", 1, make_jsonl_path("stride_aligned64")},
                {"stride_padded128", "best_shot", 30, 300, 1920, 1080, -1, "host_nv12", 2, make_jsonl_path("stride_padded128")},

                {"faces_0", "best_shot", 30, 300, 1920, 1080, 0, "cvpixelbuffer", 0, make_jsonl_path("faces_0")},
                {"faces_1", "best_shot", 30, 300, 1920, 1080, 1, "cvpixelbuffer", 0, make_jsonl_path("faces_1")},
                {"faces_4", "best_shot", 30, 300, 1920, 1080, 4, "cvpixelbuffer", 0, make_jsonl_path("faces_4")},
                {"faces_16", "best_shot", 30, 300, 1920, 1080, 16, "cvpixelbuffer", 0, make_jsonl_path("faces_16")},
                {"faces_16_all", "all", 30, 300, 1920, 1080, 16, "cvpixelbuffer", 0, make_jsonl_path("faces_16_all")},

                {"surface_iosurface", "best_shot", 30, 300, 1920, 1080, -1, "cvpixelbuffer_iosurface", 0, make_jsonl_path("surface_iosurface")},

                {"stability_1000", "best_shot", 60, 1000, 1920, 1080, -1, "cvpixelbuffer", 0, make_jsonl_path("stability_1000")}
            };

            for (const auto& item : matrix) {
                run_single_benchmark(abi, lib, img_rgb, width, height, item, profile_receiver);
            }
        } else {
            BenchmarkConfig single_cfg;
            single_cfg.scenario = bench_scenario;
            single_cfg.mode = bench_mode;
            single_cfg.warmup = warmup;
            single_cfg.loops = loops;
            single_cfg.surface_type = surface_type;
            single_cfg.stride_mode = stride_mode;
            single_cfg.num_faces = num_faces;
            if (bench_scenario == "1080p") {
                single_cfg.width = 1920; single_cfg.height = 1080;
            } else if (bench_scenario == "4k") {
                single_cfg.width = 3840; single_cfg.height = 2160;
            } else if (bench_scenario == "stability") {
                single_cfg.width = 1920; single_cfg.height = 1080;
                single_cfg.warmup = 60; single_cfg.loops = 1000;
            } else {
                single_cfg.width = width; single_cfg.height = height;
            }
            single_cfg.jsonl_path = make_jsonl_path(bench_scenario);
            run_single_benchmark(abi, lib, img_rgb, width, height, single_cfg, profile_receiver);
        }

        abi->library_close(lib);
        dlclose(handle);
        return 0;
    }

    CVPixelBufferRef pixel_buffer = create_nv12_pixel_buffer(img_rgb.data(), width, height);
    if (!pixel_buffer) {
        std::cerr << "Failed to create CVPixelBuffer" << std::endl;
        return 1;
    }

    av_algo_instance_args inst_args{};
    inst_args.size = sizeof(inst_args);
    inst_args.api_version = AV_ALGO_API_VERSION;
    inst_args.mode = (mode == "selftest") ? AV_INSTANCE_INSTALL_SELF_TEST : AV_INSTANCE_NORMAL;
    inst_args.instance_id = "inst-0";
    inst_args.instance_run_id = "run-0";
    std::string captured_result_json;
    if (mode == "benchmark") {
        inst_args.on_result = nullptr;
    } else {
        inst_args.result_user = &captured_result_json;
        inst_args.on_result = [](const av_algo_result* res, void* user) {
            if (res && res->json) {
                std::cout << ">>> RESULT (kind=" << res->kind << ", frame_id=" << res->frame_id << "):\n"
                          << res->json << "\n<<<" << std::endl;
                if (user) {
                    auto* p_json = static_cast<std::string*>(user);
                    p_json->assign(res->json, res->json_len);
                }
            }
        };
    }

    av_algo_instance inst = nullptr;
    st = abi->instance_create(lib, &inst_args, &inst);
    if (st != AV_OK) {
        std::cerr << "Instance create failed" << std::endl;
        return 1;
    }

    av_frame_desc frame{};
    frame.size = sizeof(frame);
    frame.api_version = AV_ALGO_API_VERSION;
    frame.frame_id = 1;
    frame.pts_ns = 1000000000;
    frame.width = width;
    frame.height = height;
    frame.pixel_format = AV_PIX_NV12;
    frame.memory_type = AV_MEM_PLATFORM_SURFACE;
    frame.opaque_kind = AV_OPAQUE_CVPIXELBUFFER;
    frame.opaque = pixel_buffer;

    if (mode == "run" || mode == "selftest") {
        // 对于正常运行模式，送两帧以测试轨迹确认 (track_confirm_frames) 与首优抓拍
        frame.frame_id = 1;
        frame.pts_ns = 1000000000;
        st = abi->instance_process(inst, &frame);
        if (st == AV_OK && mode == "run") {
            frame.frame_id = 2;
            frame.pts_ns = 1040000000;
            st = abi->instance_process(inst, &frame);
        }

        if (st != AV_OK) {
            char err[256] = {0};
            abi->last_error(inst, err, sizeof(err));
            std::cerr << "Process failed: " << err << std::endl;
        } else if (mode == "run" && !captured_result_json.empty()) {
            std::string out_img = "result.jpg";
            if (render_results_to_image(test_img_path, out_img, captured_result_json)) {
                std::cout << "[Visual] Successfully saved detection boxes and landmarks to " << out_img << std::endl;
            } else {
                std::cerr << "[Visual] Failed to save " << out_img << std::endl;
            }
        }
    }

    abi->instance_destroy(inst);
    CVPixelBufferRelease(pixel_buffer);
    abi->library_close(lib);
    dlclose(handle);

    return 0;
}
