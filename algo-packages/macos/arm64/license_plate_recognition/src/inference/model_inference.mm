/**
 * @file model_inference.mm
 * @brief 车牌检测 (YOLOv5-plate) 与通用多语言文本识别 (PP-OCRv4) Core ML 推理实现
 */

#include "model_inference.hpp"

#import <CoreML/CoreML.h>
#import <Foundation/Foundation.h>
#include <filesystem>
#include <cmath>

namespace lpr {

class CoreMLRunnerImpl {
public:
    MLModel* detect_model = nil;
    MLModel* rec_model = nil;

    NSString* detect_input_name = nil;
    NSString* detect_output_name = nil;
    MLMultiArray* detect_input_array = nil;
    MLDictionaryFeatureProvider* detect_input_features = nil;

    NSString* rec_input_name = nil;
    NSString* rec_output_name = nil;
    MLMultiArray* rec_input_array = nil;
    MLDictionaryFeatureProvider* rec_input_features = nil;

    ~CoreMLRunnerImpl() {
        detect_model = nil;
        rec_model = nil;
        detect_input_array = nil;
        detect_input_features = nil;
        rec_input_array = nil;
        rec_input_features = nil;
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
                                        const std::string& detect_rel_path,
                                        const std::string& rec_rel_path,
                                        std::string& error) {
    @autoreleasepool {
        std::filesystem::path root(package_root);
        NSError* ns_error = nil;

        // 1. Load Plate Detector & Pre-allocate Input MultiArray
        impl_->detect_model = compile_and_load_model(root / detect_rel_path, error);
        if (!impl_->detect_model) return false;

        MLModelDescription* detect_desc = impl_->detect_model.modelDescription;
        impl_->detect_input_name = detect_desc.inputDescriptionsByName.allKeys.firstObject;
        impl_->detect_output_name = detect_desc.outputDescriptionsByName.allKeys.firstObject;

        NSArray<NSNumber*>* detect_shape = @[@1, @3, @384, @640];
        impl_->detect_input_array = [[MLMultiArray alloc] initWithShape:detect_shape
                                                               dataType:MLMultiArrayDataTypeFloat32
                                                                  error:&ns_error];
        if (!impl_->detect_input_array || ns_error) {
            error = "failed to preallocate detect input multiarray";
            return false;
        }
        MLFeatureValue* detect_in_val = [MLFeatureValue featureValueWithMultiArray:impl_->detect_input_array];
        impl_->detect_input_features = [[MLDictionaryFeatureProvider alloc] initWithDictionary:@{impl_->detect_input_name: detect_in_val}
                                                                                          error:&ns_error];

        // 2. Load Universal PP-OCRv4 Model & Pre-allocate Input MultiArray
        impl_->rec_model = compile_and_load_model(root / rec_rel_path, error);
        if (!impl_->rec_model) return false;

        MLModelDescription* rec_desc = impl_->rec_model.modelDescription;
        impl_->rec_input_name = rec_desc.inputDescriptionsByName.allKeys.firstObject;
        impl_->rec_output_name = rec_desc.outputDescriptionsByName.allKeys.firstObject;

        NSArray<NSNumber*>* rec_shape = @[@1, @3, @48, @320];
        impl_->rec_input_array = [[MLMultiArray alloc] initWithShape:rec_shape
                                                            dataType:MLMultiArrayDataTypeFloat32
                                                               error:&ns_error];
        if (!impl_->rec_input_array || ns_error) {
            error = "failed to preallocate rec input multiarray";
            return false;
        }
        MLFeatureValue* rec_in_val = [MLFeatureValue featureValueWithMultiArray:impl_->rec_input_array];
        impl_->rec_input_features = [[MLDictionaryFeatureProvider alloc] initWithDictionary:@{impl_->rec_input_name: rec_in_val}
                                                                                      error:&ns_error];

        return true;
    }
}

bool ModelInferenceManager::run_detect(const uint8_t* rgb_640x384, PlateDetectOutput& out, std::string& error) {
    @autoreleasepool {
        if (!impl_->detect_model || !impl_->detect_input_array || !impl_->detect_input_features) {
            error = "detect model not loaded or uninitialized";
            return false;
        }

        NSError* ns_error = nil;
        float* dst = static_cast<float*>(impl_->detect_input_array.dataPointer);
        constexpr float kScale = 1.0f / 255.0f;
        constexpr int kPlaneSize = 640 * 384;

        float* dst_r = dst;
        float* dst_g = dst + kPlaneSize;
        float* dst_b = dst + 2 * kPlaneSize;

        for (int i = 0; i < kPlaneSize; ++i) {
            dst_r[i] = static_cast<float>(rgb_640x384[i * 3 + 0]) * kScale;
            dst_g[i] = static_cast<float>(rgb_640x384[i * 3 + 1]) * kScale;
            dst_b[i] = static_cast<float>(rgb_640x384[i * 3 + 2]) * kScale;
        }

        id<MLFeatureProvider> prediction = [impl_->detect_model predictionFromFeatures:impl_->detect_input_features error:&ns_error];
        if (!prediction || ns_error) {
            error = "detect inference failed: " +
                    std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown");
            return false;
        }

        MLFeatureValue* out_val = [prediction featureValueForName:impl_->detect_output_name];
        if (!out_val || out_val.type != MLFeatureTypeMultiArray) {
            error = "detect output is not a multiarray";
            return false;
        }

        MLMultiArray* out_arr = out_val.multiArrayValue;

        NSInteger num_boxes = 15120;
        NSInteger num_features = 15;
        if (out_arr.shape.count >= 3) {
            num_boxes = out_arr.shape[1].integerValue;
            num_features = out_arr.shape[2].integerValue;
        }

        NSInteger box_stride = out_arr.strides.count >= 2 ? out_arr.strides[1].integerValue : num_features;
        NSInteger feat_stride = out_arr.strides.count >= 3 ? out_arr.strides[2].integerValue : 1;

        out.data.resize(num_boxes * num_features);
        out.num_boxes = static_cast<uint32_t>(num_boxes);
        out.num_features = static_cast<uint32_t>(num_features);

        float* dst_ptr = out.data.data();

        if (out_arr.dataType == MLMultiArrayDataTypeFloat16) {
            const _Float16* src16 = static_cast<const _Float16*>(out_arr.dataPointer);
            for (NSInteger b = 0; b < num_boxes; ++b) {
                const _Float16* src_row = src16 + b * box_stride;
                float* dst_row = dst_ptr + b * num_features;
                for (NSInteger f = 0; f < num_features; ++f) {
                    dst_row[f] = static_cast<float>(src_row[f * feat_stride]);
                }
            }
        } else if (out_arr.dataType == MLMultiArrayDataTypeFloat32) {
            const float* src32 = static_cast<const float*>(out_arr.dataPointer);
            for (NSInteger b = 0; b < num_boxes; ++b) {
                const float* src_row = src32 + b * box_stride;
                float* dst_row = dst_ptr + b * num_features;
                for (NSInteger f = 0; f < num_features; ++f) {
                    dst_row[f] = src_row[f * feat_stride];
                }
            }
        } else if (out_arr.dataType == MLMultiArrayDataTypeDouble) {
            const double* src64 = static_cast<const double*>(out_arr.dataPointer);
            for (NSInteger b = 0; b < num_boxes; ++b) {
                const double* src_row = src64 + b * box_stride;
                float* dst_row = dst_ptr + b * num_features;
                for (NSInteger f = 0; f < num_features; ++f) {
                    dst_row[f] = static_cast<float>(src_row[f * feat_stride]);
                }
            }
        }

        return true;
    }
}

bool ModelInferenceManager::run_rec(const uint8_t* rgb_320x48, PlateRecOutput& out, std::string& error) {
    @autoreleasepool {
        if (!impl_->rec_model || !impl_->rec_input_array || !impl_->rec_input_features) {
            error = "recognition model not loaded or uninitialized";
            return false;
        }

        NSError* ns_error = nil;
        float* dst = static_cast<float*>(impl_->rec_input_array.dataPointer);
        // PP-OCRv4 normalization: (x / 255.0 - 0.5) / 0.5 = x * (2.0 / 255.0) - 1.0
        constexpr float kScale = 2.0f / 255.0f;
        constexpr float kBias = -1.0f;
        constexpr int kPlaneSize = 48 * 320;

        float* dst_r = dst;
        float* dst_g = dst + kPlaneSize;
        float* dst_b = dst + 2 * kPlaneSize;

        for (int i = 0; i < kPlaneSize; ++i) {
            dst_r[i] = static_cast<float>(rgb_320x48[i * 3 + 0]) * kScale + kBias;
            dst_g[i] = static_cast<float>(rgb_320x48[i * 3 + 1]) * kScale + kBias;
            dst_b[i] = static_cast<float>(rgb_320x48[i * 3 + 2]) * kScale + kBias;
        }

        id<MLFeatureProvider> prediction = [impl_->rec_model predictionFromFeatures:impl_->rec_input_features error:&ns_error];
        if (!prediction || ns_error) {
            error = "recognition inference failed: " +
                    std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown");
            return false;
        }

        MLFeatureValue* out_val = [prediction featureValueForName:impl_->rec_output_name];
        if (!out_val || out_val.type != MLFeatureTypeMultiArray) {
            error = "recognition output is not a multiarray";
            return false;
        }

        MLMultiArray* out_arr = out_val.multiArrayValue;
        const size_t total_elements = 40 * 6625;
        out.char_probs.resize(total_elements);

        NSInteger t_stride = out_arr.strides.count >= 2 ? out_arr.strides[1].integerValue : 6625;
        NSInteger c_stride = out_arr.strides.count >= 3 ? out_arr.strides[2].integerValue : 1;

        float* dst_p = out.char_probs.data();

        if (out_arr.dataType == MLMultiArrayDataTypeFloat16) {
            const _Float16* src16 = static_cast<const _Float16*>(out_arr.dataPointer);
            for (NSInteger t = 0; t < 40; ++t) {
                const _Float16* row = src16 + t * t_stride;
                float* dst_row = dst_p + t * 6625;
                for (NSInteger c = 0; c < 6625; ++c) {
                    dst_row[c] = static_cast<float>(row[c * c_stride]);
                }
            }
        } else if (out_arr.dataType == MLMultiArrayDataTypeFloat32) {
            const float* src32 = static_cast<const float*>(out_arr.dataPointer);
            for (NSInteger t = 0; t < 40; ++t) {
                const float* row = src32 + t * t_stride;
                float* dst_row = dst_p + t * 6625;
                for (NSInteger c = 0; c < 6625; ++c) {
                    dst_row[c] = src32[t * t_stride + c * c_stride];
                }
            }
        } else if (out_arr.dataType == MLMultiArrayDataTypeDouble) {
            const double* src64 = static_cast<const double*>(out_arr.dataPointer);
            for (NSInteger t = 0; t < 40; ++t) {
                const double* row = src64 + t * t_stride;
                float* dst_row = dst_p + t * 6625;
                for (NSInteger c = 0; c < 6625; ++c) {
                    dst_row[c] = static_cast<float>(row[c * c_stride]);
                }
            }
        }

        return true;
    }
}

} // namespace lpr
