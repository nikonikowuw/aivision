/**
 * @file model_inference.mm
 * @brief YOLOv8n, SCRFD, GLINTR100 Core ML 模型加载与推理实现
 */

#include "model_inference.hpp"

#import <CoreML/CoreML.h>
#import <CoreVideo/CoreVideo.h>
#import <VideoToolbox/VideoToolbox.h>
#import <Accelerate/Accelerate.h>
#import <Foundation/Foundation.h>
#include <filesystem>
#include <cmath>
#include <iostream>

namespace face_recognition {

struct ScrfdHeadNames {
    NSString* input_name = nil;
    NSString* score_8_name = nil;
    NSString* score_16_name = nil;
    NSString* score_32_name = nil;
    NSString* bbox_8_name = nil;
    NSString* bbox_16_name = nil;
    NSString* bbox_32_name = nil;
    NSString* kps_8_name = nil;
    NSString* kps_16_name = nil;
    NSString* kps_32_name = nil;

    bool map_heads(MLModel* model, std::string& error) {
        if (!model) {
            error = "SCRFD model is nil";
            return false;
        }
        MLModelDescription* desc = model.modelDescription;
        input_name = desc.inputDescriptionsByName.allKeys.firstObject;

        for (NSString* out_name in desc.outputDescriptionsByName.allKeys) {
            MLFeatureDescription* fd = desc.outputDescriptionsByName[out_name];
            NSArray<NSNumber*>* shape = fd.multiArrayConstraint.shape;
            if (shape.count >= 2) {
                NSInteger dim0 = shape[shape.count - 2].integerValue;
                NSInteger dim1 = shape[shape.count - 1].integerValue;
                if ((dim0 == 7680 || dim0 == 12800) && dim1 == 1) score_8_name = out_name;
                else if ((dim0 == 1920 || dim0 == 3200) && dim1 == 1) score_16_name = out_name;
                else if ((dim0 == 480 || dim0 == 800) && dim1 == 1) score_32_name = out_name;
                else if ((dim0 == 7680 || dim0 == 12800) && dim1 == 4) bbox_8_name = out_name;
                else if ((dim0 == 1920 || dim0 == 3200) && dim1 == 4) bbox_16_name = out_name;
                else if ((dim0 == 480 || dim0 == 800) && dim1 == 4) bbox_32_name = out_name;
                else if ((dim0 == 7680 || dim0 == 12800) && dim1 == 10) kps_8_name = out_name;
                else if ((dim0 == 1920 || dim0 == 3200) && dim1 == 10) kps_16_name = out_name;
                else if ((dim0 == 480 || dim0 == 800) && dim1 == 10) kps_32_name = out_name;
            }
        }

        if (!score_8_name || !score_16_name || !score_32_name ||
            !bbox_8_name || !bbox_16_name || !bbox_32_name ||
            !kps_8_name || !kps_16_name || !kps_32_name) {
            error = "failed to map all 9 output heads for SCRFD model";
            return false;
        }
        return true;
    }
};

class CoreMLRunnerImpl {
public:
    MLModel* yolo_model = nil;
    MLModel* scrfd_model = nil;
    MLModel* scrfd_reg_model = nil;
    MLModel* glintr_model = nil;

    NSString* yolo_input_name = nil;
    NSString* yolo_output_name = nil;

    ScrfdHeadNames scrfd_heads;
    ScrfdHeadNames scrfd_reg_heads;

    NSString* glintr_input_name = nil;
    NSString* glintr_output_name = nil;

    CVPixelBufferRef scrfd_cached_pb = nullptr;
    CVPixelBufferRef scrfd_reg_cached_pb = nullptr;
    CVPixelBufferRef glintr_cached_pb = nullptr;
    CVPixelBufferRef yolo_cached_pb = nullptr;

