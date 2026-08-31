#import <Accelerate/Accelerate.h>
#import <CoreGraphics/CoreGraphics.h>
#import <CoreText/CoreText.h>
#import <CoreVideo/CoreVideo.h>
#import <Foundation/Foundation.h>
#import <ImageIO/ImageIO.h>

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <cstdlib>
#include <filesystem>
#include <iomanip>
#include <iostream>
#include <memory>
#include <sstream>
#include <string>
#include <unordered_set>
#include <utility>
#include <vector>

#include "argus/algo.h"
#include "argus/types.h"
#include "argus/utils/env.hpp"
#include "argus/utils/json.hpp"
#include "argus/utils/profiler.hpp"
#include "inference/coreml_runner.hpp"
#include "postprocess/postprocessor.hpp"
#include "preprocess/preprocessor.hpp"

namespace {

struct NativeFrame {
    CVPixelBufferRef pixel_buffer = nullptr;
    av_frame_desc desc{};

    NativeFrame() = default;
    NativeFrame(const NativeFrame&) = delete;
    NativeFrame& operator=(const NativeFrame&) = delete;
    NativeFrame(NativeFrame&& other) noexcept : pixel_buffer(other.pixel_buffer), desc(other.desc) {
        other.pixel_buffer = nullptr;
    }
    NativeFrame& operator=(NativeFrame&& other) noexcept {
        if (this == &other) return *this;
        if (pixel_buffer) CVPixelBufferRelease(pixel_buffer);
        pixel_buffer = other.pixel_buffer;
        desc = other.desc;
        other.pixel_buffer = nullptr;
        return *this;
    }

    ~NativeFrame() {
        if (pixel_buffer) CVPixelBufferRelease(pixel_buffer);
    }
};

struct ResultCapture {
    std::string json;
    std::vector<argus::cv::DetectionBox> objects;
    std::string error;
    bool self_test = false;
    uint32_t alarm_callback_count = 0;
};

struct RunnerPackageRoot {
    std::filesystem::path path;
    std::string value;

    ~RunnerPackageRoot() {
        if (!path.empty()) {
            std::error_code error;
            std::filesystem::remove_all(path, error);
        }
    }

