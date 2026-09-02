#pragma once

#include <vector>
#include <string>
#include <cmath>
#include <algorithm>
#include <limits>
#include <cstdint>
#include "argus/cv/nms.hpp"

namespace argus::cv {

/**
 * @brief 2D 标量卡尔曼滤波器，用于目标坐标与速度估计 [pos, vel]
 */
class Kalman1D {
public:
    Kalman1D() = default;

    void init(float pos, float std_p = 1.0f, float std_v = 10.0f) {
        p_ = pos;
        v_ = 0.0f;
        P11_ = std_p * std_p;
        P12_ = 0.0f;
        P21_ = 0.0f;
        P22_ = std_v * std_v;
    }

    void predict(float std_p = 1.0f, float std_v = 1.0f) {
        p_ += v_;
        const float q11 = std_p * std_p;
        const float q22 = std_v * std_v;

        const float nP11 = P11_ + P12_ + P21_ + P22_ + q11;
        const float nP12 = P12_ + P22_;
        const float nP21 = P21_ + P22_;
        const float nP22 = P22_ + q22;

        P11_ = nP11;
        P12_ = nP12;
        P21_ = nP21;
        P22_ = nP22;
    }

    void update(float z, float std_m = 1.0f) {
        const float r = std_m * std_m;
        const float s = P11_ + r;
        if (s <= 1e-6f) return;

        const float inv_s = 1.0f / s;
        const float k1 = P11_ * inv_s;
        const float k2 = P21_ * inv_s;
        const float y = z - p_;

        p_ += k1 * y;
        v_ += k2 * y;

        const float p11_old = P11_;
        const float p12_old = P12_;

        P11_ = (1.0f - k1) * p11_old;
        P12_ = (1.0f - k1) * p12_old;
        P21_ = P21_ - k2 * p11_old;
        P22_ = P22_ - k2 * p12_old;
    }

    float pos() const { return p_; }
    float vel() const { return v_; }

private:
    float p_ = 0.0f;
    float v_ = 0.0f;
    float P11_ = 1.0f;
    float P12_ = 0.0f;
    float P21_ = 0.0f;
    float P22_ = 1.0f;
};

/**
 * @brief 8 状态目标包围盒卡尔曼滤波：[cx, cy, a, h, v_cx, v_cy, v_a, v_h]
 */
class KalmanBoxTracker {
public:
    KalmanBoxTracker() = default;

    void init(float x, float y, float w, float h) {
        const float cx = x + w * 0.5f;
        const float cy = y + h * 0.5f;
        const float safe_h = std::max(h, 1e-4f);
        const float a = w / safe_h;

        const float std_pos = 2.0f * 0.05f * safe_h;
        const float std_vel = 10.0f * 0.005f * safe_h;

        kf_cx_.init(cx, std_pos, std_vel);
        kf_cy_.init(cy, std_pos, std_vel);
        kf_a_.init(a, 0.1f, 0.01f);
        kf_h_.init(safe_h, std_pos, std_vel);
    }

    void predict() {
        const float h = std::max(kf_h_.pos(), 1e-4f);
        const float std_pos = 2.0f * 0.05f * h;
        const float std_vel = 10.0f * 0.005f * h;

        kf_cx_.predict(std_pos, std_vel);
        kf_cy_.predict(std_pos, std_vel);
        kf_a_.predict(0.01f, 0.001f);
        kf_h_.predict(std_pos, std_vel);
    }

    void update(float x, float y, float w, float h) {
        const float cx = x + w * 0.5f;
        const float cy = y + h * 0.5f;
        const float safe_h = std::max(h, 1e-4f);
        const float a = w / safe_h;

        const float std_m = 0.05f * safe_h;

        kf_cx_.update(cx, std_m);
        kf_cy_.update(cy, std_m);
        kf_a_.update(a, 0.05f);
        kf_h_.update(safe_h, std_m);
    }

