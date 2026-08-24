#include "rknn_runner.hpp"
#include <iostream>
#include <fstream>
#include <cstring>

namespace yolov8n {

#ifdef HAVE_RKNNRT

class RknnInstanceContextImpl : public RknnInstanceContext {
public:
    RknnInstanceContextImpl(rknn_context parent_ctx, uint32_t n_in, uint32_t n_out,
                            const std::vector<rknn_tensor_attr>& in_attrs,
                            const std::vector<rknn_tensor_attr>& out_attrs,
                            av_log_fn log, void* log_user)
        : n_input_(n_in), n_output_(n_out), input_attrs_(in_attrs), output_attrs_(out_attrs),
          log_(log), log_user_(log_user)
    {
        int ret = rknn_dup_context(&parent_ctx, &ctx_);
        if (ret < 0) {
            ctx_ = parent_ctx; // fallback to sharing parent context
        }
        rknn_outputs_.resize(n_output_);
        output_buffers_.resize(n_output_);
    }

    ~RknnInstanceContextImpl() override {
        if (ctx_) {
            rknn_destroy(ctx_);
            ctx_ = 0;
        }
    }

    bool run(const void* input_data, uint32_t input_size, std::vector<RknnOutputBuffer>& outputs) override {
        rknn_input rknn_in;
        std::memset(&rknn_in, 0, sizeof(rknn_in));
        rknn_in.index = 0;
        rknn_in.type = RKNN_TENSOR_UINT8;
        rknn_in.size = input_size;
        rknn_in.fmt = RKNN_TENSOR_NHWC;
        rknn_in.buf = const_cast<void*>(input_data);

        int ret = rknn_inputs_set(ctx_, 1, &rknn_in);
        if (ret < 0) {
            return false;
        }

        ret = rknn_run(ctx_, nullptr);
        if (ret < 0) {
            return false;
        }

        for (uint32_t i = 0; i < n_output_; ++i) {
            rknn_outputs_[i].want_float = 0; // Receive native INT8
            rknn_outputs_[i].index = i;
            rknn_outputs_[i].is_prealloc = 0;
        }

        ret = rknn_outputs_get(ctx_, n_output_, rknn_outputs_.data(), nullptr);
        if (ret < 0) {
            return false;
        }

        for (uint32_t i = 0; i < n_output_; ++i) {
            output_buffers_[i].data = rknn_outputs_[i].buf;
            output_buffers_[i].size = rknn_outputs_[i].size;
            output_buffers_[i].scale = output_attrs_[i].scale;
            output_buffers_[i].zero_point = output_attrs_[i].zp;
            output_buffers_[i].is_quantized = true;
        }

        outputs = output_buffers_;
        return true;
    }

private:
    rknn_context ctx_{0};
    uint32_t n_input_{0};
    uint32_t n_output_{0};
    std::vector<rknn_tensor_attr> input_attrs_;
    std::vector<rknn_tensor_attr> output_attrs_;
    std::vector<rknn_output> rknn_outputs_;
    std::vector<RknnOutputBuffer> output_buffers_;
    av_log_fn log_{nullptr};
    void* log_user_{nullptr};
};

#else

class RknnInstanceContextMock : public RknnInstanceContext {
public:
    bool run(const void*, uint32_t, std::vector<RknnOutputBuffer>& outputs) override {
        outputs.resize(9);
        return true;
    }
};

#endif

RknnRunner::RknnRunner(av_log_fn log, void* log_user)
    : log_(log), log_user_(log_user) {}

RknnRunner::~RknnRunner() {
#ifdef HAVE_RKNNRT
    if (ctx_) {
        rknn_destroy(ctx_);
        ctx_ = 0;
    }
#endif
}

bool RknnRunner::load_model(const std::string& model_path) {
    model_path_ = model_path;
#ifdef HAVE_RKNNRT
    std::ifstream file(model_path, std::ios::binary | std::ios::ate);
    if (!file.is_open()) {
        return false;
    }
    std::streamsize size = file.tellg();
    file.seekg(0, std::ios::beg);
    model_data_.resize(size);
    if (!file.read(reinterpret_cast<char*>(model_data_.data()), size)) {
        return false;
    }

    int ret = rknn_init(&ctx_, model_data_.data(), size, 0, nullptr);
    if (ret < 0) {
        return false;
    }

    rknn_input_output_num io_num;
    ret = rknn_query(ctx_, RKNN_QUERY_IN_OUT_NUM, &io_num, sizeof(io_num));
    if (ret < 0) {
        return false;
    }

    n_input_ = io_num.n_input;
    n_output_ = io_num.n_output;

    input_attrs_.resize(n_input_);
    for (uint32_t i = 0; i < n_input_; ++i) {
        input_attrs_[i].index = i;
        rknn_query(ctx_, RKNN_QUERY_INPUT_ATTR, &input_attrs_[i], sizeof(rknn_tensor_attr));
    }

    output_attrs_.resize(n_output_);
    for (uint32_t i = 0; i < n_output_; ++i) {
        output_attrs_[i].index = i;
        rknn_query(ctx_, RKNN_QUERY_OUTPUT_ATTR, &output_attrs_[i], sizeof(rknn_tensor_attr));
    }

    loaded_ = true;
    return true;
#else
    loaded_ = true;
    return true;
#endif
}

std::shared_ptr<RknnInstanceContext> RknnRunner::create_instance() {
    if (!loaded_) return nullptr;
#ifdef HAVE_RKNNRT
    return std::make_shared<RknnInstanceContextImpl>(
        ctx_, n_input_, n_output_, input_attrs_, output_attrs_, log_, log_user_
    );
#else
    return std::make_shared<RknnInstanceContextMock>();
#endif
}

} // namespace yolov8n
