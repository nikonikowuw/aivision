#pragma once

#include <memory>
#include <string>
#include <vector>
#include <cstdint>

namespace face_recognition {

/**
 * @brief YOLOv8n 输出张量 (8400, 84) 或 (84, 8400)
 */
struct YoloOutput {
    std::vector<float> data;
    uint32_t num_boxes = 8400;
    uint32_t num_classes_and_coords = 84;
};

/**
 * @brief SCRFD 9-head 输出张量
 */
struct ScrfdOutput {
    std::vector<float> score_8;   // (12800, 1)
    std::vector<float> score_16;  // (3200, 1)
    std::vector<float> score_32;  // (800, 1)
    std::vector<float> bbox_8;    // (12800, 4)
    std::vector<float> bbox_16;   // (3200, 4)
    std::vector<float> bbox_32;   // (800, 4)
    std::vector<float> kps_8;     // (12800, 10)
    std::vector<float> kps_16;    // (3200, 10)
    std::vector<float> kps_32;    // (800, 10)
};

/**
 * @brief GLINTR 512 维特征输出
 */
struct GlintrOutput {
    std::vector<float> embedding; // 512 floats
};

class CoreMLRunnerImpl;

/**
 * @brief 三个 Core ML 模型的推理管理器
 */
class ModelInferenceManager {
public:
    ModelInferenceManager();
    ~ModelInferenceManager();

    bool load_models(const std::string& package_root,
                     const std::string& yolo_rel_path,
                     const std::string& scrfd_rel_path,
                     const std::string& glintr_rel_path,
                     std::string& error);

    bool run_yolo(const uint8_t* rgb_640x640, YoloOutput& out, std::string& error);
    bool run_scrfd(const uint8_t* rgb_640x640, ScrfdOutput& out, std::string& error);
    bool run_glintr(const uint8_t* rgb_112x112, GlintrOutput& out, std::string& error);

private:
    std::unique_ptr<CoreMLRunnerImpl> impl_;
};

} // namespace face_recognition