    ~CoreMLRunnerImpl() {
        if (scrfd_cached_pb) { CVPixelBufferRelease(scrfd_cached_pb); scrfd_cached_pb = nullptr; }
        if (scrfd_reg_cached_pb) { CVPixelBufferRelease(scrfd_reg_cached_pb); scrfd_reg_cached_pb = nullptr; }
        if (glintr_cached_pb) { CVPixelBufferRelease(glintr_cached_pb); glintr_cached_pb = nullptr; }
        if (yolo_cached_pb) { CVPixelBufferRelease(yolo_cached_pb); yolo_cached_pb = nullptr; }
        yolo_model = nil;
        scrfd_model = nil;
        scrfd_reg_model = nil;
        glintr_model = nil;
    }
};

ModelInferenceManager::ModelInferenceManager() : impl_(std::make_unique<CoreMLRunnerImpl>()) {}
ModelInferenceManager::~ModelInferenceManager() = default;

namespace {

MLModel* compile_and_load_model(const std::filesystem::path& model_path, std::string& error) {
    @autoreleasepool {
        if (!std::filesystem::exists(model_path)) {
            error = "model path does not exist: " + model_path.string();
            return nil;
        }

        NSURL* model_url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:model_path.c_str()]];
        NSError* ns_error = nil;

        // Compile .mlpackage to .mlmodelc if needed
        NSURL* compiled_url = model_url;
        if ([model_url.pathExtension isEqualToString:@"mlpackage"]) {
            compiled_url = [MLModel compileModelAtURL:model_url error:&ns_error];
            if (!compiled_url || ns_error) {
                error = "failed to compile Core ML model: " +
                        std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown error");
                return nil;
            }
        }

        MLModelConfiguration* config = [[MLModelConfiguration alloc] init];
        config.computeUnits = MLComputeUnitsAll;

        MLModel* model = [MLModel modelWithContentsOfURL:compiled_url configuration:config error:&ns_error];
        if (!model || ns_error) {
            error = "failed to load Core ML model: " +
                    std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown error");
            return nil;
        }
        return model;
    }
}

bool copy_multiarray_to_float_vector(MLMultiArray* arr, std::vector<float>& target, size_t /*dim1*/ = 0) {
    if (!arr) return false;
    const size_t total_elements = static_cast<size_t>(arr.count);
    target.resize(total_elements);
    if (total_elements == 0) return true;

    // 优先检查内存布局是否连续（C 风格行优先）
    bool is_contiguous = true;
    NSInteger expected_stride = 1;
    for (NSInteger i = static_cast<NSInteger>(arr.shape.count) - 1; i >= 0; --i) {
        if (arr.strides[i].integerValue != expected_stride) {
            is_contiguous = false;
            break;
        }
        expected_stride *= arr.shape[i].integerValue;
    }

    if (is_contiguous) {
        if (arr.dataType == MLMultiArrayDataTypeFloat32) {
            std::memcpy(target.data(), arr.dataPointer, total_elements * sizeof(float));
            return true;
        }
        if (arr.dataType == MLMultiArrayDataTypeFloat16) {
            const _Float16* src16 = static_cast<const _Float16*>(arr.dataPointer);
            for (size_t i = 0; i < total_elements; ++i) {
                target[i] = static_cast<float>(src16[i]);
            }
            return true;
        }
        if (arr.dataType == MLMultiArrayDataTypeDouble) {
            const double* src64 = static_cast<const double*>(arr.dataPointer);
            for (size_t i = 0; i < total_elements; ++i) {
                target[i] = static_cast<float>(src64[i]);
            }
            return true;
        }
    }

    // 通用 N 维跨距遍历
    const size_t ndim = static_cast<size_t>(arr.shape.count);
    if (ndim == 0) return false;

    std::vector<NSInteger> shape(ndim);
    std::vector<NSInteger> strides(ndim);
    for (size_t i = 0; i < ndim; ++i) {
        shape[i] = arr.shape[i].integerValue;
        strides[i] = arr.strides[i].integerValue;
    }

    std::vector<NSInteger> coords(ndim, 0);
    for (size_t idx = 0; idx < total_elements; ++idx) {
        NSInteger offset = 0;
        for (size_t d = 0; d < ndim; ++d) {
            offset += coords[d] * strides[d];
        }
        if (arr.dataType == MLMultiArrayDataTypeFloat32) {
            target[idx] = static_cast<const float*>(arr.dataPointer)[offset];
        } else if (arr.dataType == MLMultiArrayDataTypeFloat16) {
            target[idx] = static_cast<float>(static_cast<const _Float16*>(arr.dataPointer)[offset]);
        } else if (arr.dataType == MLMultiArrayDataTypeDouble) {
            target[idx] = static_cast<float>(static_cast<const double*>(arr.dataPointer)[offset]);
        }
        // 推进 N 维坐标
        for (int d = static_cast<int>(ndim) - 1; d >= 0; --d) {
            coords[d]++;
            if (coords[d] < shape[d] || d == 0) {
                break;
            }
            coords[d] = 0;
        }
    }
    return true;
}

