#include "argus/algo.h"
#include "argus/types.h"
#include "argus/utils/profiler.hpp"
#include "argus/utils/json.hpp"
#include "core/env_config.hpp"
#include "preprocess/preprocessor.hpp"
#include "inference/rknn_runner.hpp"
#include "postprocess/postprocessor.hpp"

#define STB_IMAGE_IMPLEMENTATION
#include "third_party/stb_image.h"
#define STB_IMAGE_WRITE_IMPLEMENTATION
#include "third_party/stb_image_write.h"

#include <iostream>
#include <vector>
#include <chrono>
#include <cstring>
#include <fstream>
#include <iomanip>
#include <algorithm>

namespace {

struct ResultCapture {
    std::string json;
    std::vector<argus::cv::DetectionBox> objects;
    std::string error;
    bool self_test = false;
};

void logger(void* user, int level, const char* msg, uint32_t len) {
    std::cout << "[ALGO LOG " << level << "] " << std::string(msg, len) << std::endl;
}

void result_callback(const av_algo_result* res, void* user) {
    if (!res) return;
    auto* capture = static_cast<ResultCapture*>(user);
    if (!capture) return;

    capture->json.assign(res->json, res->json_len);
    if (res->kind == AV_RESULT_ALARM) {
        argus::utils::ParsedAlarmJson parsed;
        if (argus::utils::parse_alarm_json(capture->json, parsed, capture->error)) {
            capture->objects = std::move(parsed.objects);
        }
    }
}

// Convert RGB image to NV12 format
bool load_image_as_nv12(const std::string& path, uint32_t& width, uint32_t& height, std::vector<uint8_t>& nv12_buf) {
    int w = 0, h = 0, channels = 0;
    unsigned char* rgb = stbi_load(path.c_str(), &w, &h, &channels, 3);
    if (!rgb) {
        return false;
    }

    width = static_cast<uint32_t>(w);
    height = static_cast<uint32_t>(h);
    // Align to even dimensions for NV12
    uint32_t aligned_w = (width % 2 == 0) ? width : width - 1;
    uint32_t aligned_h = (height % 2 == 0) ? height : height - 1;
    width = aligned_w;
    height = aligned_h;

    nv12_buf.resize(width * height * 3 / 2);
    uint8_t* y_plane = nv12_buf.data();
    uint8_t* uv_plane = nv12_buf.data() + width * height;

    auto clamp_byte = [](double val) -> uint8_t {
        if (val < 0.0) return 0;
        if (val > 255.0) return 255;
        return static_cast<uint8_t>(val + 0.5);
    };

    // BT.709 limited range RGB to YUV
    for (uint32_t y = 0; y < height; ++y) {
        for (uint32_t x = 0; x < width; ++x) {
            const size_t rgb_idx = (y * w + x) * 3;
            const double r = rgb[rgb_idx];
            const double g = rgb[rgb_idx + 1];
            const double b = rgb[rgb_idx + 2];

            y_plane[y * width + x] = clamp_byte(16.0 + (65.481 * r + 128.553 * g + 24.966 * b) / 255.0);

            if ((y % 2 == 0) && (x % 2 == 0)) {
                // 2x2 block average for UV
                double r_avg = 0, g_avg = 0, b_avg = 0;
                for (int dy = 0; dy < 2; ++dy) {
                    for (int dx = 0; dx < 2; ++dx) {
                        const size_t sub_idx = ((y + dy) * w + (x + dx)) * 3;
                        r_avg += rgb[sub_idx];
                        g_avg += rgb[sub_idx + 1];
                        b_avg += rgb[sub_idx + 2];
                    }
                }
                r_avg /= 4.0;
                g_avg /= 4.0;
                b_avg /= 4.0;

                const size_t uv_idx = (y / 2) * width + x;
                uv_plane[uv_idx] = clamp_byte(128.0 + (-37.797 * r_avg - 74.203 * g_avg + 112.0 * b_avg) / 255.0);     // U
                uv_plane[uv_idx + 1] = clamp_byte(128.0 + (112.0 * r_avg - 93.786 * g_avg - 18.214 * b_avg) / 255.0); // V
            }
        }
    }

    stbi_image_free(rgb);
    return true;
}

// Draw bounding boxes on RGB image and save
bool draw_and_save_image(const std::string& input_path, const std::string& output_path,
                         const std::vector<argus::cv::DetectionBox>& boxes) {
    int w = 0, h = 0, channels = 0;
    unsigned char* img = stbi_load(input_path.c_str(), &w, &h, &channels, 3);
    if (!img) {
        return false;
    }

    // Draw boxes
    for (const auto& box : boxes) {
        int x1 = std::max(0, std::min(w - 1, static_cast<int>(box.x * w)));
        int y1 = std::max(0, std::min(h - 1, static_cast<int>(box.y * h)));
        int x2 = std::max(0, std::min(w - 1, static_cast<int>((box.x + box.w) * w)));
        int y2 = std::max(0, std::min(h - 1, static_cast<int>((box.y + box.h) * h)));

        // Draw 3px thick bounding box (red color [255, 0, 0])
        for (int t = 0; t < 3; ++t) {
            int cur_x1 = std::max(0, x1 - t);
            int cur_y1 = std::max(0, y1 - t);
            int cur_x2 = std::min(w - 1, x2 + t);
            int cur_y2 = std::min(h - 1, y2 + t);

            for (int x = cur_x1; x <= cur_x2; ++x) {
                // Top border
                img[(cur_y1 * w + x) * 3 + 0] = 255;
                img[(cur_y1 * w + x) * 3 + 1] = 0;
                img[(cur_y1 * w + x) * 3 + 2] = 0;
                // Bottom border
                img[(cur_y2 * w + x) * 3 + 0] = 255;
                img[(cur_y2 * w + x) * 3 + 1] = 0;
                img[(cur_y2 * w + x) * 3 + 2] = 0;
            }
            for (int y = cur_y1; y <= cur_y2; ++y) {
                // Left border
                img[(y * w + cur_x1) * 3 + 0] = 255;
                img[(y * w + cur_x1) * 3 + 1] = 0;
                img[(y * w + cur_x1) * 3 + 2] = 0;
                // Right border
                img[(y * w + cur_x2) * 3 + 0] = 255;
                img[(y * w + cur_x2) * 3 + 1] = 0;
                img[(y * w + cur_x2) * 3 + 2] = 0;
            }
        }
    }

    int ret = stbi_write_jpg(output_path.c_str(), w, h, 3, img, 90);
    stbi_image_free(img);
    return ret != 0;
}

} // namespace