    bool prepare(const std::string& configured_model_path) {
        const auto root = std::filesystem::current_path();
        std::error_code error;
        const auto default_model = std::filesystem::weakly_canonical(root / "model/yolo26n.mlpackage", error);
        if (error) return false;
        error.clear();
        const auto selected_model = std::filesystem::weakly_canonical(
            std::filesystem::path(configured_model_path), error);
        if (error) return false;
        if (selected_model == default_model) {
            value = root.string();
            return true;
        }

        const auto base = std::filesystem::temp_directory_path() /
            ("argus-yolo26n-runner-" + std::to_string(
                std::chrono::steady_clock::now().time_since_epoch().count()));
        error.clear();
        path = base;
        std::filesystem::create_directories(path / "model", error);
        if (error) return false;
        std::filesystem::copy(selected_model, path / "model/yolo26n.mlpackage",
                              std::filesystem::copy_options::recursive, error);
        if (error) {
            std::filesystem::remove_all(path, error);
            path.clear();
            return false;
        }
        value = path.string();
        return true;
    }
};

void runner_log(void*, int level, const char* message, uint32_t len) {
    std::cerr << "[algorithm " << level << "] " << std::string(message ? message : "", len) << '\n';
}

void result_callback(const av_algo_result* result, void* user) noexcept {
    auto* capture = static_cast<ResultCapture*>(user);
    if (!capture || !result) return;
    try {
        if (result->size < sizeof(av_algo_result) || result->api_version != AV_ALGO_API_VERSION ||
            !result->json || result->json_len > AV_MAX_RESULT_JSON_BYTES ||
            (result->image_count > 0 && !result->images)) {
            capture->error = "algorithm returned an invalid result header";
            return;
        }
        capture->json.assign(result->json, result->json_len);
        capture->self_test = result->kind == AV_RESULT_SELF_TEST;
        if (result->kind == AV_RESULT_ALARM) {
            ++capture->alarm_callback_count;
            if (capture->alarm_callback_count != 1) {
                capture->error = "algorithm returned more than one alarm batch";
                return;
            }
            argus::utils::ParsedAlarmJson parsed;
            if (!argus::utils::parse_alarm_json(capture->json, parsed, capture->error)) return;
            capture->objects = std::move(parsed.objects);
        } else if (result->kind != AV_RESULT_SELF_TEST) {
            capture->error = "algorithm returned an unknown result kind";
        }
    } catch (...) {
        capture->error = "result callback failed to parse algorithm output";
    }
}

CVPixelBufferRef load_image_as_bgra(const std::string& path, uint32_t& out_width, uint32_t& out_height) {
    @autoreleasepool {
        NSString* ns_path = [NSString stringWithUTF8String:path.c_str()];
        if (!ns_path) return nullptr;
        NSURL* url = [NSURL fileURLWithPath:ns_path];
        CGImageSourceRef source = CGImageSourceCreateWithURL((__bridge CFURLRef)url, nullptr);
        CGImageRef image = source ? CGImageSourceCreateImageAtIndex(source, 0, nullptr) : nullptr;
        if (source) CFRelease(source);
        if (!image) return nullptr;

        const size_t width = CGImageGetWidth(image);
        const size_t height = CGImageGetHeight(image);
        out_width = static_cast<uint32_t>(width);
        out_height = static_cast<uint32_t>(height);
        NSDictionary* options = @{
            (id)kCVPixelBufferCGImageCompatibilityKey: @YES,
            (id)kCVPixelBufferCGBitmapContextCompatibilityKey: @YES
        };
        CVPixelBufferRef pixel_buffer = nullptr;
        const CVReturn status = CVPixelBufferCreate(
            kCFAllocatorDefault, width, height, kCVPixelFormatType_32BGRA,
            (__bridge CFDictionaryRef)options, &pixel_buffer);
        if (status != kCVReturnSuccess || !pixel_buffer) {
            CGImageRelease(image);
            return nullptr;
        }

        if (CVPixelBufferLockBaseAddress(pixel_buffer, 0) != kCVReturnSuccess) {
            CVPixelBufferRelease(pixel_buffer);
            CGImageRelease(image);
            return nullptr;
        }
        CGColorSpaceRef color_space = CGColorSpaceCreateDeviceRGB();
        CGContextRef context = CGBitmapContextCreate(
            CVPixelBufferGetBaseAddress(pixel_buffer), width, height, 8,
            CVPixelBufferGetBytesPerRow(pixel_buffer), color_space,
            static_cast<CGBitmapInfo>(static_cast<uint32_t>(kCGImageAlphaPremultipliedFirst) |
                                       static_cast<uint32_t>(kCGBitmapByteOrder32Little)));
        if (!context) {
            CVPixelBufferUnlockBaseAddress(pixel_buffer, 0);
            CVPixelBufferRelease(pixel_buffer);
            CGColorSpaceRelease(color_space);
            CGImageRelease(image);
            return nullptr;
        }
        CGContextDrawImage(context, CGRectMake(0, 0, width, height), image);
        CGContextRelease(context);
        CGColorSpaceRelease(color_space);
        CVPixelBufferUnlockBaseAddress(pixel_buffer, 0);
        CGImageRelease(image);
        return pixel_buffer;
    }
}

uint8_t clamp_byte(double value) {
    return static_cast<uint8_t>(std::clamp<long>(std::lround(value), 0, 255));
}

NativeFrame load_image_as_nv12(const std::string& path) {
    NativeFrame frame;
    uint32_t source_width = 0;
    uint32_t source_height = 0;
    CVPixelBufferRef source = load_image_as_bgra(path, source_width, source_height);
    if (!source) return frame;

    const size_t width = source_width & ~static_cast<size_t>(1);
    const size_t height = source_height & ~static_cast<size_t>(1);
    if (width == 0 || height == 0) {
        CVPixelBufferRelease(source);
        return frame;
    }

    NSDictionary* options = @{
        (id)kCVPixelBufferCGImageCompatibilityKey: @YES,
        (id)kCVPixelBufferCGBitmapContextCompatibilityKey: @YES
    };
    CVPixelBufferRef nv12 = nullptr;
    const CVReturn status = CVPixelBufferCreate(
        kCFAllocatorDefault, width, height,
        kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange,
        (__bridge CFDictionaryRef)options, &nv12);
    if (status != kCVReturnSuccess || !nv12) {
        CVPixelBufferRelease(source);
        return frame;
    }

    const bool source_locked = CVPixelBufferLockBaseAddress(source, kCVPixelBufferLock_ReadOnly) == kCVReturnSuccess;
    const bool target_locked = source_locked && CVPixelBufferLockBaseAddress(nv12, 0) == kCVReturnSuccess;
    if (!target_locked) {
        if (source_locked) CVPixelBufferUnlockBaseAddress(source, kCVPixelBufferLock_ReadOnly);
        CVPixelBufferRelease(nv12);
        CVPixelBufferRelease(source);
        return frame;
    }

    const auto* source_bytes = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddress(source));
    const size_t source_stride = CVPixelBufferGetBytesPerRow(source);
    auto* y_plane = static_cast<uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(nv12, 0));
    auto* uv_plane = static_cast<uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(nv12, 1));
    const size_t y_stride = CVPixelBufferGetBytesPerRowOfPlane(nv12, 0);
    const size_t uv_stride = CVPixelBufferGetBytesPerRowOfPlane(nv12, 1);

    for (size_t y = 0; y < height; ++y) {
        for (size_t x = 0; x < width; ++x) {
            const uint8_t* pixel = source_bytes + y * source_stride + x * 4;
            const double b = pixel[0];
            const double g = pixel[1];
            const double r = pixel[2];
            y_plane[y * y_stride + x] = clamp_byte(16.0 + (65.481 * r + 128.553 * g + 24.966 * b) / 255.0);
        }
    }
    for (size_t y = 0; y < height; y += 2) {
        for (size_t x = 0; x < width; x += 2) {
            double sum_cb = 0.0;
            double sum_cr = 0.0;
            for (size_t dy = 0; dy < 2; ++dy) {
                for (size_t dx = 0; dx < 2; ++dx) {
                    const uint8_t* pixel = source_bytes + (y + dy) * source_stride + (x + dx) * 4;
                    const double b = pixel[0];
                    const double g = pixel[1];
                    const double r = pixel[2];
                    sum_cb += 128.0 + (-37.797 * r - 74.203 * g + 112.0 * b) / 255.0;
                    sum_cr += 128.0 + (112.0 * r - 93.786 * g - 18.214 * b) / 255.0;
                }
            }
            uint8_t* uv = uv_plane + (y / 2) * uv_stride + x;
            uv[0] = clamp_byte(sum_cb / 4.0);
            uv[1] = clamp_byte(sum_cr / 4.0);
        }
    }

    const auto* base = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddress(nv12));
    const auto* uv_base = static_cast<const uint8_t*>(CVPixelBufferGetBaseAddressOfPlane(nv12, 1));
    frame.pixel_buffer = nv12;
    frame.desc.size = sizeof(av_frame_desc);
    frame.desc.api_version = AV_ALGO_API_VERSION;
    frame.desc.frame_id = 1;
    frame.desc.wall_time_ns = std::chrono::duration_cast<std::chrono::nanoseconds>(
        std::chrono::system_clock::now().time_since_epoch()).count();
    frame.desc.pts_ns = frame.desc.wall_time_ns;
    frame.desc.opaque = nv12;
    frame.desc.platform_tag = 0x4D41434F;
    frame.desc.opaque_kind = AV_OPAQUE_CVPIXELBUFFER;
    frame.desc.memory_type = AV_MEM_PLATFORM_SURFACE;
    frame.desc.pixel_format = AV_PIX_NV12;
    frame.desc.layout = AV_LAYOUT_PLATFORM_NATIVE;
    frame.desc.width = static_cast<uint32_t>(width);
    frame.desc.height = static_cast<uint32_t>(height);
    frame.desc.alloc_width = static_cast<uint32_t>(width);
    frame.desc.alloc_height = static_cast<uint32_t>(height);
    frame.desc.stride[0] = static_cast<int32_t>(y_stride);
    frame.desc.stride[1] = static_cast<int32_t>(uv_stride);
    frame.desc.offset[0] = 0;
    frame.desc.offset[1] = base && uv_base && reinterpret_cast<uintptr_t>(uv_base) >= reinterpret_cast<uintptr_t>(base)
        ? static_cast<uint64_t>(reinterpret_cast<uintptr_t>(uv_base) - reinterpret_cast<uintptr_t>(base))
        : 0;
    frame.desc.color_primaries = AV_COLOR_PRIM_BT709;
    frame.desc.color_transfer = AV_COLOR_TRC_BT709;
    frame.desc.color_matrix = AV_COLOR_MAT_BT709;
    frame.desc.color_range = AV_COLOR_RANGE_LIMITED;
    frame.desc.plane_count = 2;
    frame.desc.time_synced = 1;

    CVPixelBufferUnlockBaseAddress(nv12, 0);
    CVPixelBufferUnlockBaseAddress(source, kCVPixelBufferLock_ReadOnly);
    CVPixelBufferRelease(source);
    return frame;
}