static MLFeatureValue* prepare_input_feature_value(
    MLFeatureDescription* input_desc,
    CVPixelBufferRef pb,
    const uint8_t* rgb,
    int width,
    int height,
    float norm_min,
    float norm_max,
    CVPixelBufferRef& cached_pb,
    std::string& error) {
    @autoreleasepool {
        if (!input_desc) {
            error = "null input feature description";
            return nil;
        }

        if (input_desc.type == MLFeatureTypeImage) {
            if (pb) {
                return [MLFeatureValue featureValueWithPixelBuffer:pb];
            }
            if (!rgb) {
                error = "both pixel buffer and RGB buffer are null";
                return nil;
            }

            if (!cached_pb ||
                CVPixelBufferGetWidth(cached_pb) != static_cast<size_t>(width) ||
                CVPixelBufferGetHeight(cached_pb) != static_cast<size_t>(height)) {
                if (cached_pb) {
                    CVPixelBufferRelease(cached_pb);
                    cached_pb = nullptr;
                }
                NSDictionary* options = @{
                    (id)kCVPixelBufferCGImageCompatibilityKey: @YES,
                    (id)kCVPixelBufferCGBitmapContextCompatibilityKey: @YES,
                    (id)kCVPixelBufferMetalCompatibilityKey: @YES
                };
                CVReturn status = CVPixelBufferCreate(
                    kCFAllocatorDefault, width, height, kCVPixelFormatType_32BGRA,
                    (__bridge CFDictionaryRef)options, &cached_pb
                );
                if (status != kCVReturnSuccess || !cached_pb) {
                    error = "failed to create destination BGRA pixel buffer";
                    return nil;
                }
            }

            CVPixelBufferLockBaseAddress(cached_pb, 0);
            uint8_t* dst = static_cast<uint8_t*>(CVPixelBufferGetBaseAddress(cached_pb));
            size_t bpr = CVPixelBufferGetBytesPerRow(cached_pb);

            vImage_Buffer src_buf = {
                .data = const_cast<uint8_t*>(rgb),
                .height = (vImagePixelCount)height,
                .width = (vImagePixelCount)width,
                .rowBytes = (size_t)width * 3
            };
            vImage_Buffer dst_buf = {
                .data = dst,
                .height = (vImagePixelCount)height,
                .width = (vImagePixelCount)width,
                .rowBytes = bpr
            };

            vImageConvert_RGB888toBGRA8888(&src_buf, nullptr, 255, &dst_buf, false, kvImageNoFlags);
            CVPixelBufferUnlockBaseAddress(cached_pb, 0);

            return [MLFeatureValue featureValueWithPixelBuffer:cached_pb];
        }

        if (input_desc.type == MLFeatureTypeMultiArray) {
            const int pixels = width * height;
            NSArray<NSNumber*>* shape = @[@1, @3, @(height), @(width)];
            NSError* ns_error = nil;
            MLMultiArray* input_array = [[MLMultiArray alloc] initWithShape:shape dataType:MLMultiArrayDataTypeFloat32 error:&ns_error];
            if (!input_array || ns_error) {
                error = "failed to allocate input MLMultiArray: " +
                        std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown error");
                return nil;
            }

            float* dst = static_cast<float*>(input_array.dataPointer);
            vImage_Buffer dst_rf = { .data = dst + 0 * pixels, .height = (vImagePixelCount)height, .width = (vImagePixelCount)width, .rowBytes = (size_t)width * sizeof(float) };
            vImage_Buffer dst_gf = { .data = dst + 1 * pixels, .height = (vImagePixelCount)height, .width = (vImagePixelCount)width, .rowBytes = (size_t)width * sizeof(float) };
            vImage_Buffer dst_bf = { .data = dst + 2 * pixels, .height = (vImagePixelCount)height, .width = (vImagePixelCount)width, .rowBytes = (size_t)width * sizeof(float) };

            if (pb) {
                CVPixelBufferLockBaseAddress(pb, kCVPixelBufferLock_ReadOnly);
                uint8_t* base = static_cast<uint8_t*>(CVPixelBufferGetBaseAddress(pb));
                size_t bpr = CVPixelBufferGetBytesPerRow(pb);
                OSType fmt = CVPixelBufferGetPixelFormatType(pb);

                vImage_Buffer src_buf = {
                    .data = base,
                    .height = (vImagePixelCount)height,
                    .width = (vImagePixelCount)width,
                    .rowBytes = bpr
                };

                std::vector<uint8_t> p0(pixels), p1(pixels), p2(pixels), p3(pixels);
                vImage_Buffer buf0 = { .data = p0.data(), .height = (vImagePixelCount)height, .width = (vImagePixelCount)width, .rowBytes = (size_t)width };
                vImage_Buffer buf1 = { .data = p1.data(), .height = (vImagePixelCount)height, .width = (vImagePixelCount)width, .rowBytes = (size_t)width };
                vImage_Buffer buf2 = { .data = p2.data(), .height = (vImagePixelCount)height, .width = (vImagePixelCount)width, .rowBytes = (size_t)width };
                vImage_Buffer buf3 = { .data = p3.data(), .height = (vImagePixelCount)height, .width = (vImagePixelCount)width, .rowBytes = (size_t)width };

                vImageConvert_ARGB8888toPlanar8(&src_buf, &buf0, &buf1, &buf2, &buf3, kvImageNoFlags);
                CVPixelBufferUnlockBaseAddress(pb, kCVPixelBufferLock_ReadOnly);

                // 根据 PixelFormat 判断通道顺序
                // kCVPixelFormatType_32BGRA 内存顺序为 B, G, R, A => buf0=B, buf1=G, buf2=R, buf3=A
                // kCVPixelFormatType_32RGBA 内存顺序为 R, G, B, A => buf0=R, buf1=G, buf2=B, buf3=A
                const vImage_Buffer* plane_r = (fmt == kCVPixelFormatType_32BGRA) ? &buf2 : &buf0;
                const vImage_Buffer* plane_g = &buf1;
                const vImage_Buffer* plane_b = (fmt == kCVPixelFormatType_32BGRA) ? &buf0 : &buf2;

                vImageConvert_Planar8toPlanarF(plane_r, &dst_rf, norm_max, norm_min, kvImageNoFlags);
                vImageConvert_Planar8toPlanarF(plane_g, &dst_gf, norm_max, norm_min, kvImageNoFlags);
                vImageConvert_Planar8toPlanarF(plane_b, &dst_bf, norm_max, norm_min, kvImageNoFlags);
            } else if (rgb) {
                vImage_Buffer src_rgb = {
                    .data = const_cast<uint8_t*>(rgb),
                    .height = (vImagePixelCount)height,
                    .width = (vImagePixelCount)width,
                    .rowBytes = (size_t)width * 3
                };
                std::vector<uint8_t> pr(pixels), pg(pixels), pb_plane(pixels);
                vImage_Buffer br = { .data = pr.data(), .height = (vImagePixelCount)height, .width = (vImagePixelCount)width, .rowBytes = (size_t)width };
                vImage_Buffer bg = { .data = pg.data(), .height = (vImagePixelCount)height, .width = (vImagePixelCount)width, .rowBytes = (size_t)width };
                vImage_Buffer bb = { .data = pb_plane.data(), .height = (vImagePixelCount)height, .width = (vImagePixelCount)width, .rowBytes = (size_t)width };

                vImageConvert_RGB888toPlanar8(&src_rgb, &br, &bg, &bb, kvImageNoFlags);

                vImageConvert_Planar8toPlanarF(&br, &dst_rf, norm_max, norm_min, kvImageNoFlags);
                vImageConvert_Planar8toPlanarF(&bg, &dst_gf, norm_max, norm_min, kvImageNoFlags);
                vImageConvert_Planar8toPlanarF(&bb, &dst_bf, norm_max, norm_min, kvImageNoFlags);
            } else {
                error = "both pixel buffer and RGB buffer are null";
                return nil;
            }

            return [MLFeatureValue featureValueWithMultiArray:input_array];
        }

        error = "unsupported input feature type: " + std::to_string(input_desc.type);
        return nil;
    }
}

