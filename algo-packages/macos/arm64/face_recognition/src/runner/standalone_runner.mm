#include "argus/algo.h"
#include "argus/utils/profiler.hpp"
#include <iostream>
#include <fstream>
#include <vector>
#include <string>
#include <chrono>
#include <cmath>
#include <cstring>
#include <algorithm>
#include <dlfcn.h>
#import <CoreVideo/CoreVideo.h>
#import <Accelerate/Accelerate.h>
#import <Foundation/Foundation.h>
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
    size_t pos = 0;

    auto extract_float_after = [&](size_t search_pos, std::string_view key, float& out_val) -> size_t {
        size_t kp = json_str.find(key, search_pos);
        if (kp == std::string::npos) return std::string::npos;
        size_t colon = json_str.find(':', kp);
        if (colon == std::string::npos) return std::string::npos;
        size_t start = colon + 1;
        while (start < json_str.size() && (json_str[start] == ' ' || json_str[start] == '\t' || json_str[start] == '\n')) start++;
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

    size_t person_start = json_str.find("\"track_id\"", pos);
    while (person_start != std::string::npos) {
        DetectedPersonInfo p{};
        float tid = 0;
        extract_float_after(person_start - 1, "\"track_id\"", tid);
        p.track_id = static_cast<int64_t>(tid);

        extract_array_4(person_start, "\"bbox\"", p.person_bbox);

        // Check if there is "face": { ... } before next track_id
        size_t next_track = json_str.find("\"track_id\"", person_start + 10);
        size_t face_pos = json_str.find("\"face\":", person_start);

        if (face_pos != std::string::npos && (next_track == std::string::npos || face_pos < next_track)) {
            // Check if face is not null
            size_t open_brace = json_str.find('{', face_pos);
            size_t null_pos = json_str.find("null", face_pos);
            if (open_brace != std::string::npos && (null_pos == std::string::npos || open_brace < null_pos) && (next_track == std::string::npos || open_brace < next_track)) {
                p.has_face = true;
                extract_array_4(open_brace, "\"bbox\"", p.face_bbox);

                // landmarks
                size_t lm_pos = json_str.find("\"landmarks\":", open_brace);
                if (lm_pos != std::string::npos && (next_track == std::string::npos || lm_pos < next_track)) {
                    size_t lm_start = json_str.find('[', lm_pos);
                    size_t lm_end = json_str.find("],", lm_start);
                    if (lm_end == std::string::npos) lm_end = json_str.find("]\n", lm_start);
                    // find pairs [[x, y], [x, y], ...]
                    size_t pair_start = json_str.find('[', lm_start + 1);
                    while (pair_start != std::string::npos && pair_start < next_track && p.landmarks.size() < 5) {
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
        person_start = next_track;
    }

    return list;
}

void draw_line(uint8_t* img, int w, int h, int x1, int y1, int x2, int y2, uint8_t r, uint8_t g, uint8_t b, int thickness) {
    int dx = std::abs(x2 - x1);
    int dy = std::abs(y2 - y1);
    int sx = (x1 < x2) ? 1 : -1;
    int sy = (y1 < y2) ? 1 : -1;
    int err = dx - dy;

    while (true) {
        for (int ty = -thickness / 2; ty <= thickness / 2; ++ty) {
            for (int tx = -thickness / 2; tx <= thickness / 2; ++tx) {
                int px = x1 + tx;
                int py = y1 + ty;
                if (px >= 0 && px < w && py >= 0 && py < h) {
                    size_t idx = (py * w + px) * 3;
                    img[idx + 0] = r;
                    img[idx + 1] = g;
                    img[idx + 2] = b;
                }
            }
        }
        if (x1 == x2 && y1 == y2) break;
        int e2 = 2 * err;
        if (e2 > -dy) {
            err -= dy;
            x1 += sx;
        }
        if (e2 < dx) {
            err += dx;
            y1 += sy;
        }
    }
}

void draw_rect(uint8_t* img, int w, int h, int x, int y, int bw, int bh, uint8_t r, uint8_t g, uint8_t b, int thickness) {
    int x2 = std::min(w - 1, x + bw);
    int y2 = std::min(h - 1, y + bh);
    int x1 = std::max(0, x);
    int y1 = std::max(0, y);

    draw_line(img, w, h, x1, y1, x2, y1, r, g, b, thickness);
    draw_line(img, w, h, x2, y1, x2, y2, r, g, b, thickness);
    draw_line(img, w, h, x2, y2, x1, y2, r, g, b, thickness);
    draw_line(img, w, h, x1, y2, x1, y1, r, g, b, thickness);
}

void draw_circle(uint8_t* img, int w, int h, int cx, int cy, int radius, uint8_t r, uint8_t g, uint8_t b) {
    for (int y = -radius; y <= radius; ++y) {
        for (int x = -radius; x <= radius; ++x) {
            if (x * x + y * y <= radius * radius) {
                int px = cx + x;
                int py = cy + y;
                if (px >= 0 && px < w && py >= 0 && py < h) {
                    size_t idx = (py * w + px) * 3;
                    img[idx + 0] = r;
                    img[idx + 1] = g;
                    img[idx + 2] = b;
                }
            }
        }
    }
}

bool render_results_to_image(const std::string& input_path, const std::string& output_path, const std::string& result_json) {
    int w = 0, h = 0, c = 0;
    uint8_t* img = stbi_load(input_path.c_str(), &w, &h, &c, 3);
    if (!img) {
        std::cerr << "Failed to load " << input_path << " for rendering" << std::endl;
        return false;
    }

    auto persons = parse_recognition_persons(result_json);

    for (const auto& p : persons) {
        // 1. Draw Person Bounding Box in Green [0, 255, 0]
        int px = static_cast<int>(p.person_bbox[0] * w);
        int py = static_cast<int>(p.person_bbox[1] * h);
        int pw = static_cast<int>(p.person_bbox[2] * w);
        int ph = static_cast<int>(p.person_bbox[3] * h);
        draw_rect(img, w, h, px, py, pw, ph, 0, 255, 0, 3);

        // 2. Draw Face Bounding Box in Blue [0, 160, 255] if present
        if (p.has_face) {
            int fx = static_cast<int>(p.face_bbox[0] * w);
            int fy = static_cast<int>(p.face_bbox[1] * h);
            int fw = static_cast<int>(p.face_bbox[2] * w);
            int fh = static_cast<int>(p.face_bbox[3] * h);
            draw_rect(img, w, h, fx, fy, fw, fh, 0, 160, 255, 2);

            // 3. Draw 5 Facial Landmarks (Keypoints) in Red / Yellow / Cyan / Magenta / White
            // 0: left eye (red), 1: right eye (red), 2: nose (yellow), 3: left mouth (magenta), 4: right mouth (cyan)
            const uint8_t colors[5][3] = {
                {255, 0, 0},     // Left eye
                {255, 0, 0},     // Right eye
                {255, 255, 0},   // Nose
                {255, 0, 255},   // Left mouth corner
                {0, 255, 255}    // Right mouth corner
            };

            for (size_t i = 0; i < p.landmarks.size() && i < 5; ++i) {
                int lx = static_cast<int>(p.landmarks[i].first * w);
                int ly = static_cast<int>(p.landmarks[i].second * h);
                draw_circle(img, w, h, lx, ly, 4, colors[i][0], colors[i][1], colors[i][2]);
            }
        }
    }

    int ret = stbi_write_jpg(output_path.c_str(), w, h, 3, img, 92);
    stbi_image_free(img);
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

} // namespace

int main(int argc, char** argv) {
    std::string mode = "run";
    if (argc > 1) mode = argv[1];

    std::string dylib_path = "build/libface_recognition.dylib";
    void* handle = dlopen(dylib_path.c_str(), RTLD_NOW | RTLD_LOCAL);
    if (!handle) {
        dylib_path = "lib/libface_recognition.dylib";
        handle = dlopen(dylib_path.c_str(), RTLD_NOW | RTLD_LOCAL);
    }
    if (!handle) {
        std::cerr << "Failed to dlopen: " << dlerror() << std::endl;
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

    av_algo_library_args lib_args{};
    lib_args.size = sizeof(lib_args);
    lib_args.api_version = AV_ALGO_API_VERSION;
    lib_args.package_root = ".";
    lib_args.platform_id = "macos-arm64-coreml";

    av_algo_library lib = nullptr;
    int st = abi->library_open(&lib_args, &lib);
    if (st != AV_OK) {
        char err[256] = {0};
        abi->last_error(nullptr, err, sizeof(err));
        std::cerr << "Library open failed: " << err << std::endl;
        return 1;
    }

    int width = 0, height = 0, channels = 0;
    uint8_t* img_data = stbi_load("testimage.jpg", &width, &height, &channels, 3);
    if (!img_data) {
        std::cerr << "Failed to load testimage.jpg" << std::endl;
        return 1;
    }

    CVPixelBufferRef pixel_buffer = create_nv12_pixel_buffer(img_data, width, height);
    stbi_image_free(img_data);
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
    frame.width = width;
    frame.height = height;
    frame.pixel_format = AV_PIX_NV12;
    frame.opaque = pixel_buffer;

    if (mode == "run" || mode == "selftest") {
        // 对于正常运行模式，送两帧以测试轨迹确认 (track_confirm_frames) 与首优抓拍
        frame.frame_id = 1;
        st = abi->instance_process(inst, &frame);
        if (st == AV_OK && mode == "run") {
            frame.frame_id = 2;
            st = abi->instance_process(inst, &frame);
        }

        if (st != AV_OK) {
            char err[256] = {0};
            abi->last_error(inst, err, sizeof(err));
            std::cerr << "Process failed: " << err << std::endl;
        } else if (mode == "run" && !captured_result_json.empty()) {
            std::string out_img = "result.jpg";
            if (render_results_to_image("testimage.jpg", out_img, captured_result_json)) {
                std::cout << "[Visual] Successfully saved detection boxes and landmarks to " << out_img << std::endl;
            } else {
                std::cerr << "[Visual] Failed to save " << out_img << std::endl;
            }
        }
    } else if (mode == "benchmark") {
        std::cout << "Starting benchmark (10 warmup, 50 measured iterations)..." << std::endl;
        for (int i = 0; i < 10; ++i) {
            frame.frame_id = i + 1;
            abi->instance_process(inst, &frame);
        }

        std::vector<double> samples;
        for (int i = 0; i < 50; ++i) {
            frame.frame_id = i + 20;
            auto start = std::chrono::high_resolution_clock::now();
            abi->instance_process(inst, &frame);
            auto end = std::chrono::high_resolution_clock::now();
            double ms = std::chrono::duration<double, std::milli>(end - start).count();
            samples.push_back(ms);
        }

        auto stats = argus::utils::BenchmarkStats::compute(samples);
        std::cout << "\n=== Face Recognition Benchmark Results ===\n"
                  << "Loops: 50\n"
                  << "Resolution: " << width << "x" << height << "\n"
                  << "Avg latency: " << stats.avg_ms << " ms\n"
                  << "P50 latency: " << stats.p50_ms << " ms\n"
                  << "P99 latency: " << stats.p99_ms << " ms\n"
                  << "Throughput:  " << stats.fps << " FPS\n"
                  << "==========================================\n" << std::endl;
    }

    abi->instance_destroy(inst);
    CVPixelBufferRelease(pixel_buffer);
    abi->library_close(lib);
    dlclose(handle);

    return 0;
}
