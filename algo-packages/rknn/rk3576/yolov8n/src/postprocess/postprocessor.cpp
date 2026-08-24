#include "postprocessor.hpp"
#include <algorithm>
#include <cmath>
#include <cstring>
#include <iostream>

namespace yolov8n {

static std::vector<std::string> g_labels;

void Postprocessor::set_labels(const std::vector<std::string>& labels) {
    g_labels = labels;
}

const std::string& Postprocessor::get_label(int class_id) {
    static const std::string unknown = "unknown";
    if (class_id >= 0 && class_id < static_cast<int>(g_labels.size())) {
        return g_labels[class_id];
    }
    return unknown;
}

static inline float dequant_i8(int8_t val, int32_t zp, float scale) {
    return static_cast<float>(val - zp) * scale;
}

static inline int8_t quant_f32(float val, int32_t zp, float scale) {
    float v = val / scale + static_cast<float>(zp);
    v = std::max(-128.0f, std::min(127.0f, v));
    return static_cast<int8_t>(std::round(v));
}

static void compute_dfl(const float* before_dfl, int dfl_len, float* box) {
    for (int k = 0; k < 4; ++k) {
        const float* dfl_reg = before_dfl + k * dfl_len;
        float max_v = dfl_reg[0];
        for (int i = 1; i < dfl_len; ++i) {
            if (dfl_reg[i] > max_v) max_v = dfl_reg[i];
        }
        float sum = 0.0f;
        float exp_dfl[16];
        for (int i = 0; i < dfl_len; ++i) {
            exp_dfl[i] = std::exp(dfl_reg[i] - max_v);
            sum += exp_dfl[i];
        }
        float acc = 0.0f;
        for (int i = 0; i < dfl_len; ++i) {
            acc += (exp_dfl[i] / sum) * static_cast<float>(i);
        }
        box[k] = acc;
    }
}

std::vector<DetectionBox> Postprocessor::decode(
    const std::vector<RknnOutputBuffer>& outputs,
    const aivision::cv::LetterboxInfo& letterbox,
    float conf_threshold,
    float nms_threshold,
    int src_w,
    int src_h
) {
    std::vector<DetectionBox> candidates;
    const int strides[3] = {8, 16, 32};
    const int dfl_len = 16;
    const int num_classes = 80;

    // Handle 9-output (3 branches * [box, cls, sum]) or 6-output or 3-output
    bool is_9_branches = (outputs.size() >= 9);

    for (int s = 0; s < 3; ++s) {
        int stride = strides[s];
        int grid_w = 640 / stride;
        int grid_h = 640 / stride;
        int grid_len = grid_w * grid_h;

        const RknnOutputBuffer* box_buf = nullptr;
        const RknnOutputBuffer* cls_buf = nullptr;
        const RknnOutputBuffer* sum_buf = nullptr;

        if (is_9_branches) {
            // Model outputs: 318(box0), sum326(cls0), 331(sum0), 338(box1), sum346(cls1), 350(sum1), 357(box2), sum365(cls2), 369(sum2)
            box_buf = &outputs[s * 3 + 0];
            cls_buf = &outputs[s * 3 + 1];
            sum_buf = &outputs[s * 3 + 2];
        } else if (outputs.size() == 6) {
            box_buf = &outputs[s * 2 + 0];
            cls_buf = &outputs[s * 2 + 1];
        } else if (outputs.size() == 3) {
            box_buf = &outputs[s];
        } else {
            continue;
        }

        const int8_t* box_ptr = reinterpret_cast<const int8_t*>(box_buf->data);
        const int8_t* cls_ptr = cls_buf ? reinterpret_cast<const int8_t*>(cls_buf->data) : nullptr;
        const int8_t* sum_ptr = sum_buf ? reinterpret_cast<const int8_t*>(sum_buf->data) : nullptr;

        int8_t cls_th_i8 = cls_buf ? quant_f32(conf_threshold, cls_buf->zero_point, cls_buf->scale) : 0;
        int8_t sum_th_i8 = sum_buf ? quant_f32(conf_threshold, sum_buf->zero_point, sum_buf->scale) : 0;

        for (int i = 0; i < grid_h; ++i) {
            for (int j = 0; j < grid_w; ++j) {
                int offset = i * grid_w + j;

                if (sum_ptr && sum_ptr[offset] < sum_th_i8) {
                    continue;
                }

                int max_class_id = -1;
                int8_t max_score = cls_buf ? -cls_buf->zero_point : -128;

                if (cls_ptr) {
                    int c_offset = offset;
                    for (int c = 0; c < num_classes; ++c) {
                        if (cls_ptr[c_offset] > cls_th_i8 && cls_ptr[c_offset] > max_score) {
                            max_score = cls_ptr[c_offset];
                            max_class_id = c;
                        }
                        c_offset += grid_len;
                    }
                }

                if (max_class_id >= 0 && max_score > cls_th_i8) {
                    int b_offset = offset;
                    float before_dfl[64];
                    for (int k = 0; k < 64; ++k) {
                        before_dfl[k] = dequant_i8(box_ptr[b_offset], box_buf->zero_point, box_buf->scale);
                        b_offset += grid_len;
                    }

                    float dfl_box[4];
                    compute_dfl(before_dfl, dfl_len, dfl_box);

                    float x1 = (-dfl_box[0] + j + 0.5f) * stride;
                    float y1 = (-dfl_box[1] + i + 0.5f) * stride;
                    float x2 = (dfl_box[2] + j + 0.5f) * stride;
                    float y2 = (dfl_box[3] + i + 0.5f) * stride;

                    // Reverse letterbox mapping
                    aivision::cv::NormalizedBBox norm_box{
                        .x_min = x1 / 640.0f,
                        .y_min = y1 / 640.0f,
                        .x_max = x2 / 640.0f,
                        .y_max = y2 / 640.0f
                    };
                    auto unletterboxed = letterbox.unletterbox_bbox(norm_box, src_w, src_h);

                    DetectionBox det;
                    det.x = unletterboxed.x_min;
                    det.y = unletterboxed.y_min;
                    det.w = unletterboxed.x_max - unletterboxed.x_min;
                    det.h = unletterboxed.y_max - unletterboxed.y_min;
                    det.confidence = dequant_i8(max_score, cls_buf->zero_point, cls_buf->scale);
                    det.class_id = max_class_id;
                    det.label = get_label(max_class_id);

                    candidates.push_back(det);
                }
            }
        }
    }

    // NMS class-wise
    std::sort(candidates.begin(), candidates.end(), [](const DetectionBox& a, const DetectionBox& b) {
        return a.confidence > b.confidence;
    });

    std::vector<DetectionBox> results;
    std::vector<bool> suppressed(candidates.size(), false);

    for (size_t i = 0; i < candidates.size(); ++i) {
        if (suppressed[i]) continue;
        results.push_back(candidates[i]);
        for (size_t j = i + 1; j < candidates.size(); ++j) {
            if (suppressed[j]) continue;
            if (candidates[i].class_id == candidates[j].class_id) {
                if (aivision::cv::compute_iou_xywh(candidates[i], candidates[j]) > nms_threshold) {
                    suppressed[j] = true;
                }
            }
        }
    }

    return results;
}

} // namespace yolov8n