static bool run_scrfd_internal(MLModel* model,
                               const ScrfdHeadNames& heads,
                               CVPixelBufferRef pb,
                               const uint8_t* rgb,
                               int net_w,
                               int net_h,
                               CVPixelBufferRef& cached_pb,
                               ScrfdOutput& out,
                               std::string& error) {
    @autoreleasepool {
        if (!model) {
            error = "SCRFD model not loaded";
            return false;
        }

        MLFeatureDescription* input_desc = model.modelDescription.inputDescriptionsByName[heads.input_name];
        // SCRFD 归一化: (x - 127.5) / 128.0 => min = -127.5/128.0, max = 127.5/128.0
        float norm_min = (0.0f - 127.5f) / 128.0f;
        float norm_max = (255.0f - 127.5f) / 128.0f;

        MLFeatureValue* input_val = prepare_input_feature_value(
            input_desc, pb, rgb, net_w, net_h, norm_min, norm_max, cached_pb, error
        );
        if (!input_val) return false;

        NSError* ns_error = nil;
        NSDictionary* input_dict = @{heads.input_name: input_val};
        id<MLFeatureProvider> input_provider = [[MLDictionaryFeatureProvider alloc] initWithDictionary:input_dict error:&ns_error];
        if (!input_provider || ns_error) {
            error = "failed to create SCRFD feature provider: " +
                    std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown error");
            return false;
        }

        id<MLFeatureProvider> output_provider = [model predictionFromFeatures:input_provider error:&ns_error];
        if (!output_provider || ns_error) {
            error = "failed to run SCRFD prediction: " +
                    std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown error");
            return false;
        }

        auto copy_head = [&](NSString* name, std::vector<float>& dst_vec, size_t dim1) -> bool {
            MLFeatureValue* fv = [output_provider featureValueForName:name];
            if (!fv || fv.type != MLFeatureTypeMultiArray) return false;
            return copy_multiarray_to_float_vector(fv.multiArrayValue, dst_vec, dim1);
        };

        if (!copy_head(heads.score_8_name, out.score_8, 1) ||
            !copy_head(heads.score_16_name, out.score_16, 1) ||
            !copy_head(heads.score_32_name, out.score_32, 1) ||
            !copy_head(heads.bbox_8_name, out.bbox_8, 4) ||
            !copy_head(heads.bbox_16_name, out.bbox_16, 4) ||
            !copy_head(heads.bbox_32_name, out.bbox_32, 4) ||
            !copy_head(heads.kps_8_name, out.kps_8, 10) ||
            !copy_head(heads.kps_16_name, out.kps_16, 10) ||
            !copy_head(heads.kps_32_name, out.kps_32, 10)) {
            error = "failed to copy one or more SCRFD output arrays";
            return false;
        }

        return true;
    }
}

} // namespace

