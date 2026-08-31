/**
 * @file model_inference.mm
 * @brief Plate Detector & CRNN OCR Core ML 模型加载与推理实现
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

    NSString* rec_input_name = nil;
    NSString* rec_char_name = nil;
    NSString* rec_color_name = nil;

    ~CoreMLRunnerImpl() {
        detect_model = nil;
        rec_model = nil;
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

        // 1. Load Plate Detector
        impl_->detect_model = compile_and_load_model(root / detect_rel_path, error);
        if (!impl_->detect_model) return false;

        MLModelDescription* detect_desc = impl_->detect_model.modelDescription;
        impl_->detect_input_name = detect_desc.inputDescriptionsByName.allKeys.firstObject;
        impl_->detect_output_name = detect_desc.outputDescriptionsByName.allKeys.firstObject;

        // 2. Load Plate OCR & Color
        impl_->rec_model = compile_and_load_model(root / rec_rel_path, error);
        if (!impl_->rec_model) return false;

        MLModelDescription* rec_desc = impl_->rec_model.modelDescription;
        impl_->rec_input_name = rec_desc.inputDescriptionsByName.allKeys.firstObject;

        for (NSString* out_name in rec_desc.outputDescriptionsByName) {
            MLFeatureDescription* fd = rec_desc.outputDescriptionsByName[out_name];
            NSArray<NSNumber*>* shape = fd.multiArrayConstraint.shape;
            if (shape.count >= 2) {
                NSInteger last_dim = shape[shape.count - 1].integerValue;
                if (last_dim == 78 || shape.count == 3) {
                    impl_->rec_char_name = out_name;
                } else if (last_dim == 5 || (shape.count == 2 && last_dim == 5)) {
                    impl_->rec_color_name = out_name;
                }
            } else if (shape.count == 1 && shape[0].integerValue == 5) {
                impl_->rec_color_name = out_name;
            }
        }

        if (!impl_->rec_char_name || !impl_->rec_color_name) {
            // Fallback: assign first to char, second to color if available
            NSArray<NSString*>* keys = rec_desc.outputDescriptionsByName.allKeys;
            if (keys.count >= 2) {
                impl_->rec_char_name = keys[0];
                impl_->rec_color_name = keys[1];
            } else {
                error = "failed to identify character and color outputs for recognition model";
                return false;
            }
        }

        return true;
    }
}

bool ModelInferenceManager::run_detect(const uint8_t* rgb_640x384, PlateDetectOutput& out, std::string& error) {
    @autoreleasepool {
        if (!impl_->detect_model) {
            error = "detect model not loaded";
            return false;
        }

        NSError* ns_error = nil;
        NSArray<NSNumber*>* shape = @[@1, @3, @384, @640];
        MLMultiArray* input_array = [[MLMultiArray alloc] initWithShape:shape dataType:MLMultiArrayDataTypeFloat32 error:&ns_error];
        if (!input_array || ns_error) {
            error = "failed to allocate detect input multiarray";
            return false;
        }

        float* dst = static_cast<float*>(input_array.dataPointer);
        // NCHW RGB: normalized with / 255.0f
        for (int c = 0; c < 3; ++c) {
            float* channel_ptr = dst + c * (640 * 384);
            for (int i = 0; i < 640 * 384; ++i) {
                channel_ptr[i] = static_cast<float>(rgb_640x384[i * 3 + c]) / 255.0f;
            }
        }

        MLFeatureValue* input_val = [MLFeatureValue featureValueWithMultiArray:input_array];
        MLDictionaryFeatureProvider* input_features =
            [[MLDictionaryFeatureProvider alloc] initWithDictionary:@{impl_->detect_input_name: input_val} error:&ns_error];

        id<MLFeatureProvider> prediction = [impl_->detect_model predictionFromFeatures:input_features error:&ns_error];
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

        NSInteger num_boxes = 25200;
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

bool ModelInferenceManager::run_rec(const uint8_t* rgb_168x48, PlateRecOutput& out, std::string& error) {
    @autoreleasepool {
        if (!impl_->rec_model) {
            error = "recognition model not loaded";
            return false;
        }

        NSError* ns_error = nil;
        NSArray<NSNumber*>* shape = @[@1, @3, @48, @168];
        MLMultiArray* input_array = [[MLMultiArray alloc] initWithShape:shape dataType:MLMultiArrayDataTypeFloat32 error:&ns_error];
        if (!input_array || ns_error) {
            error = "failed to allocate recognition input multiarray";
            return false;
        }

        float* dst = static_cast<float*>(input_array.dataPointer);
        // NCHW: CRNN model was trained on OpenCV BGR channel layout (B, G, R)
        // normalized with (x / 255.0f - 0.588f) / 0.193f
        for (int c = 0; c < 3; ++c) {
            int src_c = 2 - c; // 0 -> B(src 2), 1 -> G(src 1), 2 -> R(src 0)
            float* channel_ptr = dst + c * (48 * 168);
            for (int i = 0; i < 48 * 168; ++i) {
                float val = static_cast<float>(rgb_168x48[i * 3 + src_c]) / 255.0f;
                channel_ptr[i] = (val - 0.588f) / 0.193f;
            }
        }

        MLFeatureValue* input_val = [MLFeatureValue featureValueWithMultiArray:input_array];
        MLDictionaryFeatureProvider* input_features =
            [[MLDictionaryFeatureProvider alloc] initWithDictionary:@{impl_->rec_input_name: input_val} error:&ns_error];

        id<MLFeatureProvider> prediction = [impl_->rec_model predictionFromFeatures:input_features error:&ns_error];
        if (!prediction || ns_error) {
            error = "recognition inference failed: " +
                    std::string(ns_error ? ns_error.localizedDescription.UTF8String : "unknown");
            return false;
        }

        // 1. Character logits
        MLFeatureValue* char_val = [prediction featureValueForName:impl_->rec_char_name];
        if (char_val && char_val.type == MLFeatureTypeMultiArray) {
            MLMultiArray* char_arr = char_val.multiArrayValue;
            out.char_logits.resize(21 * 78);
            NSInteger t_stride = char_arr.strides.count >= 2 ? char_arr.strides[char_arr.strides.count - 2].integerValue : 78;
            NSInteger c_stride = char_arr.strides.count >= 1 ? char_arr.strides.lastObject.integerValue : 1;

            if (char_arr.dataType == MLMultiArrayDataTypeFloat16) {
                const _Float16* src16 = static_cast<const _Float16*>(char_arr.dataPointer);
                for (NSInteger t = 0; t < 21; ++t) {
                    for (NSInteger c = 0; c < 78; ++c) {
                        out.char_logits[t * 78 + c] = static_cast<float>(src16[t * t_stride + c * c_stride]);
                    }
                }
            } else if (char_arr.dataType == MLMultiArrayDataTypeFloat32) {
                const float* src32 = static_cast<const float*>(char_arr.dataPointer);
                for (NSInteger t = 0; t < 21; ++t) {
                    for (NSInteger c = 0; c < 78; ++c) {
                        out.char_logits[t * 78 + c] = src32[t * t_stride + c * c_stride];
                    }
                }
            } else if (char_arr.dataType == MLMultiArrayDataTypeDouble) {
                const double* src64 = static_cast<const double*>(char_arr.dataPointer);
                for (NSInteger t = 0; t < 21; ++t) {
                    for (NSInteger c = 0; c < 78; ++c) {
                        out.char_logits[t * 78 + c] = static_cast<float>(src64[t * t_stride + c * c_stride]);
                    }
                }
            }
        }

        // 2. Color logits
        MLFeatureValue* color_val = [prediction featureValueForName:impl_->rec_color_name];
        if (color_val && color_val.type == MLFeatureTypeMultiArray) {
            MLMultiArray* color_arr = color_val.multiArrayValue;
            out.color_logits.resize(5);
            NSInteger col_stride = color_arr.strides.count >= 1 ? color_arr.strides.lastObject.integerValue : 1;

            if (color_arr.dataType == MLMultiArrayDataTypeFloat16) {
                const _Float16* src16 = static_cast<const _Float16*>(color_arr.dataPointer);
                for (NSInteger c = 0; c < 5; ++c) {
                    out.color_logits[c] = static_cast<float>(src16[c * col_stride]);
                }
            } else if (color_arr.dataType == MLMultiArrayDataTypeFloat32) {
                const float* src32 = static_cast<const float*>(color_arr.dataPointer);
                for (NSInteger c = 0; c < 5; ++c) {
                    out.color_logits[c] = src32[c * col_stride];
                }
            } else if (color_arr.dataType == MLMultiArrayDataTypeDouble) {
                const double* src64 = static_cast<const double*>(color_arr.dataPointer);
                for (NSInteger c = 0; c < 5; ++c) {
                    out.color_logits[c] = static_cast<float>(src64[c * col_stride]);
                }
            }
        }

        return true;
    }
}

} // namespace lpr