int main(int argc, char** argv) {
    bool is_benchmark = false;
    std::string package_root = ".";
    std::string nv12_file = "";

    for (int i = 1; i < argc; ++i) {
        if (std::string(argv[i]) == "--benchmark") {
            is_benchmark = true;
        } else if (std::string(argv[i]).find(".bin") != std::string::npos) {
            nv12_file = argv[i];
        } else if (argv[i][0] != '-') {
            package_root = argv[i];
        }
    }

    auto env_config = yolov8n::load_package_env(package_root);

    const av_algo_abi* abi = av_algo_get_abi(AV_ALGO_API_VERSION);
    if (!abi) {
        std::cerr << "Failed to get av_algo_abi!" << std::endl;
        return 1;
    }

    av_algo_library_args lib_args{};
    lib_args.size = sizeof(lib_args);
    lib_args.api_version = AV_ALGO_API_VERSION;
    lib_args.package_root = package_root.c_str();
    lib_args.platform_id = "rk3576-rknn";
    lib_args.log = logger;

    av_algo_library lib = nullptr;
    int ret = abi->library_open(&lib_args, &lib);
    if (ret != AV_OK) {
        std::cerr << "library_open failed with error: " << ret << std::endl;
        return 1;
    }

    std::string config_json = "{\n  \"confidence_threshold\": " +
                              std::to_string(env_config.conf_thresh) + ",\n  \"iou_threshold\": " +
                              std::to_string(env_config.iou_thresh) + "\n}";

    ResultCapture capture;
    av_algo_instance_args inst_args{};
    inst_args.size = sizeof(inst_args);
    inst_args.api_version = AV_ALGO_API_VERSION;
    inst_args.mode = AV_INSTANCE_NORMAL;
    inst_args.config_json = config_json.c_str();
    inst_args.config_json_len = static_cast<uint32_t>(config_json.size());
    inst_args.on_result = result_callback;
    inst_args.result_user = &capture;

    av_algo_instance inst = nullptr;
    ret = abi->instance_create(lib, &inst_args, &inst);
    if (ret != AV_OK) {
        std::cerr << "instance_create failed with error: " << ret << std::endl;
        abi->library_close(lib);
        return 1;
    }

    // Read real image if available
    uint32_t width = 0;
    uint32_t height = 0;
    std::vector<uint8_t> frame_buf;
    std::string input_image_path = package_root + "/" + env_config.input_image;

    if (!load_image_as_nv12(input_image_path, width, height, frame_buf)) {
        if (!nv12_file.empty()) {
            std::ifstream ifs(nv12_file, std::ios::binary);
            if (ifs.is_open()) {
                frame_buf = std::vector<uint8_t>(std::istreambuf_iterator<char>(ifs), {});
                width = 810;
                height = 1080;
                std::cout << "Loaded NV12 frame from " << nv12_file << " (" << frame_buf.size() << " bytes)" << std::endl;
            }
        }
    } else {
        std::cout << "Loaded test image from " << input_image_path
                  << " (" << width << "x" << height << ")" << std::endl;
    }

    if (frame_buf.empty()) {
        width = 1920;
        height = 1080;
        frame_buf.assign(width * height * 3 / 2, 128);
    }

    // Negotiate
    av_frame_caps offered{};
    offered.size = sizeof(offered);
    offered.api_version = AV_ALGO_API_VERSION;
    offered.pixel_format_count = 1;
    offered.pixel_formats[0] = AV_PIX_NV12;
    offered.memory_type_count = 1;
    offered.memory_types[0] = AV_MEM_HOST;
    offered.min_width = std::min(width, 640u);
    offered.min_height = std::min(height, 640u);
    offered.max_width = std::max(width, 1920u);
    offered.max_height = std::max(height, 1080u);

    av_frame_caps accepted{};
    accepted.size = sizeof(accepted);
    accepted.api_version = AV_ALGO_API_VERSION;
    ret = abi->instance_negotiate(inst, &offered, &accepted);
    if (ret != AV_OK) {
        std::cerr << "instance_negotiate failed: " << ret << std::endl;
    }

    av_frame_desc frame{};
    frame.size = sizeof(frame);
    frame.api_version = AV_ALGO_API_VERSION;
    frame.pixel_format = AV_PIX_NV12;
    frame.memory_type = AV_MEM_HOST;
    frame.width = width;
    frame.height = height;
    frame.alloc_width = width;
    frame.alloc_height = height;
    frame.plane_count = 2;
    frame.stride[0] = width;
    frame.stride[1] = width;
    frame.offset[0] = 0;
    frame.offset[1] = width * height;
    frame.opaque = frame_buf.data();

    if (is_benchmark) {
        constexpr int kIterations = 100;
        constexpr int kWarmup = 5;

        // Direct pipeline runner for stage metrics breakdown
        yolov8n::RknnRunner direct_runner(logger, nullptr);
        std::string model_path = package_root + "/" + env_config.model_path;
        if (!direct_runner.load_model(model_path)) {
            model_path = package_root + "/model/yolov8n.rknn";
            direct_runner.load_model(model_path);
        }
        auto direct_instance = direct_runner.create_instance();

        // Warmup
        for (int i = 0; i < kWarmup; ++i) {
            abi->instance_process(inst, &frame);
        }

        std::vector<double> abi_times;
        std::vector<double> prep_times;
        std::vector<double> infer_times;
        std::vector<double> post_times;
        std::vector<double> e2e_times;
        abi_times.reserve(kIterations);
        prep_times.reserve(kIterations);
        infer_times.reserve(kIterations);
        post_times.reserve(kIterations);
        e2e_times.reserve(kIterations);

        for (int i = 0; i < kIterations; ++i) {
            auto t0 = std::chrono::high_resolution_clock::now();
            abi->instance_process(inst, &frame);
            auto t1 = std::chrono::high_resolution_clock::now();
            abi_times.push_back(std::chrono::duration<double, std::milli>(t1 - t0).count());

            if (direct_instance) {
                double t_prep = 0.0, t_infer = 0.0, t_post = 0.0;

                auto s0 = std::chrono::high_resolution_clock::now();
                yolov8n::PreparedInput prepared;
                bool prep_ok = yolov8n::Preprocessor::prepare_input(&frame, nullptr, 640, 640, prepared);
                auto s1 = std::chrono::high_resolution_clock::now();
                t_prep = std::chrono::duration<double, std::milli>(s1 - s0).count();

                std::vector<yolov8n::RknnOutputBuffer> outputs;
                if (prep_ok && prepared.view.data) {
                    direct_instance->run(prepared.view.data, prepared.view.width * prepared.view.height * 3, outputs);
                }
                auto s2 = std::chrono::high_resolution_clock::now();
                t_infer = std::chrono::duration<double, std::milli>(s2 - s1).count();

                yolov8n::Postprocessor::decode(outputs, prepared.letterbox, env_config.conf_thresh, env_config.iou_thresh, frame.width, frame.height);
                auto s3 = std::chrono::high_resolution_clock::now();
                t_post = std::chrono::duration<double, std::milli>(s3 - s2).count();
                double t_e2e = std::chrono::duration<double, std::milli>(s3 - s0).count();

                yolov8n::Preprocessor::release_input(prepared, nullptr);

                prep_times.push_back(t_prep);
                infer_times.push_back(t_infer);
                post_times.push_back(t_post);
                e2e_times.push_back(t_e2e);
            }
        }

        const auto abi_stats = argus::utils::BenchmarkStats::compute(abi_times);

        std::cout << "\n--- Benchmark Report (" << kIterations << " iterations, " << kWarmup << " warmup) ---\n";
        if (direct_instance && !e2e_times.empty()) {
            const auto prep_stats = argus::utils::BenchmarkStats::compute(prep_times);
            const auto infer_stats = argus::utils::BenchmarkStats::compute(infer_times);
            const auto post_stats = argus::utils::BenchmarkStats::compute(post_times);
            const auto e2e_stats = argus::utils::BenchmarkStats::compute(e2e_times);

            std::cout << "  Preprocess:  Avg " << std::fixed << std::setprecision(2) << prep_stats.avg_ms
                      << " ms | P50 " << prep_stats.p50_ms << " ms | P99 " << prep_stats.p99_ms << " ms\n"
                      << "  Inference:   Avg " << infer_stats.avg_ms << " ms | P50 " << infer_stats.p50_ms
                      << " ms | P99 " << infer_stats.p99_ms << " ms\n"
                      << "  Postprocess: Avg " << post_stats.avg_ms << " ms | P50 " << post_stats.p50_ms
                      << " ms | P99 " << post_stats.p99_ms << " ms\n"
                      << "  End-to-end:  Avg " << e2e_stats.avg_ms << " ms | P50 " << e2e_stats.p50_ms
                      << " ms | P99 " << e2e_stats.p99_ms << " ms | FPS: " << e2e_stats.fps << "\n";
        }
        std::cout << "  ABI process: Avg " << std::fixed << std::setprecision(2) << abi_stats.avg_ms
                  << " ms | P50 " << abi_stats.p50_ms << " ms | P99 " << abi_stats.p99_ms
                  << " ms | FPS: " << abi_stats.fps << "\n\n";
    } else {
        ret = abi->instance_process(inst, &frame);
        std::cout << "Process return code: " << ret << std::endl;
        std::cout << "[Detection Result]\n" << capture.json << std::endl;

        std::string output_image_path = package_root + "/" + env_config.output_image;
        if (draw_and_save_image(input_image_path, output_image_path, capture.objects)) {
            std::cout << "[Visualizer] Saved result image to " << output_image_path << std::endl;
        } else {
            std::cerr << "[Visualizer] Failed to write result image to " << output_image_path << std::endl;
        }
    }

    abi->instance_destroy(inst);
    abi->library_close(lib);
    return 0;
}
