#include "argus/algo.h"
#include <iostream>
#include <fstream>
#include <vector>
#include <string>
#include <sstream>
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

void log_callback(void* user_data, int level, const char* msg, uint32_t len) {
    (void)user_data;
    std::cout << "[algorithm " << level << "] " << std::string(msg ? msg : "", len) << std::endl;
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

bool render_results_to_image(const std::string& input_path, const std::string& output_path, const std::string& json_str) {
    int w = 0, h = 0, ch = 0;
    uint8_t* img = stbi_load(input_path.c_str(), &w, &h, &ch, 3);
    if (!img) return false;

    // Parse plate boxes from json
    size_t pos = 0;
    while ((pos = json_str.find("\"bbox\": {", pos)) != std::string::npos || (pos = json_str.find("\"bbox\":{", pos)) != std::string::npos) {
        float x_min = 0, y_min = 0, x_max = 0, y_max = 0;
        auto get_f = [&](std::string_view key, float& out_val) {
            size_t k = json_str.find(key, pos);
            if (k != std::string::npos && k < json_str.find('}', pos)) {
                size_t c = json_str.find(':', k);
                if (c != std::string::npos) {
                    out_val = std::stof(json_str.substr(c + 1));
                }
            }
        };
        get_f("\"x_min\":", x_min);
        get_f("\"y_min\":", y_min);
        get_f("\"x_max\":", x_max);
        get_f("\"y_max\":", y_max);

        int bx = static_cast<int>(x_min * w);
        int by = static_cast<int>(y_min * h);
        int bw = static_cast<int>((x_max - x_min) * w);
        int bh = static_cast<int>((y_max - y_min) * h);
        draw_rect(img, w, h, bx, by, bw, bh, 0, 255, 0, 3); // Green plate box

        // Check vehicle box
        size_t vpos = json_str.find("\"vehicle_bbox\": {", pos);
        if (vpos == std::string::npos) vpos = json_str.find("\"vehicle_bbox\":{", pos);
        if (vpos != std::string::npos && vpos < pos + 400) {
            float vx_min = 0, vy_min = 0, vx_max = 0, vy_max = 0;
            auto get_vf = [&](std::string_view key, float& out_val) {
                size_t k = json_str.find(key, vpos);
                if (k != std::string::npos && k < json_str.find('}', vpos)) {
                    size_t c = json_str.find(':', k);
                    if (c != std::string::npos) {
                        out_val = std::stof(json_str.substr(c + 1));
                    }
                }
            };
            get_vf("\"x_min\":", vx_min);
            get_vf("\"y_min\":", vy_min);
            get_vf("\"x_max\":", vx_max);
            get_vf("\"y_max\":", vy_max);

            int vbx = static_cast<int>(vx_min * w);
            int vby = static_cast<int>(vy_min * h);
            int vbw = static_cast<int>((vx_max - vx_min) * w);
            int vbh = static_cast<int>((vy_max - vy_min) * h);
            draw_rect(img, w, h, vbx, vby, vbw, vbh, 0, 160, 255, 2); // Blue vehicle box
        }

        pos += 8;
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
    if (status != kCVReturnSuccess || !pixel_buffer) {
        return nullptr;
    }

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

} // namespace

int main(int argc, char** argv) {
    std::cout << "========================================\n";
    std::cout << "  License Plate Recognition Standalone Runner (macOS ARM64)\n";
    std::cout << "========================================\n";

    std::string mode = "run";
    std::string image_path = "testimage.jpg";
    std::string output_path = "result.jpg";
    std::string dylib_path = "lib/liblicense_plate_recognition.dylib";
    std::string package_root = ".";

    for (int i = 1; i < argc; ++i) {
        std::string arg = argv[i];
        if (arg == "run" || arg == "benchmark" || arg == "selftest") {
            mode = arg;
        } else if (arg == "--image" && i + 1 < argc) {
            image_path = argv[++i];
        } else if (arg == "--output" && i + 1 < argc) {
            output_path = argv[++i];
        } else if (arg == "--dylib" && i + 1 < argc) {
            dylib_path = argv[++i];
        } else if (arg == "--root" && i + 1 < argc) {
            package_root = argv[++i];
        } else if (arg[0] != '-') {
            image_path = arg;
        }
    }

    std::cout << "[Config] mode=" << mode << " input=" << image_path << " output=" << output_path
              << " package_root=" << package_root << '\n';

    void* handle = nullptr;
    std::vector<std::string> search_paths = {
        dylib_path,
        "lib/liblicense_plate_recognition.dylib",
        "build/liblicense_plate_recognition.dylib",
        "./liblicense_plate_recognition.dylib"
    };

    for (const auto& p : search_paths) {
        handle = dlopen(p.c_str(), RTLD_NOW | RTLD_LOCAL);
        if (handle) {
            dylib_path = p;
            break;
        }
    }

    if (!handle) {
        std::cerr << "Failed to dlopen " << dylib_path << ": " << dlerror() << std::endl;
        return 1;
    }

    using GetAbiFn = const av_algo_abi* (*)(uint32_t);
    auto get_abi = reinterpret_cast<GetAbiFn>(dlsym(handle, "av_algo_get_abi"));
    if (!get_abi) {
        std::cerr << "Failed to find av_algo_get_abi symbol" << std::endl;
        dlclose(handle);
        return 1;
    }

    const av_algo_abi* abi = get_abi(AV_ALGO_API_VERSION);
    if (!abi) {
        std::cerr << "ABI version mismatch" << std::endl;
        dlclose(handle);
        return 1;
    }

    av_algo_library_args lib_args{};
    lib_args.size = sizeof(av_algo_library_args);
    lib_args.api_version = AV_ALGO_API_VERSION;
    lib_args.package_root = package_root.c_str();
    lib_args.platform_id = "macos-arm64-coreml";
    lib_args.log = log_callback;

    av_algo_library lib = nullptr;
    int rc = abi->library_open(&lib_args, &lib);
    if (rc != AV_OK) {
        char err[256] = {0};
        abi->last_error(nullptr, err, sizeof(err));
        std::cerr << "library_open failed: " << rc << ", " << err << std::endl;
        dlclose(handle);
        return 1;
    }

    std::string captured_result_json;
    av_algo_instance_args inst_args{};
    inst_args.size = sizeof(av_algo_instance_args);
    inst_args.api_version = AV_ALGO_API_VERSION;
    inst_args.mode = (mode == "selftest") ? AV_INSTANCE_INSTALL_SELF_TEST : AV_INSTANCE_NORMAL;
    inst_args.instance_id = "runner-lpr-inst";
    inst_args.result_user = &captured_result_json;
    inst_args.on_result = [](const av_algo_result* res, void* user) {
        if (!res) return;
        if (res->json && res->json_len > 0) {
            std::cout << "[Recognition Result]\n"
                      << std::string(res->json, res->json_len) << "\n" << std::endl;
            if (user) {
                auto* p_json = static_cast<std::string*>(user);
                p_json->assign(res->json, res->json_len);
            }
        }
    };

    av_algo_instance inst = nullptr;
    rc = abi->instance_create(lib, &inst_args, &inst);
    if (rc != AV_OK) {
        char err[256] = {0};
        abi->last_error(nullptr, err, sizeof(err));
        std::cerr << "instance_create failed: " << rc << ", " << err << std::endl;
        abi->library_close(lib);
        dlclose(handle);
        return 1;
    }

    int width = 0, height = 0, channels = 0;
    uint8_t* img_data = stbi_load(image_path.c_str(), &width, &height, &channels, 3);
    if (!img_data) {
        std::cerr << "Note: Could not open " << image_path << ", generating synthetic test frame (1280x720)..." << std::endl;
        width = 1280;
        height = 720;
        std::vector<uint8_t> syn_rgb(static_cast<size_t>(width) * height * 3, 128);
        img_data = (uint8_t*)malloc(syn_rgb.size());
        std::memcpy(img_data, syn_rgb.data(), syn_rgb.size());
    }

    CVPixelBufferRef pixel_buffer = create_nv12_pixel_buffer(img_data, width, height);
    free(img_data);

    if (!pixel_buffer) {
        std::cerr << "Failed to create CVPixelBuffer" << std::endl;
        abi->instance_destroy(inst);
        abi->library_close(lib);
        dlclose(handle);
        return 1;
    }

    av_frame_desc frame{};
    frame.size = sizeof(av_frame_desc);
    frame.api_version = AV_ALGO_API_VERSION;
    frame.memory_type = AV_MEM_HOST;
    frame.opaque_kind = AV_OPAQUE_CVPIXELBUFFER;
    frame.opaque = pixel_buffer;
    frame.pixel_format = AV_PIX_NV12;
    frame.width = width;
    frame.height = height;
    frame.stride[0] = width;
    frame.stride[1] = width;

    // Send multiple frames to satisfy tracking & majority voting window
    int num_frames = (mode == "benchmark") ? 50 : 5;
    for (int iter = 1; iter <= num_frames; ++iter) {
        frame.frame_id = iter;
        frame.pts_ns = iter * 40'000'000LL; // 25 FPS
        frame.wall_time_ns = std::chrono::duration_cast<std::chrono::nanoseconds>(
                                std::chrono::system_clock::now().time_since_epoch()).count();

        rc = abi->instance_process(inst, &frame);
        if (rc != AV_OK) {
            char err[256] = {0};
            abi->last_error(inst, err, sizeof(err));
            std::cerr << "instance_process failed at frame " << iter << ": " << rc << ", " << err << std::endl;
            break;
        }
    }

    if (!captured_result_json.empty() && mode == "run") {
        if (render_results_to_image(image_path, output_path, captured_result_json)) {
            std::cout << "[Visualizer] Saved result image to " << output_path << std::endl;
        }
    }

    CVPixelBufferRelease(pixel_buffer);
    abi->instance_destroy(inst);
    abi->library_close(lib);
    dlclose(handle);

    return 0;
}
