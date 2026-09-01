#pragma once

#include <memory>
#include <string>
#include <vector>
#include <cstdint>

namespace face_recognition {

/**
 * @brief YOLOv8n 输出张量 (5040, 84) 或 (84, 5040)
 */
struct YoloOutput {
    std::vector<float> data;
    uint32_t num_boxes = 5040;
    uint32_t num_classes_and_coords = 84;
};

/**
 * @brief SCRFD 9-head 输出张量
 * - 640x384 输入 (流媒体) anchor 数分别为 7680 / 1920 / 480
 * - 640x640 输入 (静态注册) anchor 数分别为 12800 / 3200 / 800
 */
struct ScrfdOutput {
    std::vector<float> score_8;   // (N, 1)
    std::vector<float> score_16;  // (N, 1)
    std::vector<float> score_32;  // (N, 1)
    std::vector<float> bbox_8;    // (N, 4)
    std::vector<float> bbox_16;   // (N, 4)
    std::vector<float> bbox_32;   // (N, 4)
    std::vector<float> kps_8;     // (N, 10)
    std::vector<float> kps_16;    // (N, 10)
    std::vector<float> kps_32;    // (N, 10)
};

/**
 * @brief GLINTR 512 维特征输出
 */
struct GlintrOutput {
    std::vector<float> embedding; // 512 floats
};

class CoreMLRunnerImpl;

/**
 * @brief Core ML 模型的推理管理器
 */
class ModelInferenceManager {
public:
    ModelInferenceManager();
    ~ModelInferenceManager();

    bool load_models(const std::string& package_root,
                     const std::string& yolo_rel_path,
                     const std::string& scrfd_rel_path,
                     const std::string& scrfd_reg_rel_path,
                     const std::string& glintr_rel_path,
                     std::string& error);

    bool run_yolo(const uint8_t* rgb_640x384, YoloOutput& out, std::string& error);
    bool run_scrfd(const uint8_t* rgb_640x384, ScrfdOutput& out, std::string& error);
    bool run_scrfd_reg(const uint8_t* rgb_640x640, ScrfdOutput& out, std::string& error);
    bool run_glintr(const uint8_t* rgb_112x112, GlintrOutput& out, std::string& error);

private:
    std::unique_ptr<CoreMLRunnerImpl> impl_;
};

} // namespace face_recognition