std::string make_config_json(float confidence, float iou) {
    std::ostringstream output;
    output << std::setprecision(9)
           << "{\"confidence_threshold\":" << confidence
           << ",\"iou_threshold\":" << iou << "}";
    return output.str();
}

std::unordered_set<std::string> parse_target_classes(const std::string& value) {
    std::unordered_set<std::string> classes;
    size_t begin = 0;
    while (begin < value.size()) {
        size_t end = value.find(',', begin);
        if (end == std::string::npos) end = value.size();
        std::string item = value.substr(begin, end - begin);
        const size_t first = item.find_first_not_of(" \t");
        const size_t last = item.find_last_not_of(" \t");
        if (first != std::string::npos) classes.insert(item.substr(first, last - first + 1));
        begin = end + 1;
    }
    return classes;
}

std::vector<argus::cv::DetectionBox> filter_target_classes(
    const std::vector<argus::cv::DetectionBox>& objects,
    const std::unordered_set<std::string>& target_classes) {
    if (target_classes.empty()) return objects;
    std::vector<argus::cv::DetectionBox> filtered;
    for (const auto& object : objects) {
        if (target_classes.contains(object.label)) filtered.push_back(object);
    }
    return filtered;
}

bool save_visualization(const std::string& output_path, uint32_t width, uint32_t height,
                       const std::vector<argus::cv::DetectionBox>& objects,
                       const std::string& input_path) {
    @autoreleasepool {
        NSString* ns_path = [NSString stringWithUTF8String:input_path.c_str()];
        NSURL* url = ns_path ? [NSURL fileURLWithPath:ns_path] : nil;
        CGImageSourceRef source = url ? CGImageSourceCreateWithURL((__bridge CFURLRef)url, nullptr) : nullptr;
        CGImageRef original = source ? CGImageSourceCreateImageAtIndex(source, 0, nullptr) : nullptr;
        if (source) CFRelease(source);

        CGColorSpaceRef color_space = CGColorSpaceCreateDeviceRGB();
        CGContextRef context = CGBitmapContextCreate(nullptr, width, height, 8, width * 4, color_space,
                                                       kCGImageAlphaPremultipliedLast);
        if (!context) {
            if (original) CGImageRelease(original);
            CGColorSpaceRelease(color_space);
            return false;
        }
        if (original) {
            CGContextDrawImage(context, CGRectMake(0, 0, width, height), original);
            CGImageRelease(original);
        } else {
            CGContextSetRGBFillColor(context, 0.2, 0.25, 0.3, 1.0);
            CGContextFillRect(context, CGRectMake(0, 0, width, height));
        }

        CGContextSetLineWidth(context, 3.0);
        CTFontRef font = CTFontCreateWithName(CFSTR("Helvetica"), 15.0, nullptr);
        for (const auto& object : objects) {
            const CGFloat x = object.x * width;
            const CGFloat y = (1.0 - object.y - object.h) * height;
            const CGFloat w = object.w * width;
            const CGFloat h = object.h * height;
            CGContextSetRGBStrokeColor(context, 0.0, 1.0, 0.0, 1.0);
            CGContextStrokeRect(context, CGRectMake(x, y, w, h));
            if (font) {
                CGContextSetRGBFillColor(context, 0.0, 1.0, 0.0, 1.0);
                const std::string label = object.label + " " + std::to_string(object.confidence).substr(0, 5);
                NSString* label_string = [NSString stringWithUTF8String:label.c_str()];
                NSDictionary* attributes = @{
                    (__bridge id)kCTFontAttributeName: (__bridge id)font
                };
                NSAttributedString* attributed = [[NSAttributedString alloc]
                    initWithString:label_string ? label_string : @"object"
                    attributes:attributes];
                CTLineRef line = CTLineCreateWithAttributedString((__bridge CFAttributedStringRef)attributed);
                if (line) {
                    CGContextSetTextPosition(context, x, std::min<CGFloat>(height - 4.0, y + h + 16.0));
                    CTLineDraw(line, context);
                    CFRelease(line);
                }
            }
        }
        if (font) CFRelease(font);

        CGImageRef image = CGBitmapContextCreateImage(context);
        NSURL* output_url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:output_path.c_str()]];
        CGImageDestinationRef destination = output_url
            ? CGImageDestinationCreateWithURL((__bridge CFURLRef)output_url, CFSTR("public.jpeg"), 1, nullptr)
            : nullptr;
        const bool ok = image && destination && (CGImageDestinationAddImage(destination, image, nullptr),
                                                  CGImageDestinationFinalize(destination));
        if (destination) CFRelease(destination);
        if (image) CGImageRelease(image);
        CGContextRelease(context);
        CGColorSpaceRelease(color_space);
        return ok;
    }
}

