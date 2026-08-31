#pragma once

#include "argus/algo.h"
#include "argus/cv/nms.hpp"

#include <cmath>
#include <cstddef>
#include <cstdint>
#include <string>
#include <unordered_map>
#include <unordered_set>
#include <utility>
#include <vector>

namespace yolo26n {

struct RuleState {
    uint32_t role = 0;
    uint32_t mode = 0;
    std::vector<av_point> points;
};

inline constexpr uint32_t kMaxRuleTrackMissedFrames = 30;

inline bool point_in_polygon(const av_point& point, const std::vector<av_point>& polygon) {
    bool inside = false;
    for (size_t i = 0, j = polygon.size() - 1; i < polygon.size(); j = i++) {
        const auto& a = polygon[i];
        const auto& b = polygon[j];
        const bool crosses = ((a.y > point.y) != (b.y > point.y)) &&
                             (point.x < (b.x - a.x) * (point.y - a.y) / (b.y - a.y) + a.x);
        if (crosses) inside = !inside;
    }
    return inside;
}

inline float cross(const av_point& a, const av_point& b, const av_point& c) {
    return (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x);
}

inline bool segments_intersect(const av_point& a, const av_point& b,
                               const av_point& c, const av_point& d) {
    constexpr float kEpsilon = 1e-6f;
    const auto cross_prod = [](const av_point& o, const av_point& first, const av_point& second) {
        return (first.x - o.x) * (second.y - o.y) - (first.y - o.y) * (second.x - o.x);
    };
    const auto on_segment = [](const av_point& p, const av_point& q, const av_point& r) {
        return q.x >= std::min(p.x, r.x) - kEpsilon &&
               q.x <= std::max(p.x, r.x) + kEpsilon &&
               q.y >= std::min(p.y, r.y) - kEpsilon &&
               q.y <= std::max(p.y, r.y) + kEpsilon;
    };

    const float d1 = cross_prod(c, d, a);
    const float d2 = cross_prod(c, d, b);
    const float d3 = cross_prod(a, b, c);
    const float d4 = cross_prod(a, b, d);

    if (((d1 > kEpsilon && d2 < -kEpsilon) || (d1 < -kEpsilon && d2 > kEpsilon)) &&
        ((d3 > kEpsilon && d4 < -kEpsilon) || (d3 < -kEpsilon && d4 > kEpsilon))) {
        return true;
    }
    return (std::fabs(d1) <= kEpsilon && on_segment(c, a, d)) ||
           (std::fabs(d2) <= kEpsilon && on_segment(c, b, d)) ||
           (std::fabs(d3) <= kEpsilon && on_segment(a, c, b)) ||
           (std::fabs(d4) <= kEpsilon && on_segment(a, d, b));
}

inline float polygon_signed_area(const std::vector<av_point>& polygon) {
    float area = 0.0f;
    for (size_t i = 0; i < polygon.size(); ++i) {
        const auto& current = polygon[i];
        const auto& next = polygon[(i + 1) % polygon.size()];
        area += current.x * next.y - next.x * current.y;
    }
    return area * 0.5f;
}

inline bool validate_and_copy_rules(const av_rule* rules, uint32_t count,
                                    std::vector<RuleState>& out, std::string& error) {
    if (count > 64) {
        error = "rule count exceeds 64";
        return false;
    }
    if (count > 0 && !rules) {
        error = "rules pointer is null";
        return false;
    }

    std::vector<RuleState> candidate;
    candidate.reserve(count);
    for (uint32_t i = 0; i < count; ++i) {
        const av_rule& rule = rules[i];
        if (rule.size < sizeof(av_rule) || rule.api_version != AV_ALGO_API_VERSION || !rule.points) {
            error = "rule header is invalid";
            return false;
        }
        if (rule.role != AV_RULE_ROI && rule.role != AV_RULE_MASK && rule.role != AV_RULE_LINE) {
            error = "rule role is invalid";
            return false;
        }
        const uint32_t min_points = rule.role == AV_RULE_LINE ? 2u : 3u;
        if (rule.point_count < min_points || rule.point_count > 256) {
            error = "rule point count is invalid";
            return false;
        }
        if (rule.role == AV_RULE_LINE && rule.mode > AV_LINE_DIR_B_TO_A) {
            error = "rule line direction is invalid";
            return false;
        }
        if (rule.role != AV_RULE_LINE && rule.mode != 0) {
            error = "region rule mode must be zero";
            return false;
        }

        RuleState state;
        state.role = rule.role;
        state.mode = rule.mode;
        state.points.assign(rule.points, rule.points + rule.point_count);
        for (const av_point& point : state.points) {
            if (!std::isfinite(point.x) || !std::isfinite(point.y) || point.x < 0.0f || point.x > 1.0f ||
                point.y < 0.0f || point.y > 1.0f) {
                error = "rule point must be finite and normalized";
                return false;
            }
        }

        const size_t segment_count = rule.role == AV_RULE_LINE ? state.points.size() - 1 : state.points.size();
        for (size_t point_index = 0; point_index < segment_count; ++point_index) {
            const auto& point = state.points[point_index];
            const auto& next = state.points[(point_index + 1) % state.points.size()];
            if (std::fabs(point.x - next.x) <= 1e-6f && std::fabs(point.y - next.y) <= 1e-6f) {
                error = "rule contains a zero-length segment";
                return false;
            }
        }

        if (rule.role != AV_RULE_LINE) {
            if (std::fabs(polygon_signed_area(state.points)) <= 1e-6f) {
                error = "region rule polygon has zero area";
                return false;
            }
            for (size_t a = 0; a < state.points.size(); ++a) {
                const size_t a_next = (a + 1) % state.points.size();
                for (size_t b = a + 1; b < state.points.size(); ++b) {
                    const size_t b_next = (b + 1) % state.points.size();
                    if (a == b || a_next == b || b_next == a) continue;
                    if (segments_intersect(state.points[a], state.points[a_next], state.points[b], state.points[b_next])) {
                        error = "region rule polygon self-intersects";
                        return false;
                    }
                }
            }
        }
        candidate.push_back(std::move(state));
    }

    out = std::move(candidate);
    return true;
}

inline bool line_crossed(const av_point& previous, const av_point& current,
                         const av_point& line_a, const av_point& line_b, uint32_t direction) {
    const float previous_side = cross(line_a, line_b, previous);
    const float current_side = cross(line_a, line_b, current);
    if (std::fabs(previous_side) < 1e-6f || std::fabs(current_side) < 1e-6f || previous_side * current_side >= 0.0f) {
        return false;
    }
    if (direction == AV_LINE_DIR_BOTH) return true;
    const bool a_to_b = previous_side < current_side;
    return direction == AV_LINE_DIR_A_TO_B ? a_to_b : !a_to_b;
}

inline std::vector<argus::cv::DetectionBox> apply_rules(
    const std::vector<RuleState>& rules,
    std::unordered_map<int64_t, av_point>& previous_points,
    std::unordered_map<int64_t, uint32_t>& missed_frames,
    const std::vector<argus::cv::DetectionBox>& objects) {
    if (rules.empty()) return objects;

    bool has_roi = false;
    bool has_line = false;
    for (const RuleState& rule : rules) {
        has_roi = has_roi || rule.role == AV_RULE_ROI;
        has_line = has_line || rule.role == AV_RULE_LINE;
    }

    if (has_line) {
        std::unordered_set<int64_t> current_track_ids;
        current_track_ids.reserve(objects.size());
        for (const auto& object : objects) {
            if (object.track_id >= 0) current_track_ids.insert(object.track_id);
        }
        for (auto it = previous_points.begin(); it != previous_points.end();) {
            if (current_track_ids.contains(it->first)) {
                missed_frames[it->first] = 0;
                ++it;
                continue;
            }

            uint32_t& missed = missed_frames[it->first];
            ++missed;
            if (missed > kMaxRuleTrackMissedFrames) {
                missed_frames.erase(it->first);
                it = previous_points.erase(it);
            } else {
                ++it;
            }
        }
    }

    std::vector<argus::cv::DetectionBox> filtered;
    for (const auto& object : objects) {
        const av_point point{object.x + object.w * 0.5f, object.y + object.h};
        bool masked = false;
        bool in_roi = false;
        bool crossed = false;
        for (const RuleState& rule : rules) {
            if (rule.role == AV_RULE_MASK && point_in_polygon(point, rule.points)) masked = true;
            if (rule.role == AV_RULE_ROI && point_in_polygon(point, rule.points)) in_roi = true;
            if (rule.role == AV_RULE_LINE && object.track_id >= 0 && rule.points.size() >= 2) {
                const auto previous = previous_points.find(object.track_id);
                if (previous != previous_points.end()) {
                    for (size_t i = 1; i < rule.points.size(); ++i) {
                        if (line_crossed(previous->second, point, rule.points[i - 1], rule.points[i], rule.mode)) {
                            crossed = true;
                            break;
                        }
                    }
                }
            }
        }
        if (object.track_id >= 0) {
            previous_points[object.track_id] = point;
            missed_frames[object.track_id] = 0;
        }
        if (masked || (has_roi && !in_roi) || (has_line && !crossed)) continue;
        filtered.push_back(object);
    }
    return filtered;
}

} // namespace yolo26n