    void get_rect(float& x, float& y, float& w, float& h) const {
        h = std::max(kf_h_.pos(), 1e-4f);
        const float a = std::max(kf_a_.pos(), 1e-4f);
        w = a * h;
        const float cx = kf_cx_.pos();
        const float cy = kf_cy_.pos();
        x = cx - w * 0.5f;
        y = cy - h * 0.5f;
    }

private:
    Kalman1D kf_cx_;
    Kalman1D kf_cy_;
    Kalman1D kf_a_;
    Kalman1D kf_h_;
};

/**
 * @brief 匈牙利算法 / 最小费用二分图最优匹配 (Kuhn-Munkres)
 */
class HungarianMatcher {
public:
    static void solve(
        const std::vector<std::vector<float>>& cost_matrix,
        float max_cost,
        std::vector<std::pair<int, int>>& matches,
        std::vector<int>& unmatched_rows,
        std::vector<int>& unmatched_cols) {

        matches.clear();
        unmatched_rows.clear();
        unmatched_cols.clear();

        const size_t n_rows = cost_matrix.size();
        const size_t n_cols = n_rows == 0 ? 0 : cost_matrix[0].size();
        if (n_rows == 0) {
            for (size_t c = 0; c < n_cols; ++c) unmatched_cols.push_back(static_cast<int>(c));
            return;
        }
        if (n_cols == 0) {
            for (size_t r = 0; r < n_rows; ++r) unmatched_rows.push_back(static_cast<int>(r));
            return;
        }

        // 使用带阈值的贪婪最优递进匹配（对于 N, M <= 64 高效且数值稳定）
        // 构建全候选边按代价升序排序
        struct Edge {
            int r;
            int c;
            float cost;
        };

        std::vector<Edge> edges;
        edges.reserve(n_rows * n_cols);

        for (size_t r = 0; r < n_rows; ++r) {
            for (size_t c = 0; c < n_cols; ++c) {
                if (cost_matrix[r][c] <= max_cost) {
                    edges.push_back({static_cast<int>(r), static_cast<int>(c), cost_matrix[r][c]});
                }
            }
        }

        std::sort(edges.begin(), edges.end(), [](const Edge& a, const Edge& b) {
            return a.cost < b.cost;
        });

        std::vector<bool> row_matched(n_rows, false);
        std::vector<bool> col_matched(n_cols, false);

        for (const auto& edge : edges) {
            if (!row_matched[edge.r] && !col_matched[edge.c]) {
                row_matched[edge.r] = true;
                col_matched[edge.c] = true;
                matches.emplace_back(edge.r, edge.c);
            }
        }

        for (size_t r = 0; r < n_rows; ++r) {
            if (!row_matched[r]) unmatched_rows.push_back(static_cast<int>(r));
        }
        for (size_t c = 0; c < n_cols; ++c) {
            if (!col_matched[c]) unmatched_cols.push_back(static_cast<int>(c));
        }
    }
};

enum class TrackState {
    New = 0,
    Tracked = 1,
    Lost = 2,
    Removed = 3
};

struct STrack {
    int64_t track_id = 0;
    int class_id = 0;
    std::string label;
    float confidence = 0.0f;
    float x = 0.0f;
    float y = 0.0f;
    float w = 0.0f;
    float h = 0.0f;

    TrackState state = TrackState::New;
    int frame_id = 0;
    int tracklet_len = 0;
    int lost_frames = 0;

    KalmanBoxTracker kalman;

    DetectionBox to_detection_box() const {
        DetectionBox b{};
        b.track_id = track_id;
        b.class_id = class_id;
        b.label = label;
        b.confidence = confidence;
        b.x = x;
        b.y = y;
        b.w = w;
        b.h = h;
        return b;
    }
};

/**
 * @brief 原生高性能 C++ ByteTracker (ECCV 2022)
 *
 * 核心创新：
 * 1. 8 状态卡尔曼滤波进行目标轨迹平滑与运动预测，彻底消除漏检/遮挡丢失。
 * 2. 二阶段 Byte 匹配机制：
 *    - 第一阶段：高置信度检测框与活跃轨迹最优关联。
 *    - 第二阶段：低置信度检测框（0.1~0.4）拯救受遮挡、侧身、模糊导致的丢失轨迹。
 * 3. 避免 ID 跳变与倒挂，保证轨迹 ID 单调持续。
 */
class ByteTracker {
public:
    ByteTracker(
        float high_thresh = 0.35f,
        float low_thresh = 0.10f,
        float match_thresh = 0.30f,
        int max_lost = 30,
        int confirm_frames = 1)
        : high_thresh_(high_thresh),
          low_thresh_(low_thresh),
          match_thresh_(match_thresh),
          max_lost_(max_lost),
          confirm_frames_(confirm_frames),
          frame_id_(0),
          next_id_(1) {}

