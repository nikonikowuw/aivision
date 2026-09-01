#include "argus/algo.h"
#include "argus/utils/profiler.hpp"
#include "../preprocess/preprocessor.hpp"
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

    av_algo_library_args lib_args{};
    lib_args.size = sizeof(lib_args);
    lib_args.api_version = AV_ALGO_API_VERSION;
    lib_args.package_root = package_root.c_str();
    lib_args.platform_id = "macos-arm64-coreml";

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
    } else if (mode == "benchmark") {
        std::cout << "Starting benchmark (10 warmup, 50 measured iterations)..." << std::endl;
        for (int i = 0; i < 10; ++i) {
            frame.frame_id = i + 1;
            frame.pts_ns = 1000000000 + i * 40000000;
            abi->instance_process(inst, &frame);
        }

        std::vector<double> samples;
        for (int i = 0; i < 50; ++i) {
            frame.frame_id = i + 20;
            frame.pts_ns = 1000000000 + (i + 20) * 40000000;
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