bool ModelInferenceManager::load_models(const std::string& package_root,
                                        const std::string& yolo_rel_path,
                                        const std::string& scrfd_rel_path,
                                        const std::string& scrfd_reg_rel_path,
                                        const std::string& glintr_rel_path,
                                        std::string& error) {
    @autoreleasepool {
        std::filesystem::path root(package_root);

        // 1. Load YOLOv8n / YOLO26n (可选能力)
        std::string actual_yolo_path = yolo_rel_path;
        if (!actual_yolo_path.empty() && !std::filesystem::exists(root / actual_yolo_path)) {
            if (std::filesystem::exists(root / "model/yolo26n.mlpackage")) {
                actual_yolo_path = "model/yolo26n.mlpackage";
            }
        }
        if (!actual_yolo_path.empty() && std::filesystem::exists(root / actual_yolo_path)) {
            std::string yolo_err;
            impl_->yolo_model = compile_and_load_model(root / actual_yolo_path, yolo_err);
            if (impl_->yolo_model) {
                MLModelDescription* yolo_desc = impl_->yolo_model.modelDescription;
                if (yolo_desc.inputDescriptionsByName.count > 0 && yolo_desc.outputDescriptionsByName.count > 0) {
                    impl_->yolo_input_name = yolo_desc.inputDescriptionsByName.allKeys.firstObject;
                    impl_->yolo_output_name = yolo_desc.outputDescriptionsByName.allKeys.firstObject;
                }
            }
        }

        // 2. Load SCRFD Stream Model (必选流媒体人脸检测模型，384x640)
        impl_->scrfd_model = compile_and_load_model(root / scrfd_rel_path, error);
        if (!impl_->scrfd_model) return false;

        if (!impl_->scrfd_heads.map_heads(impl_->scrfd_model, error)) {
            return false;
        }

        // 2.1 Load SCRFD Registration Model (可选/推荐 640x640 静态注册人脸检测模型)
        if (!scrfd_reg_rel_path.empty() && std::filesystem::exists(root / scrfd_reg_rel_path)) {
            std::string reg_err;
            impl_->scrfd_reg_model = compile_and_load_model(root / scrfd_reg_rel_path, reg_err);
            if (impl_->scrfd_reg_model) {
                if (!impl_->scrfd_reg_heads.map_heads(impl_->scrfd_reg_model, reg_err)) {
                    impl_->scrfd_reg_model = nil;
                }
            }
        }

        // 3. Load GLINTR100 (必选人脸特征提取模型)
        impl_->glintr_model = compile_and_load_model(root / glintr_rel_path, error);
        if (!impl_->glintr_model) return false;

        MLModelDescription* glintr_desc = impl_->glintr_model.modelDescription;
        impl_->glintr_input_name = glintr_desc.inputDescriptionsByName.allKeys.firstObject;
        impl_->glintr_output_name = glintr_desc.outputDescriptionsByName.allKeys.firstObject;

        return true;
    }
}

