#pragma once

#include <vector>
#include <string>
#include <cmath>
#include <algorithm>
#include "aivision/cv/nms.hpp"

namespace aivision::cv {

struct TrackedObject {
    int64_t track_id;
    int class_id;
    std::string label;
    float x;
    float y;
    float w;
    float h;
    float confidence;
    int lost_frames = 0;
};

// Lightweight ByteTrack/IOU-based Multi-Object Tracker
class SimpleTracker {
public:
    SimpleTracker(float iou_thresh = 0.3f, int max_lost = 30)
        : iou_thresh_(iou_thresh), max_lost_(max_lost), next_id_(1) {}

    std::vector<DetectionBox> update(const std::vector<DetectionBox>& detections) {
        std::vector<DetectionBox> tracked_results;
        std::vector<bool> matched_det(detections.size(), false);
        std::vector<bool> matched_trk(tracks_.size(), false);

        // Match existing tracks with new detections using IoU
        for (size_t t = 0; t < tracks_.size(); ++t) {
            float best_iou = 0.0f;
            int best_d = -1;

            DetectionBox trk_box{};
            trk_box.x = tracks_[t].x;
            trk_box.y = tracks_[t].y;
            trk_box.w = tracks_[t].w;
            trk_box.h = tracks_[t].h;

            for (size_t d = 0; d < detections.size(); ++d) {
                if (matched_det[d]) continue;
                if (detections[d].class_id != tracks_[t].class_id) continue;

                float iou = compute_iou_xywh(trk_box, detections[d]);
                if (iou > best_iou) {
                    best_iou = iou;
                    best_d = static_cast<int>(d);
                }
            }

            if (best_iou >= iou_thresh_ && best_d >= 0) {
                matched_trk[t] = true;
                matched_det[best_d] = true;

                // Update track position with smoothing
                tracks_[t].x = 0.8f * detections[best_d].x + 0.2f * tracks_[t].x;
                tracks_[t].y = 0.8f * detections[best_d].y + 0.2f * tracks_[t].y;
                tracks_[t].w = 0.8f * detections[best_d].w + 0.2f * tracks_[t].w;
                tracks_[t].h = 0.8f * detections[best_d].h + 0.2f * tracks_[t].h;
                tracks_[t].confidence = detections[best_d].confidence;
                tracks_[t].lost_frames = 0;

                DetectionBox out = detections[best_d];
                out.track_id = tracks_[t].track_id;
                tracked_results.push_back(out);
            }
        }

        // Increment lost frames for unmatched tracks
        for (size_t t = 0; t < tracks_.size(); ++t) {
            if (!matched_trk[t]) {
                tracks_[t].lost_frames++;
            }
        }

        // Remove dead tracks
        tracks_.erase(
            std::remove_if(tracks_.begin(), tracks_.end(), [this](const TrackedObject& obj) {
                return obj.lost_frames > max_lost_;
            }),
            tracks_.end()
        );

        // Spawn new tracks for unmatched detections
        for (size_t d = 0; d < detections.size(); ++d) {
            if (!matched_det[d]) {
                TrackedObject new_trk{};
                new_trk.track_id = next_id_++;
                new_trk.class_id = detections[d].class_id;
                new_trk.label = detections[d].label;
                new_trk.x = detections[d].x;
                new_trk.y = detections[d].y;
                new_trk.w = detections[d].w;
                new_trk.h = detections[d].h;
                new_trk.confidence = detections[d].confidence;
                new_trk.lost_frames = 0;

                tracks_.push_back(new_trk);

                DetectionBox out = detections[d];
                out.track_id = new_trk.track_id;
                tracked_results.push_back(out);
            }
        }

        return tracked_results;
    }

    void reset() {
        tracks_.clear();
        next_id_ = 1;
    }

private:
    float iou_thresh_;
    int max_lost_;
    int64_t next_id_;
    std::vector<TrackedObject> tracks_;
};

} // namespace aivision::cv
