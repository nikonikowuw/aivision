#pragma once

#include "argus/algo.h"

#include <memory>
#include <string>
#include <vector>

namespace yolo26n {

class CoreMLRunner {
public:
    struct Impl;

    CoreMLRunner(av_log_fn log = nullptr, void* log_user = nullptr);
    ~CoreMLRunner();

    bool load_model(const std::string& model_package_path);
    bool run_pixelbuffer(void* cvpixelbuffer, std::vector<float>& out_tensor);

private:
    std::unique_ptr<Impl> impl_;
};

} // namespace yolo26n
