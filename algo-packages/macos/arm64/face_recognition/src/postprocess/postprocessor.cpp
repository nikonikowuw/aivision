/**
 * @file postprocessor.cpp
 * @brief YOLO person 过滤、SCRFD 人脸关键点解码、人体/人脸关联、Embedding 处理与 JSON 序列化
 */

#include "postprocessor.hpp"
#include <algorithm>
#include <cmath>
#include <sstream>
#include <iomanip>

namespace face_recognition {

namespace {

inline float safe_clamp(float val, float min_v, float max_v, float fallback = 0.0f) {
    if (!std::isfinite(val)) return fallback;
    if (min_v > max_v) return min_v;
    return std::clamp(val, min_v, max_v);
}

// Base64 编码辅助函数
std::string base64_encode(const uint8_t* data, size_t len) {
    static const char base64_chars[] =
        "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
        "abcdefghijklmnopqrstuvwxyz"
        "0123456789+/";
    std::string ret;
    ret.reserve((len + 2) / 3 * 4);

    size_t i = 0;
    while (i < len) {
        const size_t remaining = len - i;
        const size_t bytes_in_block = std::min<std::size_t>(remaining, 3);
        const uint32_t octet_a = data[i++];
        const uint32_t octet_b = bytes_in_block > 1 ? data[i++] : 0;
        const uint32_t octet_c = bytes_in_block > 2 ? data[i++] : 0;
        const uint32_t triple = (octet_a << 16) + (octet_b << 8) + octet_c;

        ret.push_back(base64_chars[(triple >> 18) & 0x3F]);
        ret.push_back(base64_chars[(triple >> 12) & 0x3F]);
        ret.push_back(bytes_in_block > 1 ? base64_chars[(triple >> 6) & 0x3F] : '=');
        ret.push_back(bytes_in_block > 2 ? base64_chars[triple & 0x3F] : '=');
    }
    return ret;
}

} // namespace

std::vector<argus::cv::DetectionBox> Postprocessor::decode_yolo_persons(
    const YoloOutput& yolo_out,
    const argus::cv::LetterboxInfo& lb_info,
    uint32_t orig_w, uint32_t orig_h,
    float conf_thresh, float nms_thresh) {

    std::vector<argus::cv::DetectionBox> candidates;
    if (yolo_out.data.empty() || orig_w == 0 || orig_h == 0) return candidates;

    const size_t total_elements = yolo_out.data.size();
    const float net_w = lb_info.net_w > 0 ? static_cast<float>(lb_info.net_w) : 640.0f;
    const float net_h = lb_info.net_h > 0 ? static_cast<float>(lb_info.net_h) : 384.0f;

    // Case 1: YOLO26 / YOLOv10 end-to-end output [N, 6] -> (x1, y1, x2, y2, score, cls_id)
    if (total_elements % 6 == 0 && total_elements <= 3000) {
        const size_t num_boxes = total_elements / 6;
        for (size_t i = 0; i < num_boxes; ++i) {
            const float score = yolo_out.data[i * 6 + 4];
            const int cls_id = static_cast<int>(std::round(yolo_out.data[i * 6 + 5]));
            if (!std::isfinite(score) || score < conf_thresh || cls_id != 0) continue; // class 0 = person

            const float x1 = yolo_out.data[i * 6 + 0];
            const float y1 = yolo_out.data[i * 6 + 1];
            const float x2 = yolo_out.data[i * 6 + 2];
            const float y2 = yolo_out.data[i * 6 + 3];
            if (!std::isfinite(x1) || !std::isfinite(y1) || !std::isfinite(x2) || !std::isfinite(y2) ||
                x2 <= x1 || y2 <= y1) continue;

            argus::cv::NormalizedBBox norm_lb{
                .x_min = x1 / net_w,
                .y_min = y1 / net_h,
                .x_max = x2 / net_w,
                .y_max = y2 / net_h
            };
            argus::cv::NormalizedBBox orig_norm = lb_info.unletterbox_bbox(norm_lb, orig_w, orig_h);
            const float bw = orig_norm.x_max - orig_norm.x_min;
            const float bh = orig_norm.y_max - orig_norm.y_min;
            if (bw < 0.01f || bh < 0.01f) continue;

            argus::cv::DetectionBox det{};
            det.class_id = 0;
            det.label = "person";
            det.confidence = std::clamp(score, 0.0f, 1.0f);
            det.x = orig_norm.x_min;
            det.y = orig_norm.y_min;
            det.w = bw;
            det.h = bh;
            candidates.push_back(det);
        }
        return argus::cv::nms_filter(candidates, nms_thresh);
    }

    // Case 2: YOLOv8 raw anchor output [84, num_boxes]
    if (total_elements % 84 == 0) {
        const size_t num_boxes = total_elements / 84;
        for (size_t i = 0; i < num_boxes; ++i) {
            const float cx = yolo_out.data[0 * num_boxes + i];
            const float cy = yolo_out.data[1 * num_boxes + i];
            const float w = yolo_out.data[2 * num_boxes + i];
            const float h = yolo_out.data[3 * num_boxes + i];
            const float person_score = yolo_out.data[4 * num_boxes + i]; // class 0 = person
            if (!std::isfinite(person_score) || person_score < conf_thresh) continue;
            if (!std::isfinite(cx) || !std::isfinite(cy) || !std::isfinite(w) || !std::isfinite(h) ||
                w <= 0.0f || h <= 0.0f) continue;

            const float x1 = cx - w * 0.5f;
            const float y1 = cy - h * 0.5f;
            const float x2 = cx + w * 0.5f;
            const float y2 = cy + h * 0.5f;

            argus::cv::NormalizedBBox norm_lb{
                .x_min = x1 / net_w,
                .y_min = y1 / net_h,
                .x_max = x2 / net_w,
                .y_max = y2 / net_h
            };
            argus::cv::NormalizedBBox orig_norm = lb_info.unletterbox_bbox(norm_lb, orig_w, orig_h);
            const float bw = orig_norm.x_max - orig_norm.x_min;
            const float bh = orig_norm.y_max - orig_norm.y_min;
            if (bw < 0.01f || bh < 0.01f) continue;

            argus::cv::DetectionBox det{};
            det.class_id = 0;
            det.label = "person";
            det.confidence = std::clamp(person_score, 0.0f, 1.0f);
            det.x = orig_norm.x_min;
            det.y = orig_norm.y_min;
            det.w = bw;
            det.h = bh;
            candidates.push_back(det);
        }
        return argus::cv::nms_filter(candidates, nms_thresh);
    }

    return candidates;
}

std::vector<FaceDetection> Postprocessor::decode_scrfd_faces(
    const ScrfdOutput& scrfd_out,
    const argus::cv::LetterboxInfo& lb_info,
    uint32_t orig_w, uint32_t orig_h,
    float conf_thresh, float nms_thresh) {

    std::vector<FaceDetection> raw_faces;
    if (orig_w == 0 || orig_h == 0) return raw_faces;

    const int feat_strides[3] = {8, 16, 32};
    const float* scores[3] = {scrfd_out.score_8.data(), scrfd_out.score_16.data(), scrfd_out.score_32.data()};
    const float* bboxes[3] = {scrfd_out.bbox_8.data(), scrfd_out.bbox_16.data(), scrfd_out.bbox_32.data()};
    const float* kps[3] = {scrfd_out.kps_8.data(), scrfd_out.kps_16.data(), scrfd_out.kps_32.data()};

    const float pad_x = lb_info.pad_x;
    const float pad_y = lb_info.pad_y;
    const float scale = lb_info.scale;

    const float net_w = lb_info.net_w > 0 ? static_cast<float>(lb_info.net_w) : 640.0f;
    const float net_h = lb_info.net_h > 0 ? static_cast<float>(lb_info.net_h) : 384.0f;

    for (int stride_idx = 0; stride_idx < 3; ++stride_idx) {
        int stride = feat_strides[stride_idx];
        int feat_h = static_cast<int>(net_h) / stride;
        int feat_w = static_cast<int>(net_w) / stride;
        int num_anchors = 2; // SCRFD 10G has 2 anchors per location

        const float* score_ptr = scores[stride_idx];
        const float* bbox_ptr = bboxes[stride_idx];
        const float* kps_ptr = kps[stride_idx];

        int total_anchors = feat_h * feat_w * num_anchors;

        for (int a_idx = 0; a_idx < total_anchors; ++a_idx) {
            float score = score_ptr[a_idx];
            if (score < conf_thresh) continue;

            int loc_idx = a_idx / num_anchors;
            int cy_grid = loc_idx / feat_w;
            int cx_grid = loc_idx % feat_w;

            float anchor_x = static_cast<float>(cx_grid * stride);
            float anchor_y = static_cast<float>(cy_grid * stride);

            // Distance to bbox
            float d_x1 = bbox_ptr[a_idx * 4 + 0] * stride;
            float d_y1 = bbox_ptr[a_idx * 4 + 1] * stride;
            float d_x2 = bbox_ptr[a_idx * 4 + 2] * stride;
            float d_y2 = bbox_ptr[a_idx * 4 + 3] * stride;

            float x1_lb = anchor_x - d_x1;
            float y1_lb = anchor_y - d_y1;
            float x2_lb = anchor_x + d_x2;
            float y2_lb = anchor_y + d_y2;

            // Unletterbox to original image pixel coordinates
            FaceDetection face{};
            face.score = std::clamp(score, 0.0f, 1.0f);
            face.x1 = std::clamp((x1_lb - pad_x) / scale, 0.0f, static_cast<float>(orig_w));
            face.y1 = std::clamp((y1_lb - pad_y) / scale, 0.0f, static_cast<float>(orig_h));
            face.x2 = std::clamp((x2_lb - pad_x) / scale, 0.0f, static_cast<float>(orig_w));
            face.y2 = std::clamp((y2_lb - pad_y) / scale, 0.0f, static_cast<float>(orig_h));
            if (face.x2 <= face.x1 || face.y2 <= face.y1) continue;

            for (int k = 0; k < 5; ++k) {
                float kx_lb = anchor_x + kps_ptr[a_idx * 10 + k * 2 + 0] * stride;
                float ky_lb = anchor_y + kps_ptr[a_idx * 10 + k * 2 + 1] * stride;
                face.landmarks[k * 2 + 0] = std::clamp((kx_lb - pad_x) / scale, 0.0f, static_cast<float>(orig_w));
                face.landmarks[k * 2 + 1] = std::clamp((ky_lb - pad_y) / scale, 0.0f, static_cast<float>(orig_h));
            }

            raw_faces.push_back(face);
        }
    }

    if (raw_faces.empty()) return {};

    // Sort by score descending
    std::sort(raw_faces.begin(), raw_faces.end(), [](const FaceDetection& a, const FaceDetection& b) {
        return a.score > b.score;
    });

    // NMS on face boxes
    std::vector<FaceDetection> keep_faces;
    std::vector<bool> suppressed(raw_faces.size(), false);

    for (size_t i = 0; i < raw_faces.size(); ++i) {
        if (suppressed[i]) continue;
        keep_faces.push_back(raw_faces[i]);

        float area_i = (raw_faces[i].x2 - raw_faces[i].x1) * (raw_faces[i].y2 - raw_faces[i].y1);

        for (size_t j = i + 1; j < raw_faces.size(); ++j) {
            if (suppressed[j]) continue;

            float xx1 = std::max(raw_faces[i].x1, raw_faces[j].x1);
            float yy1 = std::max(raw_faces[i].y1, raw_faces[j].y1);
            float xx2 = std::min(raw_faces[i].x2, raw_faces[j].x2);
            float yy2 = std::min(raw_faces[i].y2, raw_faces[j].y2);

            float inter_w = std::max(0.0f, xx2 - xx1);
            float inter_h = std::max(0.0f, yy2 - yy1);
            float inter_area = inter_w * inter_h;

            float area_j = (raw_faces[j].x2 - raw_faces[j].x1) * (raw_faces[j].y2 - raw_faces[j].y1);
            float union_area = area_i + area_j - inter_area;

            if (union_area > 0.0f && (inter_area / union_area) > nms_thresh) {
                suppressed[j] = true;
            }
        }
    }

    return keep_faces;
}

bool Postprocessor::process_and_encode_embedding(
    const std::vector<float>& raw_embedding,
    std::string& out_base64,
    std::string& error) {

    if (raw_embedding.size() != 512) {
        error = "invalid embedding size: expected 512 floats";
        return false;
    }

    // Check for non-finite values (NaN / Inf)
    double l2_norm_sq = 0.0;
    for (float v : raw_embedding) {
        if (!std::isfinite(v)) {
            error = "embedding contains non-finite values";
            return false;
        }
        const double value = static_cast<double>(v);
        l2_norm_sq += value * value;
    }

    const double norm = std::sqrt(l2_norm_sq);
    if (!std::isfinite(norm) || norm < 1e-12) {
        error = "embedding norm is zero or invalid";
        return false;
    }

    std::vector<float> normalized(512);
    for (size_t i = 0; i < 512; ++i) {
        normalized[i] = static_cast<float>(static_cast<double>(raw_embedding[i]) / norm);
    }

    // Convert to little-endian bytes and Base64 encode
    // On Apple Silicon (ARM64), float is already IEEE 754 little-endian
    const uint8_t* byte_ptr = reinterpret_cast<const uint8_t*>(normalized.data());
    out_base64 = base64_encode(byte_ptr, 512 * sizeof(float));

    return true;
}

std::string Postprocessor::serialize_recognition_json(
    const std::vector<RecognizedPerson>& persons,
    uint64_t frame_id,
    uint64_t pts_ns) {

    std::ostringstream ss;
    ss << std::fixed << std::setprecision(6);
    ss << "{\n";
    ss << "  \"schema_version\": 1,\n";
    ss << "  \"frame_id\": " << frame_id << ",\n";
    ss << "  \"pts_ns\": " << pts_ns << ",\n";
    ss << "  \"algorithm_type\": \"face_recognition\",\n";
    ss << "  \"persons\": [\n";

    for (size_t i = 0; i < persons.size(); ++i) {
        const auto& p = persons[i];
        const float bx = safe_clamp(p.person_bbox[0], 0.0f, 0.9999f, 0.0f);
        const float by = safe_clamp(p.person_bbox[1], 0.0f, 0.9999f, 0.0f);
        const float bw = safe_clamp(p.person_bbox[2], 0.0001f, 1.0f - bx, 0.0001f);
        const float bh = safe_clamp(p.person_bbox[3], 0.0001f, 1.0f - by, 0.0001f);
        const float conf = safe_clamp(p.person_confidence, 0.0f, 1.0f, 0.0f);

        ss << "    {\n";
        ss << "      \"track_id\": " << p.track_id << ",\n";
        ss << "      \"target_type\": \"" << p.target_type << "\",\n";
        ss << "      \"bbox\": [" << bx << ", " << by << ", " << bw << ", " << bh << "],\n";
        ss << "      \"confidence\": " << conf << ",\n";

        if (p.has_face) {
            const float fx = safe_clamp(p.face_bbox[0], 0.0f, 0.9999f, 0.0f);
            const float fy = safe_clamp(p.face_bbox[1], 0.0f, 0.9999f, 0.0f);
            const float fw = safe_clamp(p.face_bbox[2], 0.0001f, 1.0f - fx, 0.0001f);
            const float fh = safe_clamp(p.face_bbox[3], 0.0001f, 1.0f - fy, 0.0001f);
            const float f_conf = safe_clamp(p.face_confidence, 0.0f, 1.0f, 0.0f);
            const float q_score = safe_clamp(p.face_quality, 0.0f, 1.0f, f_conf);

            ss << "      \"face\": {\n";
            ss << "        \"bbox\": [" << fx << ", " << fy << ", " << fw << ", " << fh << "],\n";
            ss << "        \"confidence\": " << f_conf << ",\n";
            ss << "        \"quality_score\": " << q_score << ",\n";
            ss << "        \"landmarks\": [\n";
            for (int k = 0; k < 5; ++k) {
                const float lx = safe_clamp(p.face_landmarks[k * 2], 0.0f, 1.0f, 0.0f);
                const float ly = safe_clamp(p.face_landmarks[k * 2 + 1], 0.0f, 1.0f, 0.0f);
                ss << "          [" << lx << ", " << ly << "]" << (k == 4 ? "" : ",") << "\n";
            }
            ss << "        ],\n";
            if (!p.embedding_base64.empty()) {
                ss << "        \"embedding\": {\n";
                ss << "          \"model\": \"glintr100\",\n";
                ss << "          \"dimension\": 512,\n";
                ss << "          \"dtype\": \"float32\",\n";
                ss << "          \"normalized\": true,\n";
                ss << "          \"encoding\": \"base64\",\n";
                ss << "          \"byte_order\": \"little_endian\",\n";
                ss << "          \"data\": \"" << p.embedding_base64 << "\"\n";
                ss << "        }\n";
            } else {
                ss << "        \"embedding\": null\n";
            }
            ss << "      }\n";
        } else {
            ss << "      \"face\": null\n";
        }

        ss << "    }" << (i + 1 == persons.size() ? "" : ",") << "\n";
    }

    ss << "  ]\n";
    ss << "}";
    return ss.str();
}

} // namespace face_recognition
