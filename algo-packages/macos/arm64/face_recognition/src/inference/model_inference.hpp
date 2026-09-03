#pragma once

#include <memory>
#include <string>
#include <vector>
#include <cstdint>

#ifdef __APPLE__
typedef struct __CVBuffer* CVPixelBufferRef;
#else
typedef void* CVPixelBufferRef;
#endif

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
 * @brief 零拷贝只读输出头视图
 */
struct ScrfdHeadView {
    const float* data = nullptr;           // 连续或基地址指针 (Float32)
    const _Float16* data_fp16 = nullptr;   // 若 Core ML 原生输出为 Float16 则指向此指针
    bool is_fp16 = false;                  // 是否为 FP16 数据类型
    uint32_t num_anchors = 0;              // 锚点数量 (7680, 1920, 480 等)
    uint32_t dim1 = 1;                     // 通道维度 (Score=1, Bbox=4, Kps=10)
    int64_t stride_anchor = 0;             // 相邻 anchor 之间的元素步长
    int64_t stride_channel = 1;            // 相同 anchor 内相邻 channel 的元素步长

    // 内联快速读取单一浮点数
    inline float get(uint32_t a_idx, uint32_t c_idx = 0) const noexcept {
        const int64_t offset = a_idx * stride_anchor + c_idx * stride_channel;
        if (__builtin_expect(!is_fp16, 1)) {
            return data ? data[offset] : 0.0f;
        } else {
            return data_fp16 ? static_cast<float>(data_fp16[offset]) : 0.0f;
        }
    }

    inline bool valid() const noexcept {
        return (data != nullptr || data_fp16 != nullptr) && num_anchors > 0;
    }
};

/**
 * @brief SCRFD 9-head 输出张量视图 (零拷贝)
 * - 640x384 输入 (流媒体) anchor 数分别为 7680 / 1920 / 480
 * - 640x640 输入 (静态注册) anchor 数分别为 12800 / 3200 / 800
 */
struct ScrfdOutput {
    ScrfdHeadView score_8;
    ScrfdHeadView score_16;
    ScrfdHeadView score_32;
    ScrfdHeadView bbox_8;
    ScrfdHeadView bbox_16;
    ScrfdHeadView bbox_32;
    ScrfdHeadView kps_8;
    ScrfdHeadView kps_16;
    ScrfdHeadView kps_32;

    // 生命周期 Token：持有 Core ML MLFeatureProvider，
    // 确保底层 MLMultiArray 内存指针在当前帧后处理完成前始终有效且不被回收
    std::shared_ptr<void> buffer_holder;
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
    bool run_yolo(CVPixelBufferRef pixel_buffer, YoloOutput& out, std::string& error);

    bool run_scrfd(const uint8_t* rgb_640x384, ScrfdOutput& out, std::string& error);
    bool run_scrfd(CVPixelBufferRef pixel_buffer, ScrfdOutput& out, std::string& error);

    bool run_scrfd_reg(const uint8_t* rgb_640x640, ScrfdOutput& out, std::string& error);
    bool run_scrfd_reg(CVPixelBufferRef pixel_buffer, ScrfdOutput& out, std::string& error);

    bool run_glintr(const uint8_t* rgb_112x112, GlintrOutput& out, std::string& error);
    bool run_glintr(CVPixelBufferRef pixel_buffer, GlintrOutput& out, std::string& error);

private:
    std::unique_ptr<CoreMLRunnerImpl> impl_;
};

} // namespace face_recognition
