#pragma once

#include <memory>
#include <string>
#include <vector>
#include <cstdint>

namespace lpr {

struct PlateDetectOutput {
    std::vector<float> data;
    uint32_t num_boxes = 15120;
    uint32_t num_features = 15;
};

struct PlateRecOutput {
    std::vector<float> char_logits; // [21, 78]
    std::vector<float> color_logits; // [5]
};

class CoreMLRunnerImpl;

class ModelInferenceManager {
public:
    ModelInferenceManager();
    ~ModelInferenceManager();

    bool load_models(const std::string& package_root,
                     const std::string& detect_rel_path,
                     const std::string& rec_rel_path,
                     std::string& error);

    bool run_detect(const uint8_t* rgb_640x384, PlateDetectOutput& out, std::string& error);
    bool run_rec(const uint8_t* rgb_168x48, PlateRecOutput& out, std::string& error);

private:
    std::unique_ptr<CoreMLRunnerImpl> impl_;
};

} // namespace lpr
