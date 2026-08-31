#import "coreml_runner.hpp"
#import <Foundation/Foundation.h>
#import <CoreML/CoreML.h>
#import <CoreVideo/CoreVideo.h>
#import <Accelerate/Accelerate.h>

#include <algorithm>
#include <cmath>
#include <cstring>
#include <limits>

namespace yolov8n {

struct CoreMLRunner::Impl {
    MLModel* model = nil;
    std::string input_name = "image";
    std::string output_name = "var_911";
    av_log_fn log = nullptr;
    void* log_user = nullptr;
};

namespace {

void log_message(const CoreMLRunner::Impl& impl, const char* message) {
    if (!impl.log || !message) return;
    impl.log(impl.log_user, 3, message, static_cast<uint32_t>(std::strlen(message)));
}

void log_error(const CoreMLRunner::Impl& impl, NSString* prefix, NSError* error) {
    NSString* detail = error ? [NSString stringWithFormat:@"%@ (code %ld, domain %@)", [error localizedDescription], (long)[error code], [error domain]] : @"unknown error";
    NSString* message = [NSString stringWithFormat:@"%@%s", prefix, detail.UTF8String ? detail.UTF8String : "unknown error"];
    log_message(impl, message.UTF8String);
}

} // namespace

CoreMLRunner::CoreMLRunner(av_log_fn log, void* log_user) : impl_(std::make_unique<Impl>()) {
    impl_->log = log;
    impl_->log_user = log_user;
}

CoreMLRunner::~CoreMLRunner() {
    if (impl_) impl_->model = nil;
}

bool CoreMLRunner::load_model(const std::string& model_path) {
    @autoreleasepool {
        NSString* path_str = [NSString stringWithUTF8String:model_path.c_str()];
        if (!path_str) {
            log_message(*impl_, "CoreML model path is not valid UTF-8");
            return false;
        }
        NSURL* model_url = [NSURL fileURLWithPath:path_str];
        if (!model_url) {
            log_message(*impl_, "CoreML model URL could not be created");
            return false;
        }

        NSError* error = nil;
        NSURL* compiled_url = model_url;
        if ([path_str hasSuffix:@".mlpackage"] || [path_str hasSuffix:@".mlmodel"]) {
            std::string compile_msg = std::string("Compiling CoreML model at URL: ") + ([model_url.path UTF8String] ? [model_url.path UTF8String] : "");
            log_message(*impl_, compile_msg.c_str());
            compiled_url = [MLModel compileModelAtURL:model_url error:&error];
            if (error || !compiled_url) {
                log_error(*impl_, @"CoreML compileModelAtURL failed: ", error);
                return false;
            }
            std::string compiled_msg = std::string("Compiled CoreML model URL: ") + ([compiled_url.path UTF8String] ? [compiled_url.path UTF8String] : "");
            log_message(*impl_, compiled_msg.c_str());
        }

        MLModelConfiguration* config = [[MLModelConfiguration alloc] init];
        config.computeUnits = MLComputeUnitsAll;
        MLModel* loaded = [MLModel modelWithContentsOfURL:compiled_url configuration:config error:&error];
        if (error || !loaded) {
            log_error(*impl_, @"CoreML model load failed: ", error);
            return false;
        }

        impl_->model = loaded;
        impl_->input_name = "image";
        impl_->output_name = "var_911";

        MLModelDescription* description = impl_->model.modelDescription;
        for (NSString* input_key in description.inputDescriptionsByName) {
            MLFeatureDescription* feature = description.inputDescriptionsByName[input_key];
            if (feature.type == MLFeatureTypeImage && input_key.UTF8String) {
                impl_->input_name = input_key.UTF8String;
                break;
            }
        }
        for (NSString* output_key in description.outputDescriptionsByName) {
            MLFeatureDescription* feature = description.outputDescriptionsByName[output_key];
            if (feature.type == MLFeatureTypeMultiArray && output_key.UTF8String) {
                impl_->output_name = output_key.UTF8String;
                break;
            }
        }
        return true;
    }
}

bool CoreMLRunner::run_pixelbuffer(void* pixel_buffer, std::vector<float>& out_tensor) {
    if (!impl_ || !impl_->model || !pixel_buffer) return false;

    @autoreleasepool {
        CVPixelBufferRef cv_buffer = static_cast<CVPixelBufferRef>(pixel_buffer);
        MLFeatureValue* input_value = [MLFeatureValue featureValueWithPixelBuffer:cv_buffer];
        if (!input_value) return false;
        NSString* input_name = [NSString stringWithUTF8String:impl_->input_name.c_str()];
        NSString* output_name = [NSString stringWithUTF8String:impl_->output_name.c_str()];
        if (!input_name || !output_name) return false;

        NSDictionary* input_dictionary = @{input_name : input_value};
        NSError* error = nil;
        MLDictionaryFeatureProvider* input_provider = [[MLDictionaryFeatureProvider alloc]
            initWithDictionary:input_dictionary error:&error];
        if (error || !input_provider) {
            return false;
        }

        id<MLFeatureProvider> output_provider = [impl_->model predictionFromFeatures:input_provider error:&error];
        if (error || !output_provider) {
            log_error(*impl_, @"CoreML prediction failed: ", error);
            return false;
        }

        MLFeatureValue* output_value = [output_provider featureValueForName:output_name];
        if (!output_value || output_value.type != MLFeatureTypeMultiArray) return false;

        MLMultiArray* array = output_value.multiArrayValue;
        if (!array || array.shape.count != 3 || array.strides.count != 3 || array.shape[0].integerValue != 1 ||
            array.shape[1].integerValue != 84 || array.shape[2].integerValue != 5040 || array.count != 84 * 5040) {
            log_message(*impl_, "CoreML output tensor shape is not [1,84,5040]");
            return false;
        }

        const NSInteger count = array.count;
        const NSInteger stride1 = array.strides[1].integerValue;
        const NSInteger stride2 = array.strides[2].integerValue;
        out_tensor.assign(static_cast<size_t>(count), 0.0f);

        if (array.dataType == MLMultiArrayDataTypeFloat32) {
            const float* data = static_cast<const float*>(array.dataPointer);
            for (NSInteger channel = 0; channel < 84; ++channel) {
                for (NSInteger item = 0; item < 5040; ++item) {
                    const float value = data[channel * stride1 + item * stride2];
                    if (!std::isfinite(value)) {
                        log_message(*impl_, "CoreML output tensor contains a non-finite Float32 value");
                        return false;
                    }
                    out_tensor[static_cast<size_t>(channel * 5040 + item)] = value;
                }
            }
        } else if (array.dataType == MLMultiArrayDataTypeDouble) {
            const double* data = static_cast<const double*>(array.dataPointer);
            for (NSInteger channel = 0; channel < 84; ++channel) {
                for (NSInteger item = 0; item < 5040; ++item) {
                    const double value = data[channel * stride1 + item * stride2];
                    if (!std::isfinite(value) || value < -std::numeric_limits<float>::max() ||
                        value > std::numeric_limits<float>::max()) {
                        return false;
                    }
                    out_tensor[static_cast<size_t>(channel * 5040 + item)] = static_cast<float>(value);
                }
            }
        } else if (array.dataType == MLMultiArrayDataTypeFloat16) {
            std::vector<uint16_t> float16_values(static_cast<size_t>(count));
            const uint16_t* data = static_cast<const uint16_t*>(array.dataPointer);
            for (NSInteger channel = 0; channel < 84; ++channel) {
                for (NSInteger item = 0; item < 5040; ++item) {
                    float16_values[static_cast<size_t>(channel * 5040 + item)] =
                        data[channel * stride1 + item * stride2];
                }
            }
            vImage_Buffer source{float16_values.data(), 1, static_cast<vImagePixelCount>(count),
                                 static_cast<size_t>(count * sizeof(uint16_t))};
            vImage_Buffer destination{out_tensor.data(), 1, static_cast<vImagePixelCount>(count),
                                      static_cast<size_t>(count * sizeof(float))};
            if (vImageConvert_Planar16FtoPlanarF(&source, &destination, kvImageNoFlags) != kvImageNoError) {
                return false;
            }
        } else {
            log_message(*impl_, "CoreML output tensor data type is unsupported");
            return false;
        }

        if (std::any_of(out_tensor.begin(), out_tensor.end(), [](float value) {
                return !std::isfinite(value);
            })) {
            log_message(*impl_, "CoreML output tensor contains a non-finite value");
            return false;
        }
    }
    return true;
}

} // namespace yolov8n
