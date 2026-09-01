/**
 * @file model_inference.mm
 * @brief YOLOv8n, SCRFD, GLINTR100 Core ML 模型加载与推理实现
 */

#include "model_inference.hpp"

#import <CoreML/CoreML.h>
#import <Foundation/Foundation.h>
#include <filesystem>
#include <cmath>

namespace face_recognition {

class CoreMLRunnerImpl {
public:
    MLModel* yolo_model = nil;
    MLModel* scrfd_model = nil;
    MLModel* glintr_model = nil;

    MLFeatureDescription* yolo_input_desc = nil;
    MLFeatureDescription* scrfd_input_desc = nil;
    MLFeatureDescription* glintr_input_desc = nil;

    NSString* yolo_input_name = nil;
    NSString* yolo_output_name = nil;

    NSString* scrfd_input_name = nil;
    // Map of output names for SCRFD
    NSString* scrfd_score_8_name = nil;
    NSString* scrfd_score_16_name = nil;
    NSString* scrfd_score_32_name = nil;
    NSString* scrfd_bbox_8_name = nil;
    NSString* scrfd_bbox_16_name = nil;
    NSString* scrfd_bbox_32_name = nil;
    NSString* scrfd_kps_8_name = nil;
    NSString* scrfd_kps_16_name = nil;
    NSString* scrfd_kps_32_name = nil;

    NSString* glintr_input_name = nil;
    NSString* glintr_output_name = nil;