struct StageSamples {
    std::vector<double> preprocess_ms;
    std::vector<double> inference_ms;
    std::vector<double> postprocess_ms;
    std::vector<double> end_to_end_ms;
};

bool run_direct_pipeline(const av_frame_desc& frame, yolo26n::CoreMLRunner& runner,
                         float confidence, float iou, StageSamples* samples) {
    const auto end_to_end_begin = std::chrono::steady_clock::now();
    const auto preprocess_begin = std::chrono::steady_clock::now();
    void* input_pixelbuffer = yolo26n::Preprocessor::create_input_pixelbuffer(&frame, nullptr, 640, 384);
    const auto preprocess_end = std::chrono::steady_clock::now();
    if (!input_pixelbuffer) return false;
    struct PixelBufferGuard {
        void* value;
        ~PixelBufferGuard() { yolo26n::Preprocessor::release_pixelbuffer(value); }
    } guard{input_pixelbuffer};

    std::vector<float> network_output;
    const auto inference_begin = std::chrono::steady_clock::now();
    const bool inference_ok = runner.run_pixelbuffer(input_pixelbuffer, network_output);
    const auto inference_end = std::chrono::steady_clock::now();
    if (!inference_ok) return false;

    const auto postprocess_begin = std::chrono::steady_clock::now();
    const auto objects = yolo26n::Postprocessor::postprocess(
        network_output, confidence, iou, frame.width, frame.height);
    const auto postprocess_end = std::chrono::steady_clock::now();
    const auto end_to_end_end = std::chrono::steady_clock::now();
    if (samples) {
        samples->preprocess_ms.push_back(std::chrono::duration<double, std::milli>(
            preprocess_end - preprocess_begin).count());
        samples->inference_ms.push_back(std::chrono::duration<double, std::milli>(
            inference_end - inference_begin).count());
        samples->postprocess_ms.push_back(std::chrono::duration<double, std::milli>(
            postprocess_end - postprocess_begin).count());
        samples->end_to_end_ms.push_back(std::chrono::duration<double, std::milli>(
            end_to_end_end - end_to_end_begin).count());
    }
    (void)objects;
    return true;
}

