#include "postprocessor.hpp"
#include "../core/config.hpp"
#include <algorithm>
#include <cmath>
#include <cstring>
#include <iostream>

namespace yolov8n {

static std::vector<std::string> init_default_labels() {
    std::vector<std::string> labels;
    labels.reserve(80);
    for (int i = 0; i < 80; ++i) {
        labels.emplace_back(kCocoClasses[i]);
    }
    return labels;
}

static std::vector<std::string> g_labels = init_default_labels();

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
    const argus::cv::LetterboxInfo& letterbox,
    float conf_threshold,
    float nms_threshold,
    const std::bitset<80>& enabled_classes_mask,
    int src_w,
    int src_h
) {
    std::vector<DetectionBox> candidates;
    if (outputs.empty() || !enabled_classes_mask.any()) {
        return candidates;
    }

    const int strides[3] = {8, 16, 32};
    const int dfl_len = 16;
    const int num_classes = 80;

    // Case 1: Single combined tensor [1, 84, 5040] (cx, cy, w, h + 80 class scores)
    if (outputs.size() == 1 && outputs[0].data != nullptr) {
        constexpr int kAnchors = 5040;
        const auto& out_buf = outputs[0];

        if (out_buf.is_quantized) {
            const int8_t* ptr = reinterpret_cast<const int8_t*>(out_buf.data);
            int8_t conf_th_i8 = quant_f32(conf_threshold, out_buf.zero_point, out_buf.scale);

            for (int a = 0; a < kAnchors; ++a) {
                int max_class_id = -1;
                int8_t max_score = -128;

                for (int c = 0; c < num_classes; ++c) {
                    if (!enabled_classes_mask.test(static_cast<size_t>(c))) continue;
                    int8_t score_i8 = ptr[(4 + c) * kAnchors + a];
                    if (score_i8 > conf_th_i8 && score_i8 > max_score) {
                        max_score = score_i8;
                        max_class_id = c;
                    }
                }

                if (max_class_id >= 0 && max_score > conf_th_i8) {
                    float cx = dequant_i8(ptr[0 * kAnchors + a], out_buf.zero_point, out_buf.scale);
                    float cy = dequant_i8(ptr[1 * kAnchors + a], out_buf.zero_point, out_buf.scale);
                    float w = dequant_i8(ptr[2 * kAnchors + a], out_buf.zero_point, out_buf.scale);
                    float h = dequant_i8(ptr[3 * kAnchors + a], out_buf.zero_point, out_buf.scale);

                    float x1 = cx - w * 0.5f;
                    float y1 = cy - h * 0.5f;
                    float x2 = cx + w * 0.5f;
                    float y2 = cy + h * 0.5f;

                    argus::cv::NormalizedBBox norm_box{
                        .x_min = x1 / static_cast<float>(letterbox.net_w),
                        .y_min = y1 / static_cast<float>(letterbox.net_h),
                        .x_max = x2 / static_cast<float>(letterbox.net_w),
                        .y_max = y2 / static_cast<float>(letterbox.net_h)
                    };
                    auto unletterboxed = letterbox.unletterbox_bbox(norm_box, src_w, src_h);

                    DetectionBox det;
                    det.x = unletterboxed.x_min;
                    det.y = unletterboxed.y_min;
                    det.w = unletterboxed.x_max - unletterboxed.x_min;
                    det.h = unletterboxed.y_max - unletterboxed.y_min;
                    det.confidence = dequant_i8(max_score, out_buf.zero_point, out_buf.scale);
                    det.class_id = max_class_id;
                    det.label = get_label(max_class_id);

                    candidates.push_back(det);
                }
            }
        } else {
            const float* ptr = reinterpret_cast<const float*>(out_buf.data);
            for (int a = 0; a < kAnchors; ++a) {
                int max_class_id = -1;
                float max_score = 0.0f;

                for (int c = 0; c < num_classes; ++c) {
                    if (!enabled_classes_mask.test(static_cast<size_t>(c))) continue;
                    float score = ptr[(4 + c) * kAnchors + a];
                    if (score > conf_threshold && score > max_score) {
                        max_score = score;
                        max_class_id = c;
                    }
                }

                if (max_class_id >= 0 && max_score > conf_threshold) {
                    float cx = ptr[0 * kAnchors + a];
                    float cy = ptr[1 * kAnchors + a];
                    float w = ptr[2 * kAnchors + a];
                    float h = ptr[3 * kAnchors + a];

                    float x1 = cx - w * 0.5f;
                    float y1 = cy - h * 0.5f;
                    float x2 = cx + w * 0.5f;
                    float y2 = cy + h * 0.5f;

                    argus::cv::NormalizedBBox norm_box{
                        .x_min = x1 / static_cast<float>(letterbox.net_w),
                        .y_min = y1 / static_cast<float>(letterbox.net_h),
                        .x_max = x2 / static_cast<float>(letterbox.net_w),
                        .y_max = y2 / static_cast<float>(letterbox.net_h)
                    };
                    auto unletterboxed = letterbox.unletterbox_bbox(norm_box, src_w, src_h);

                    DetectionBox det;
                    det.x = unletterboxed.x_min;
                    det.y = unletterboxed.y_min;
                    det.w = unletterboxed.x_max - unletterboxed.x_min;
                    det.h = unletterboxed.y_max - unletterboxed.y_min;
                    det.confidence = max_score;
                    det.class_id = max_class_id;
                    det.label = get_label(max_class_id);

                    candidates.push_back(det);
                }
            }
        }
    } else {
        // Multi-branch decode (6 branches or 9 branches)
        bool is_9_branches = (outputs.size() >= 9);

        for (int s = 0; s < 3; ++s) {
            int stride = strides[s];
            int grid_w = letterbox.net_w / stride;
            int grid_h = letterbox.net_h / stride;
            int grid_len = grid_w * grid_h;

            const RknnOutputBuffer* box_buf = nullptr;
            const RknnOutputBuffer* cls_buf = nullptr;
            const RknnOutputBuffer* sum_buf = nullptr;

            if (is_9_branches) {
                box_buf = &outputs[s * 3 + 0];
                cls_buf = &outputs[s * 3 + 1];
                sum_buf = &outputs[s * 3 + 2];
            } else if (outputs.size() == 6) {
                // Check if ordered by [box0, cls0, box1, cls1, ...] or [box0, box1, box2, cls0, cls1, cls2]
                if (outputs[s * 2 + 0].size == static_cast<size_t>(64 * grid_len)) {
                    box_buf = &outputs[s * 2 + 0];
                    cls_buf = &outputs[s * 2 + 1];
                } else if (outputs[s].size == static_cast<size_t>(64 * grid_len) &&
                           outputs[s + 3].size == static_cast<size_t>(80 * grid_len)) {
                    box_buf = &outputs[s];
                    cls_buf = &outputs[s + 3];
                } else {
                    box_buf = &outputs[s * 2 + 0];
                    cls_buf = &outputs[s * 2 + 1];
                }
            } else if (outputs.size() == 3) {
                box_buf = &outputs[s];
            } else {
                continue;
            }

            if (!box_buf || !box_buf->data) continue;

            const int8_t* box_ptr = reinterpret_cast<const int8_t*>(box_buf->data);
            const int8_t* cls_ptr = (cls_buf && cls_buf->data) ? reinterpret_cast<const int8_t*>(cls_buf->data) : nullptr;
            const int8_t* sum_ptr = (sum_buf && sum_buf->data) ? reinterpret_cast<const int8_t*>(sum_buf->data) : nullptr;

            int8_t cls_th_i8 = cls_buf ? quant_f32(conf_threshold, cls_buf->zero_point, cls_buf->scale) : 0;
            int8_t sum_th_i8 = sum_buf ? quant_f32(conf_threshold, sum_buf->zero_point, sum_buf->scale) : 0;

            for (int i = 0; i < grid_h; ++i) {
                for (int j = 0; j < grid_w; ++j) {
                    int offset = i * grid_w + j;

                    if (sum_ptr && sum_ptr[offset] < sum_th_i8) {
                        continue;
                    }

                    int max_class_id = -1;
                    int8_t max_score = cls_buf ? -128 : 0;

                    if (cls_ptr) {
                        int c_offset = offset;
                        for (int c = 0; c < num_classes; ++c) {
                            if (enabled_classes_mask.test(static_cast<size_t>(c))) {
                                int8_t val = cls_ptr[c_offset];
                                if (val > cls_th_i8 && val > max_score) {
                                    max_score = val;
                                    max_class_id = c;
                                }
                            }
                            c_offset += grid_len;
                        }
                    }

                    // Early exit: If no enabled class surpassed threshold at this anchor, skip DFL computation!
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
                        argus::cv::NormalizedBBox norm_box{
                            .x_min = x1 / static_cast<float>(letterbox.net_w),
                            .y_min = y1 / static_cast<float>(letterbox.net_h),
                            .x_max = x2 / static_cast<float>(letterbox.net_w),
                            .y_max = y2 / static_cast<float>(letterbox.net_h)
                        };
                        auto unletterboxed = letterbox.unletterbox_bbox(norm_box, src_w, src_h);

                        DetectionBox det;
                        det.x = unletterboxed.x_min;
                        det.y = unletterboxed.y_min;
                        det.w = unletterboxed.x_max - unletterboxed.x_min;
                        det.h = unletterboxed.y_max - unletterboxed.y_min;
                        det.confidence = cls_buf ? dequant_i8(max_score, cls_buf->zero_point, cls_buf->scale) : 0.0f;
                        det.class_id = max_class_id;
                        det.label = get_label(max_class_id);

                        candidates.push_back(det);
                    }
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
                if (argus::cv::compute_iou_xywh(candidates[i], candidates[j]) > nms_threshold) {
                    suppressed[j] = true;
                }
            }
        }
    }

    return results;
}

} // namespace yolov8n