static bool run_yolo_internal(MLModel* model,
                              NSString* input_name,
                              NSString* output_name,
                              CVPixelBufferRef pb,
                              const uint8_t* rgb,
                              CVPixelBufferRef& cached_pb,
                              YoloOutput& out,
                              std::string& error) {
    @autoreleasepool {
        if (!model) {
            error = "yolov8n model not loaded";
            return false;
        }

        constexpr int kNetW = 640;
        constexpr int kNetH = 384;

        MLFeatureDescription* input_desc = model.modelDescription.inputDescriptionsByName[input_name];
        // YOLO 归一化: x / 255.0 => min = 0.0, max = 1.0
        float norm_min = 0.0f;
        float norm_max = 1.0f;

        MLFeatureValue* input_val = prepare_input_feature_value(
            input_desc, pb, rgb, kNetW, kNetH, norm_min, norm_max, cached_pb, error
        );
        if (!input_val) return false;

        NSError* ns_error = nil;
        NSDictionary* input_dict = @{input_name: input_val};
        id<MLFeatureProvider> input_provider = [[MLDictionaryFeatureProvider alloc] initWithDictionary:input_dict error:&ns_error];
        if (!input_provider || ns_error) {
            error = "failed to create YOLO feature provider: " +
                    std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown error");
            return false;
        }

        id<MLFeatureProvider> output_provider = [model predictionFromFeatures:input_provider error:&ns_error];
        if (!output_provider || ns_error) {
            error = "failed to run YOLO prediction: " +
                    std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown error");
            return false;
        }

        MLFeatureValue* out_val = [output_provider featureValueForName:output_name];
        if (!out_val || out_val.type != MLFeatureTypeMultiArray) {
            error = "invalid YOLO output multiarray";
            return false;
        }

        if (!copy_multiarray_to_float_vector(out_val.multiArrayValue, out.data, 84)) {
            error = "failed to copy YOLO output";
            return false;
        }

        return true;
    }
}

