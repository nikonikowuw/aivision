#pragma once

/**
 * @file model_inference.hpp
 * @brief 车牌检测 (YOLOv5-plate) 与通用多语言文本识别 (PP-OCRv4) Core ML 推理管理器
 */

#include <string>
#include <vector>
#include <memory>
#include <cstdint>

namespace lpr {

struct PlateDetectOutput {
    std::vector<float> data; // [1, 15120, 15]
    uint32_t num_boxes = 0;
    uint32_t num_features = 0;
};

struct PlateRecOutput {
    std::vector<float> char_probs; // [1, 40, 6625]
};

class CoreMLRunnerImpl;

class ModelInferenceManager {
public:
    ModelInferenceManager();
    ~ModelInferenceManager();

    /**
     * @brief 加载检测模型与通用 PP-OCR 识别模型
     */
    bool load_models(const std::string& package_root,
                     const std::string& detect_rel_path,
                     const std::string& rec_rel_path,
                     std::string& error);

    /**
     * @brief 执行车牌检测 (640x384 RGB)
     */
    bool run_detect(const uint8_t* rgb_640x384, PlateDetectOutput& out, std::string& error);

    /**
     * @brief 执行通用多语言车牌识别 (320x48 RGB)
     */
    bool run_rec(const uint8_t* rgb_320x48, PlateRecOutput& out, std::string& error);

private:
    std::unique_ptr<CoreMLRunnerImpl> impl_;
};

} // namespace lpr