int run(int argc, char* argv[]) {
    const auto env = argus::utils::EnvReader::load_file(".env");
    const float confidence = argus::utils::EnvReader::get_float("CONF_THRESH", 0.5f, env);
    const float iou = argus::utils::EnvReader::get_float("IOU_THRESH", 0.45f, env);
    const std::string input_path = argus::utils::EnvReader::get("INPUT_IMAGE", "testimage.jpg", env);
    const std::string output_path = argus::utils::EnvReader::get("OUTPUT_IMAGE", "result.jpg", env);
    const std::string model_path = argus::utils::EnvReader::get("MODEL_PATH", "model/yolo26n.mlpackage", env);
    const std::unordered_set<std::string> target_classes = parse_target_classes(
        argus::utils::EnvReader::get("TARGET_CLASSES", "", env));
    const bool benchmark = argc > 1 && std::string(argv[1]) == "--benchmark";
    const int loops = argus::utils::EnvReader::get_int("LOOPS", benchmark ? 100 : 1, env);
    const int warmup = argus::utils::EnvReader::get_int("WARMUP", benchmark ? 5 : 0, env);
    if (loops <= 0 || warmup < 0) {
        std::cerr << "LOOPS must be positive and WARMUP must be non-negative\n";
        return 1;
    }

    std::cout << "[Config] confidence=" << confidence << " iou=" << iou
              << " input=" << input_path << " output=" << output_path
              << " model=" << model_path << " loops=" << loops << " warmup=" << warmup << '\n';

    RunnerPackageRoot runner_package_root;
    if (!runner_package_root.prepare(model_path)) {
        std::cerr << "Failed to stage model inside a private runner package root: " << model_path << '\n';
        return 1;
    }

    NativeFrame frame = load_image_as_nv12(input_path);
    if (!frame.pixel_buffer) {
        std::cerr << "Failed to load input image as NV12: " << input_path << '\n';
        return 1;
    }

    const av_algo_abi* abi = av_algo_get_abi(AV_ALGO_API_VERSION);
    if (!abi || abi->size < sizeof(av_algo_abi) || abi->api_version != AV_ALGO_API_VERSION ||
        !abi->library_open || !abi->library_query || !abi->library_close || !abi->instance_create ||
        !abi->instance_negotiate || !abi->instance_update_config || !abi->instance_set_rules ||
        !abi->instance_process || !abi->instance_flush || !abi->instance_destroy || !abi->last_error) {
        std::cerr << "Algorithm ABI is incomplete\n";
        return 1;
    }

    av_algo_library_args library_args{};
    library_args.size = sizeof(library_args);
    library_args.api_version = AV_ALGO_API_VERSION;
    library_args.package_root = runner_package_root.value.c_str();
    library_args.platform_id = "macos-arm64-coreml";
    library_args.platform_tag = 0x4D41434F;
    library_args.log = runner_log;
    av_algo_library library = nullptr;
    if (abi->library_open(&library_args, &library) != AV_OK || !library) {
        std::cerr << "library_open failed\n";
        return 1;
    }

    av_algo_library_info library_info{};
    library_info.size = sizeof(library_info);
    library_info.api_version = AV_ALGO_API_VERSION;
    if (abi->library_query(library, &library_info) != AV_OK) {
        abi->library_close(library);
        std::cerr << "library_query failed\n";
        return 1;
    }

    const std::string config = make_config_json(confidence, iou);
    ResultCapture capture;
    av_algo_instance_args instance_args{};
    instance_args.size = sizeof(instance_args);
    instance_args.api_version = AV_ALGO_API_VERSION;
    instance_args.mode = AV_INSTANCE_NORMAL;
    instance_args.instance_id = "standalone";
    instance_args.instance_run_id = "standalone-run";
    instance_args.config_json = config.c_str();
    instance_args.config_json_len = static_cast<uint32_t>(config.size());
    instance_args.on_result = result_callback;
    instance_args.result_user = &capture;
    av_algo_instance instance = nullptr;
    if (abi->instance_create(library, &instance_args, &instance) != AV_OK || !instance) {
        abi->library_close(library);
        std::cerr << "instance_create failed\n";
        return 1;
    }

    av_frame_caps offered{};
    offered.size = sizeof(offered);
    offered.api_version = AV_ALGO_API_VERSION;
    offered.pixel_format_count = 1;
    offered.pixel_formats[0] = AV_PIX_NV12;
    offered.memory_type_count = 1;
    offered.memory_types[0] = AV_MEM_PLATFORM_SURFACE;
    offered.required_opaque_kind = AV_OPAQUE_CVPIXELBUFFER;
    offered.min_width = frame.desc.width;
    offered.min_height = frame.desc.height;
    offered.max_width = frame.desc.width;
    offered.max_height = frame.desc.height;
    av_frame_caps accepted{};
    accepted.size = sizeof(accepted);
    accepted.api_version = AV_ALGO_API_VERSION;
    if (abi->instance_negotiate(instance, &offered, &accepted) != AV_OK) {
        abi->instance_destroy(instance);
        abi->library_close(library);
        std::cerr << "instance_negotiate failed\n";
        return 1;
    }

    std::unique_ptr<yolo26n::CoreMLRunner> direct_runner;
    StageSamples stage_samples;
    if (benchmark) {
        direct_runner = std::make_unique<yolo26n::CoreMLRunner>(runner_log, nullptr);
        const auto staged_model_path = std::filesystem::path(runner_package_root.value) /
            "model/yolo26n.mlpackage";
        if (!direct_runner->load_model(staged_model_path.string())) {
            abi->instance_destroy(instance);
            abi->library_close(library);
            std::cerr << "direct benchmark model load failed\n";
            return 1;
        }
        stage_samples.preprocess_ms.reserve(static_cast<size_t>(loops));
        stage_samples.inference_ms.reserve(static_cast<size_t>(loops));
        stage_samples.postprocess_ms.reserve(static_cast<size_t>(loops));
        stage_samples.end_to_end_ms.reserve(static_cast<size_t>(loops));
    }

    std::vector<double> process_times;
    process_times.reserve(static_cast<size_t>(loops));
    for (int i = 0; i < warmup; ++i) {
        capture = {};
        if (abi->instance_process(instance, &frame.desc) != AV_OK || !capture.error.empty() ||
            (benchmark && !run_direct_pipeline(frame.desc, *direct_runner, confidence, iou, nullptr))) {
            abi->instance_destroy(instance);
            abi->library_close(library);
            std::cerr << "warmup process failed: " << capture.error << '\n';
            return 1;
        }
    }
    if (warmup > 0 && abi->instance_flush(instance) != AV_OK) {
        abi->instance_destroy(instance);
        abi->library_close(library);
        std::cerr << "instance_flush after warmup failed\n";
        return 1;
    }

    for (int i = 0; i < loops; ++i) {
        capture = {};
        const auto begin = std::chrono::steady_clock::now();
        const int status = abi->instance_process(instance, &frame.desc);
        const auto end = std::chrono::steady_clock::now();
        process_times.push_back(std::chrono::duration<double, std::milli>(end - begin).count());
        if (status != AV_OK || !capture.error.empty() ||
            (benchmark && !run_direct_pipeline(frame.desc, *direct_runner, confidence, iou, &stage_samples))) {
            abi->instance_destroy(instance);
            abi->library_close(library);
            std::cerr << "process failed: " << capture.error << '\n';
            return 1;
        }
        if (i == 0) {
            const auto objects = filter_target_classes(capture.objects, target_classes);
            std::cout << "[Detection Result]\n" << capture.json << '\n';
            if (!save_visualization(output_path, frame.desc.width, frame.desc.height, objects, input_path)) {
                abi->instance_destroy(instance);
                abi->library_close(library);
                std::cerr << "failed to write result image: " << output_path << '\n';
                return 1;
            }
            std::cout << "[Visualizer] Saved result image to " << output_path << '\n';
        }
    }

    const int flush_status = abi->instance_flush(instance);
    const int destroy_status = abi->instance_destroy(instance);
    const int close_status = abi->library_close(library);
    if (flush_status != AV_OK || destroy_status != AV_OK || close_status != AV_OK) {
        std::cerr << "algorithm shutdown failed\n";
        return 1;
    }

    const auto stats = argus::utils::BenchmarkStats::compute(process_times);
    if (benchmark) {
        const auto preprocess_stats = argus::utils::BenchmarkStats::compute(stage_samples.preprocess_ms);
        const auto inference_stats = argus::utils::BenchmarkStats::compute(stage_samples.inference_ms);
        const auto postprocess_stats = argus::utils::BenchmarkStats::compute(stage_samples.postprocess_ms);
        const auto end_to_end_stats = argus::utils::BenchmarkStats::compute(stage_samples.end_to_end_ms);
        std::cout << "\n--- Benchmark Report (" << loops << " iterations, " << warmup << " warmup) ---\n"
                  << "  Preprocess:  Avg " << preprocess_stats.avg_ms << " ms | P50 " << preprocess_stats.p50_ms
                  << " ms | P99 " << preprocess_stats.p99_ms << " ms\n"
                  << "  Inference:   Avg " << inference_stats.avg_ms << " ms | P50 " << inference_stats.p50_ms
                  << " ms | P99 " << inference_stats.p99_ms << " ms\n"
                  << "  Postprocess: Avg " << postprocess_stats.avg_ms << " ms | P50 " << postprocess_stats.p50_ms
                  << " ms | P99 " << postprocess_stats.p99_ms << " ms\n"
                  << "  End-to-end:  Avg " << end_to_end_stats.avg_ms << " ms | P50 " << end_to_end_stats.p50_ms
                  << " ms | P99 " << end_to_end_stats.p99_ms << " ms | FPS: " << end_to_end_stats.fps << '\n'
                  << "  ABI process: Avg " << stats.avg_ms << " ms | P50 " << stats.p50_ms
                  << " ms | P99 " << stats.p99_ms << " ms | FPS: " << stats.fps << '\n';
    }
    return 0;
}

} // namespace

int main(int argc, char* argv[]) {
    std::cout << "========================================\n"
              << "  YOLO26n Standalone Runner (macOS ARM64)\n"
              << "========================================\n";
    return run(argc, argv);
}