    ~CoreMLRunnerImpl() {
        yolo_model = nil;
        scrfd_model = nil;
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

} // namespace

bool ModelInferenceManager::load_models(const std::string& package_root,
                                        const std::string& yolo_rel_path,
                                        const std::string& scrfd_rel_path,
                                        const std::string& glintr_rel_path,
                                        std::string& error) {
    @autoreleasepool {
        std::filesystem::path root(package_root);

        // 1. Load YOLOv8n
        impl_->yolo_model = compile_and_load_model(root / yolo_rel_path, error);
        if (!impl_->yolo_model) return false;

        MLModelDescription* yolo_desc = impl_->yolo_model.modelDescription;
        if (yolo_desc.inputDescriptionsByName.count == 0 || yolo_desc.outputDescriptionsByName.count == 0) {
            error = "yolov8n model has missing inputs or outputs";
            return false;
        }
        impl_->yolo_input_name = yolo_desc.inputDescriptionsByName.allKeys.firstObject;
        impl_->yolo_output_name = yolo_desc.outputDescriptionsByName.allKeys.firstObject;

        // 2. Load SCRFD
        impl_->scrfd_model = compile_and_load_model(root / scrfd_rel_path, error);
        if (!impl_->scrfd_model) return false;

        MLModelDescription* scrfd_desc = impl_->scrfd_model.modelDescription;
        impl_->scrfd_input_name = scrfd_desc.inputDescriptionsByName.allKeys.firstObject;

        for (NSString* out_name in scrfd_desc.outputDescriptionsByName.allKeys) {
            MLFeatureDescription* fd = scrfd_desc.outputDescriptionsByName[out_name];
            NSArray<NSNumber*>* shape = fd.multiArrayConstraint.shape;
            if (shape.count >= 2) {
                NSInteger dim0 = shape[shape.count - 2].integerValue;
                NSInteger dim1 = shape[shape.count - 1].integerValue;
                if ((dim0 == 7680 || dim0 == 12800) && dim1 == 1) impl_->scrfd_score_8_name = out_name;
                else if ((dim0 == 1920 || dim0 == 3200) && dim1 == 1) impl_->scrfd_score_16_name = out_name;
                else if ((dim0 == 480 || dim0 == 800) && dim1 == 1) impl_->scrfd_score_32_name = out_name;
                else if ((dim0 == 7680 || dim0 == 12800) && dim1 == 4) impl_->scrfd_bbox_8_name = out_name;
                else if ((dim0 == 1920 || dim0 == 3200) && dim1 == 4) impl_->scrfd_bbox_16_name = out_name;
                else if ((dim0 == 480 || dim0 == 800) && dim1 == 4) impl_->scrfd_bbox_32_name = out_name;
                else if ((dim0 == 7680 || dim0 == 12800) && dim1 == 10) impl_->scrfd_kps_8_name = out_name;
                else if ((dim0 == 1920 || dim0 == 3200) && dim1 == 10) impl_->scrfd_kps_16_name = out_name;
                else if ((dim0 == 480 || dim0 == 800) && dim1 == 10) impl_->scrfd_kps_32_name = out_name;
            }
        }

        if (!impl_->scrfd_score_8_name || !impl_->scrfd_score_16_name || !impl_->scrfd_score_32_name ||
            !impl_->scrfd_bbox_8_name || !impl_->scrfd_bbox_16_name || !impl_->scrfd_bbox_32_name ||
            !impl_->scrfd_kps_8_name || !impl_->scrfd_kps_16_name || !impl_->scrfd_kps_32_name) {
            error = "failed to map all 9 output heads for SCRFD model";
            return false;
        }

        // 3. Load GLINTR100
        impl_->glintr_model = compile_and_load_model(root / glintr_rel_path, error);
        if (!impl_->glintr_model) return false;

        MLModelDescription* glintr_desc = impl_->glintr_model.modelDescription;
        impl_->glintr_input_name = glintr_desc.inputDescriptionsByName.allKeys.firstObject;
        impl_->glintr_output_name = glintr_desc.outputDescriptionsByName.allKeys.firstObject;

        return true;
    }
}

bool ModelInferenceManager::run_yolo(const uint8_t* rgb_640x384, YoloOutput& out, std::string& error) {
    @autoreleasepool {
        if (!impl_->yolo_model) {
            error = "yolov8n model not loaded";
            return false;
        }

        constexpr int kNetW = 640;
        constexpr int kNetH = 384;
        constexpr int kPixels = kNetW * kNetH;

        // Check if YOLO expects Image or MultiArray
        MLFeatureDescription* input_desc = impl_->yolo_model.modelDescription.inputDescriptionsByName[impl_->yolo_input_name];
        MLFeatureValue* input_val = nil;
        NSError* ns_error = nil;

        if (input_desc.type == MLFeatureTypeImage) {
            // Create 640x384 32BGRA CVPixelBuffer
            NSDictionary* options = @{
                (id)kCVPixelBufferCGImageCompatibilityKey: @YES,
                (id)kCVPixelBufferCGBitmapContextCompatibilityKey: @YES
            };
            CVPixelBufferRef pb = nullptr;
            CVReturn status = CVPixelBufferCreate(
                kCFAllocatorDefault, kNetW, kNetH, kCVPixelFormatType_32BGRA,
                (__bridge CFDictionaryRef)options, &pb);
            if (status != kCVReturnSuccess || !pb) {
                error = "failed to create pixel buffer for YOLO input";
                return false;
            }

            CVPixelBufferLockBaseAddress(pb, 0);
            uint8_t* dst = static_cast<uint8_t*>(CVPixelBufferGetBaseAddress(pb));
            size_t bytes_per_row = CVPixelBufferGetBytesPerRow(pb);
            for (int y = 0; y < kNetH; ++y) {
                uint8_t* row = dst + y * bytes_per_row;
                for (int x = 0; x < kNetW; ++x) {
                    const uint8_t* src_px = rgb_640x384 + (y * kNetW + x) * 3;
                    row[x * 4 + 0] = src_px[2]; // B
                    row[x * 4 + 1] = src_px[1]; // G
                    row[x * 4 + 2] = src_px[0]; // R
                    row[x * 4 + 3] = 255;       // A
                }
            }
            CVPixelBufferUnlockBaseAddress(pb, 0);
            input_val = [MLFeatureValue featureValueWithPixelBuffer:pb];
            CVPixelBufferRelease(pb);
        } else {
            NSArray<NSNumber*>* shape = @[@1, @3, @(kNetH), @(kNetW)];
            MLMultiArray* input_array = [[MLMultiArray alloc] initWithShape:shape dataType:MLMultiArrayDataTypeFloat32 error:&ns_error];
            if (!input_array || ns_error) {
                error = "failed to allocate YOLO input multiarray";
                return false;
            }

            float* dst = static_cast<float*>(input_array.dataPointer);
            for (int c = 0; c < 3; ++c) {
                float* channel_ptr = dst + c * kPixels;
                for (int i = 0; i < kPixels; ++i) {
                    channel_ptr[i] = static_cast<float>(rgb_640x384[i * 3 + c]) / 255.0f;
                }
            }
            input_val = [MLFeatureValue featureValueWithMultiArray:input_array];
        }

        MLDictionaryFeatureProvider* input_features =
            [[MLDictionaryFeatureProvider alloc] initWithDictionary:@{impl_->yolo_input_name: input_val} error:&ns_error];

        id<MLFeatureProvider> prediction = [impl_->yolo_model predictionFromFeatures:input_features error:&ns_error];
        if (!prediction || ns_error) {
            error = "YOLO inference failed: " + std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown");
            return false;
        }

        MLFeatureValue* output_value = [prediction featureValueForName:impl_->yolo_output_name];
        MLMultiArray* output_array = output_value.multiArrayValue;
        if (!output_array) {
            error = "YOLO output feature is missing";
            return false;
        }

        out.data.resize(output_array.count);
        float* src = static_cast<float*>(output_array.dataPointer);
        std::memcpy(out.data.data(), src, output_array.count * sizeof(float));

        return true;
    }
}

bool ModelInferenceManager::run_scrfd(const uint8_t* rgb_640x384, ScrfdOutput& out, std::string& error) {
    @autoreleasepool {
        if (!impl_->scrfd_model) {
            error = "scrfd model not loaded";
            return false;
        }

        constexpr int kNetW = 640;
        constexpr int kNetH = 384;
        constexpr int kPixels = kNetW * kNetH;

        NSError* ns_error = nil;
        NSArray<NSNumber*>* shape = @[@1, @3, @(kNetH), @(kNetW)];
        MLMultiArray* input_array = [[MLMultiArray alloc] initWithShape:shape dataType:MLMultiArrayDataTypeFloat32 error:&ns_error];
        if (!input_array || ns_error) {
            error = "failed to allocate SCRFD input multiarray";
            return false;
        }

        float* dst = static_cast<float*>(input_array.dataPointer);
        // Normalize SCRFD: (x - 127.5f) / 128.0f, RGB planar
        for (int c = 0; c < 3; ++c) {
            float* channel_ptr = dst + c * kPixels;
            for (int i = 0; i < kPixels; ++i) {
                channel_ptr[i] = (static_cast<float>(rgb_640x384[i * 3 + c]) - 127.5f) / 128.0f;
            }
        }

        MLDictionaryFeatureProvider* input_features =
            [[MLDictionaryFeatureProvider alloc] initWithDictionary:@{impl_->scrfd_input_name: input_array} error:&ns_error];

        id<MLFeatureProvider> prediction = [impl_->scrfd_model predictionFromFeatures:input_features error:&ns_error];
        if (!prediction || ns_error) {
            error = "SCRFD inference failed: " + std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown");
            return false;
        }

        const auto copy_head = [&](NSString* name, std::vector<float>& target, size_t dim1) -> bool {
            MLFeatureValue* fv = [prediction featureValueForName:name];
            MLMultiArray* arr = fv.multiArrayValue;
            if (!arr) return false;
            
            size_t total_elements = arr.count;
            size_t dim0 = total_elements / dim1;

            NSInteger row_stride = dim1;
            if (arr.strides.count >= 2) {
                row_stride = arr.strides[arr.strides.count - 2].integerValue;
            } else if (arr.strides.count == 1) {
                row_stride = arr.strides[0].integerValue;
            }

            target.resize(total_elements);
            if (arr.dataType == MLMultiArrayDataTypeFloat16) {
                const _Float16* src16 = static_cast<const _Float16*>(arr.dataPointer);
                for (size_t r = 0; r < dim0; ++r) {
                    const _Float16* row_src = src16 + r * row_stride;
                    float* row_dst = target.data() + r * dim1;
                    for (size_t c = 0; c < dim1; ++c) {
                        row_dst[c] = static_cast<float>(row_src[c]);
                    }
                }
            } else {
                const float* src = static_cast<const float*>(arr.dataPointer);
                if (static_cast<size_t>(row_stride) == dim1) {
                    std::memcpy(target.data(), src, total_elements * sizeof(float));
                } else {
                    for (size_t r = 0; r < dim0; ++r) {
                        std::memcpy(target.data() + r * dim1, src + r * row_stride, dim1 * sizeof(float));
                    }
                }
            }
            return true;
        };

        if (!copy_head(impl_->scrfd_score_8_name, out.score_8, 1) ||
            !copy_head(impl_->scrfd_score_16_name, out.score_16, 1) ||
            !copy_head(impl_->scrfd_score_32_name, out.score_32, 1) ||
            !copy_head(impl_->scrfd_bbox_8_name, out.bbox_8, 4) ||
            !copy_head(impl_->scrfd_bbox_16_name, out.bbox_16, 4) ||
            !copy_head(impl_->scrfd_bbox_32_name, out.bbox_32, 4) ||
            !copy_head(impl_->scrfd_kps_8_name, out.kps_8, 10) ||
            !copy_head(impl_->scrfd_kps_16_name, out.kps_16, 10) ||
            !copy_head(impl_->scrfd_kps_32_name, out.kps_32, 10)) {
            error = "failed to copy SCRFD output head tensors";
            return false;
        }

        return true;
    }
}

bool ModelInferenceManager::run_glintr(const uint8_t* rgb_112x112, GlintrOutput& out, std::string& error) {
    @autoreleasepool {
        if (!impl_->glintr_model) {
            error = "glintr model not loaded";
            return false;
        }

        NSError* ns_error = nil;
        NSArray<NSNumber*>* shape = @[@1, @3, @112, @112];
        MLMultiArray* input_array = [[MLMultiArray alloc] initWithShape:shape dataType:MLMultiArrayDataTypeFloat32 error:&ns_error];
        if (!input_array || ns_error) {
            error = "failed to allocate GLINTR input multiarray";
            return false;
        }

        float* dst = static_cast<float*>(input_array.dataPointer);
        // Normalize GLINTR: (x - 127.5f) / 127.5f, RGB planar
        for (int c = 0; c < 3; ++c) {
            float* channel_ptr = dst + c * (112 * 112);
            for (int i = 0; i < 112 * 112; ++i) {
                channel_ptr[i] = (static_cast<float>(rgb_112x112[i * 3 + c]) - 127.5f) / 127.5f;
            }
        }

        MLDictionaryFeatureProvider* input_features =
            [[MLDictionaryFeatureProvider alloc] initWithDictionary:@{impl_->glintr_input_name: input_array} error:&ns_error];

        id<MLFeatureProvider> prediction = [impl_->glintr_model predictionFromFeatures:input_features error:&ns_error];
        if (!prediction || ns_error) {
            error = "GLINTR inference failed: " + std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown");
            return false;
        }

        MLFeatureValue* output_value = [prediction featureValueForName:impl_->glintr_output_name];
        MLMultiArray* output_array = output_value.multiArrayValue;
        if (!output_array || output_array.count != 512) {
            error = "GLINTR output must have exactly 512 dimensions";
            return false;
        }

        out.embedding.resize(512);
        if (output_array.dataType == MLMultiArrayDataTypeFloat16) {
            const _Float16* src16 = static_cast<const _Float16*>(output_array.dataPointer);
            for (size_t i = 0; i < 512; ++i) {
                out.embedding[i] = static_cast<float>(src16[i]);
            }
        } else {
            const float* src = static_cast<const float*>(output_array.dataPointer);
            std::memcpy(out.embedding.data(), src, 512 * sizeof(float));
        }

        return true;
    }
}

} // namespace face_recognition
