#pragma once

#include "aivision/algo.h"
#include "aivision/types.h"
#include "../postprocess/postprocessor.hpp"
#include <memory>
#include <string>
#include <vector>
#include <mutex>

#if defined(HAVE_RKNPU2) || defined(HAVE_LIBRKNNRT) || defined(__aarch64__)
#define HAVE_RKNNRT 1
#include "rknn_api.h"
#endif

namespace yolov8n {

class RknnInstanceContext {
public:
    virtual ~RknnInstanceContext() = default;
    virtual bool run(const void* input_data, uint32_t input_size, std::vector<RknnOutputBuffer>& outputs) = 0;
};

class RknnRunner {
public:
    RknnRunner(av_log_fn log, void* log_user);
    virtual ~RknnRunner();

    bool load_model(const std::string& model_path);
    std::shared_ptr<RknnInstanceContext> create_instance();

private:
    av_log_fn log_ = nullptr;
    void* log_user_ = nullptr;
    std::string model_path_;
    bool loaded_ = false;

#ifdef HAVE_RKNNRT
    rknn_context ctx_ = 0;
    uint32_t n_input_ = 0;
    uint32_t n_output_ = 0;
    std::vector<rknn_tensor_attr> input_attrs_;
    std::vector<rknn_tensor_attr> output_attrs_;
    std::vector<uint8_t> model_data_;
#endif

    friend class RknnInstanceContextImpl;
};

} // namespace yolov8n