static bool run_glintr_internal(MLModel* model,
                                NSString* input_name,
                                NSString* output_name,
                                CVPixelBufferRef pb,
                                const uint8_t* rgb,
                                CVPixelBufferRef& cached_pb,
                                GlintrOutput& out,
                                std::string& error) {
    @autoreleasepool {
        if (!model) {
            error = "GLINTR model not loaded";
            return false;
        }

        constexpr int kNetDim = 112;

        MLFeatureDescription* input_desc = model.modelDescription.inputDescriptionsByName[input_name];
        // GLINTR 归一化: (x - 127.5) / 127.5 => min = -1.0, max = 1.0
        float norm_min = (0.0f - 127.5f) / 127.5f;
        float norm_max = (255.0f - 127.5f) / 127.5f;

        MLFeatureValue* input_val = prepare_input_feature_value(
            input_desc, pb, rgb, kNetDim, kNetDim, norm_min, norm_max, cached_pb, error
        );
        if (!input_val) return false;

        NSError* ns_error = nil;
        NSDictionary* input_dict = @{input_name: input_val};
        id<MLFeatureProvider> input_provider = [[MLDictionaryFeatureProvider alloc] initWithDictionary:input_dict error:&ns_error];
        if (!input_provider || ns_error) {
            error = "failed to create GLINTR feature provider: " +
                    std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown error");
            return false;
        }

        id<MLFeatureProvider> output_provider = [model predictionFromFeatures:input_provider error:&ns_error];
        if (!output_provider || ns_error) {
            error = "failed to run GLINTR prediction: " +
                    std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown error");
            return false;
        }

        MLFeatureValue* out_val = [output_provider featureValueForName:output_name];
        if (!out_val || out_val.type != MLFeatureTypeMultiArray) {
            error = "invalid GLINTR output multiarray";
            return false;
        }

        if (!copy_multiarray_to_float_vector(out_val.multiArrayValue, out.embedding, 512)) {
            error = "failed to copy GLINTR output";
            return false;
        }

        if (out.embedding.size() != 512) {
            error = "unexpected GLINTR embedding dimension (expected 512, got " + std::to_string(out.embedding.size()) + ")";
            return false;
        }

        return true;
    }
}

bool ModelInferenceManager::run_yolo(CVPixelBufferRef pixel_buffer, YoloOutput& out, std::string& error) {
    return run_yolo_internal(impl_->yolo_model, impl_->yolo_input_name, impl_->yolo_output_name, pixel_buffer, nullptr, impl_->yolo_cached_pb, out, error);
}

bool ModelInferenceManager::run_yolo(const uint8_t* rgb_640x384, YoloOutput& out, std::string& error) {
    return run_yolo_internal(impl_->yolo_model, impl_->yolo_input_name, impl_->yolo_output_name, nullptr, rgb_640x384, impl_->yolo_cached_pb, out, error);
}

bool ModelInferenceManager::run_scrfd(CVPixelBufferRef pixel_buffer, ScrfdOutput& out, std::string& error) {
    return run_scrfd_internal(impl_->scrfd_model, impl_->scrfd_heads, pixel_buffer, nullptr, 640, 384, impl_->scrfd_cached_pb, out, error);
}

bool ModelInferenceManager::run_scrfd(const uint8_t* rgb_640x384, ScrfdOutput& out, std::string& error) {
    return run_scrfd_internal(impl_->scrfd_model, impl_->scrfd_heads, nullptr, rgb_640x384, 640, 384, impl_->scrfd_cached_pb, out, error);
}

bool ModelInferenceManager::run_scrfd_reg(CVPixelBufferRef pixel_buffer, ScrfdOutput& out, std::string& error) {
    if (!impl_->scrfd_reg_model) {
        error = "SCRFD 640x640 registration model not loaded";
        return false;
    }
    return run_scrfd_internal(impl_->scrfd_reg_model, impl_->scrfd_reg_heads, pixel_buffer, nullptr, 640, 640, impl_->scrfd_reg_cached_pb, out, error);
}

bool ModelInferenceManager::run_scrfd_reg(const uint8_t* rgb_640x640, ScrfdOutput& out, std::string& error) {
    if (!impl_->scrfd_reg_model) {
        error = "SCRFD 640x640 registration model not loaded";
        return false;
    }
    return run_scrfd_internal(impl_->scrfd_reg_model, impl_->scrfd_reg_heads, nullptr, rgb_640x640, 640, 640, impl_->scrfd_reg_cached_pb, out, error);
}

bool ModelInferenceManager::run_glintr(CVPixelBufferRef pixel_buffer, GlintrOutput& out, std::string& error) {
    return run_glintr_internal(impl_->glintr_model, impl_->glintr_input_name, impl_->glintr_output_name, pixel_buffer, nullptr, impl_->glintr_cached_pb, out, error);
}

bool ModelInferenceManager::run_glintr(const uint8_t* rgb_112x112, GlintrOutput& out, std::string& error) {
    return run_glintr_internal(impl_->glintr_model, impl_->glintr_input_name, impl_->glintr_output_name, nullptr, rgb_112x112, impl_->glintr_cached_pb, out, error);
}

} // namespace face_recognition