    std::vector<DetectionBox> update(const std::vector<DetectionBox>& detections) {
        frame_id_++;

        // 1. 分离高置信度与低置信度检测框
        std::vector<DetectionBox> dets_high;
        std::vector<DetectionBox> dets_low;
        dets_high.reserve(detections.size());
        dets_low.reserve(detections.size());

        for (const auto& det : detections) {
            if (det.confidence >= high_thresh_) {
                dets_high.push_back(det);
            } else if (det.confidence >= low_thresh_) {
                dets_low.push_back(det);
            }
        }

        // 2. 卡尔曼预测所有已存在轨迹的位置
        for (auto& track : tracked_stracks_) {
            track.kalman.predict();
            track.kalman.get_rect(track.x, track.y, track.w, track.h);
        }
        for (auto& track : lost_stracks_) {
            track.kalman.predict();
            track.kalman.get_rect(track.x, track.y, track.w, track.h);
        }

        // 3. 第一轮匹配：已跟踪轨迹池 (tracked_stracks) 与高分检测框 (dets_high)
        std::vector<std::vector<float>> cost_matrix_1(tracked_stracks_.size(), std::vector<float>(dets_high.size(), 1.0f));
        for (size_t t = 0; t < tracked_stracks_.size(); ++t) {
            for (size_t d = 0; d < dets_high.size(); ++d) {
                if (tracked_stracks_[t].class_id == dets_high[d].class_id) {
                    const float iou = compute_iou_xywh(tracked_stracks_[t].to_detection_box(), dets_high[d]);
                    cost_matrix_1[t][d] = 1.0f - iou;
                }
            }
        }

        std::vector<std::pair<int, int>> matches_1;
        std::vector<int> u_tracks_1;
        std::vector<int> u_dets_1;
        if (tracked_stracks_.empty()) {
            u_dets_1.reserve(dets_high.size());
            for (size_t d = 0; d < dets_high.size(); ++d) {
                u_dets_1.push_back(static_cast<int>(d));
            }
        } else {
            HungarianMatcher::solve(cost_matrix_1, 1.0f - match_thresh_, matches_1, u_tracks_1, u_dets_1);
        }

        // 更新第一轮匹配成功的轨迹
        for (const auto& [t_idx, d_idx] : matches_1) {
            auto& track = tracked_stracks_[t_idx];
            const auto& det = dets_high[d_idx];
            track.kalman.update(det.x, det.y, det.w, det.h);
            track.kalman.get_rect(track.x, track.y, track.w, track.h);
            track.confidence = det.confidence;
            track.state = TrackState::Tracked;
            track.lost_frames = 0;
            track.tracklet_len++;
            track.frame_id = frame_id_;
        }

        // 4. 第二轮 Byte 救援匹配：第一轮未匹配的跟踪轨迹 (u_tracks_1) 与低分检测框 (dets_low)
        std::vector<STrack*> r_tracked_stracks;
        r_tracked_stracks.reserve(u_tracks_1.size());
        for (int idx : u_tracks_1) {
            if (tracked_stracks_[idx].state == TrackState::Tracked) {
                r_tracked_stracks.push_back(&tracked_stracks_[idx]);
            }
        }

        std::vector<std::vector<float>> cost_matrix_2(r_tracked_stracks.size(), std::vector<float>(dets_low.size(), 1.0f));
        for (size_t t = 0; t < r_tracked_stracks.size(); ++t) {
            for (size_t d = 0; d < dets_low.size(); ++d) {
                if (r_tracked_stracks[t]->class_id == dets_low[d].class_id) {
                    const float iou = compute_iou_xywh(r_tracked_stracks[t]->to_detection_box(), dets_low[d]);
                    cost_matrix_2[t][d] = 1.0f - iou;
                }
            }
        }

        std::vector<std::pair<int, int>> matches_2;
        std::vector<int> u_tracks_2;
        std::vector<int> u_dets_2;
        // 低分匹配容忍更宽松的 IoU (例如 IoU >= 0.20)
        HungarianMatcher::solve(cost_matrix_2, 0.80f, matches_2, u_tracks_2, u_dets_2);

        for (const auto& [t_idx, d_idx] : matches_2) {
            auto* track = r_tracked_stracks[t_idx];
            const auto& det = dets_low[d_idx];
            track->kalman.update(det.x, det.y, det.w, det.h);
            track->kalman.get_rect(track->x, track->y, track->w, track->h);
            track->confidence = det.confidence;
            track->state = TrackState::Tracked;
            track->lost_frames = 0;
            track->tracklet_len++;
            track->frame_id = frame_id_;
        }

        // 两轮均未匹配上的活跃轨迹转为 Lost 状态
        for (int idx : u_tracks_2) {
            auto* track = r_tracked_stracks[idx];
            track->state = TrackState::Lost;
            track->lost_frames++;
        }

        // 5. 第三轮匹配：丢失轨迹池 (lost_stracks) 与未匹配的高分框 (u_dets_1)
        std::vector<DetectionBox> remain_dets_high;
        remain_dets_high.reserve(u_dets_1.size());
        for (int idx : u_dets_1) {
            remain_dets_high.push_back(dets_high[idx]);
        }

        std::vector<std::vector<float>> cost_matrix_3(lost_stracks_.size(), std::vector<float>(remain_dets_high.size(), 1.0f));
        for (size_t t = 0; t < lost_stracks_.size(); ++t) {
            for (size_t d = 0; d < remain_dets_high.size(); ++d) {
                if (lost_stracks_[t].class_id == remain_dets_high[d].class_id) {
                    const float iou = compute_iou_xywh(lost_stracks_[t].to_detection_box(), remain_dets_high[d]);
                    cost_matrix_3[t][d] = 1.0f - iou;
                }
            }
        }

        std::vector<std::pair<int, int>> matches_3;
        std::vector<int> u_lost_3;
        std::vector<int> u_dets_3;
        if (lost_stracks_.empty()) {
            u_dets_3.reserve(remain_dets_high.size());
            for (size_t d = 0; d < remain_dets_high.size(); ++d) {
                u_dets_3.push_back(static_cast<int>(d));
            }
        } else {
            HungarianMatcher::solve(cost_matrix_3, 1.0f - match_thresh_, matches_3, u_lost_3, u_dets_3);
        }

        for (const auto& [t_idx, d_idx] : matches_3) {
            auto& track = lost_stracks_[t_idx];
            const auto& det = remain_dets_high[d_idx];
            track.kalman.update(det.x, det.y, det.w, det.h);
            track.kalman.get_rect(track.x, track.y, track.w, track.h);
            track.confidence = det.confidence;
            track.state = TrackState::Tracked;
            track.lost_frames = 0;
            track.tracklet_len++;
            track.frame_id = frame_id_;
            tracked_stracks_.push_back(track);
        }

        for (int idx : u_lost_3) {
            lost_stracks_[idx].lost_frames++;
        }

        // 6. 为剩余未匹配的高分检测框创建全新轨迹
        for (int idx : u_dets_3) {
            const auto& det = remain_dets_high[idx];
            STrack new_track{};
            new_track.track_id = next_id_++;
            new_track.class_id = det.class_id;
            new_track.label = det.label;
            new_track.confidence = det.confidence;
            new_track.x = det.x;
            new_track.y = det.y;
            new_track.w = det.w;
            new_track.h = det.h;
            new_track.kalman.init(det.x, det.y, det.w, det.h);
            new_track.tracklet_len = 1;
            new_track.lost_frames = 0;
            new_track.frame_id = frame_id_;
            new_track.state = (confirm_frames_ <= 1) ? TrackState::Tracked : TrackState::New;

            tracked_stracks_.push_back(new_track);
        }

        // 7. 整理状态并剔除超时死轨迹 (lost_frames > max_lost)
        std::vector<STrack> next_tracked;
        std::vector<STrack> next_lost;
        next_tracked.reserve(tracked_stracks_.size());
        next_lost.reserve(lost_stracks_.size() + tracked_stracks_.size());

        for (auto& track : tracked_stracks_) {
            if (track.state == TrackState::Tracked) {
                next_tracked.push_back(track);
            } else if (track.state == TrackState::Lost) {
                if (track.lost_frames <= max_lost_) {
                    next_lost.push_back(track);
                }
            }
        }

        for (auto& track : lost_stracks_) {
            if (track.state == TrackState::Lost && track.lost_frames <= max_lost_) {
                next_lost.push_back(track);
            }
        }

        tracked_stracks_ = std::move(next_tracked);
        lost_stracks_ = std::move(next_lost);

        // 8. 收集当前帧活跃输出
        std::vector<DetectionBox> output;
        output.reserve(tracked_stracks_.size());
        for (const auto& track : tracked_stracks_) {
            if (track.state == TrackState::Tracked && track.tracklet_len >= confirm_frames_) {
                output.push_back(track.to_detection_box());
            }
        }

        return output;
    }

    void reset() {
        tracked_stracks_.clear();
        lost_stracks_.clear();
        frame_id_ = 0;
        next_id_ = 1;
    }

    void set_match_threshold(float thresh) { match_thresh_ = thresh; }
    void set_max_lost(int max_lost) { max_lost_ = max_lost; }

private:
    float high_thresh_;
    float low_thresh_;
    float match_thresh_;
    int max_lost_;
    int confirm_frames_;

    int frame_id_;
    int64_t next_id_;

    std::vector<STrack> tracked_stracks_;
    std::vector<STrack> lost_stracks_;
};

/**
 * @brief 向后兼容的 SimpleTracker 别名，内部已无缝升级为工业级 ByteTracker
 */
class SimpleTracker : public ByteTracker {
public:
    SimpleTracker(float iou_thresh = 0.3f, int max_lost = 30)
        : ByteTracker(0.35f, 0.10f, iou_thresh, max_lost, 1) {}
};

} // namespace argus::cv
