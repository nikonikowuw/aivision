#pragma once

/**
 * @file motion_gate.hpp
 * @brief Frigate 风格低成本运动检测门控
 *
 * 职责：
 * 1. 从解码后的 av_frame_desc 提取亮度（Y 分量/单通道）数据；
 * 2. 降采样至固定小尺寸灰度图，应用 Mask 排除多边形；
 * 3. 运行滑动平均背景建模与帧差分判定；
 * 4. 结合保活间隔（keepalive_interval）输出放行、保活或跳过决策。
 */

#include <chrono>
#include <cstdint>
#include <vector>
#include <string>
#include "argus/types.h"

namespace argus::core {

/**
 * @brief 运动门控评估决策
 */
enum class MotionDecision {
    MOTION,       ///< 检测到有效画面变化，放行推理
    KEEPALIVE,    ///< 画面静止但已达到保活间隔，放行保活推理
    SKIP,         ///< 画面静止且未达保活时间，跳过推理
    PASSTHROUGH   ///< 门控未启用或不支持的格式，降级全量放行
};

/**
 * @brief 运动门控配置
 */
struct MotionGateConfig {
    bool enabled = false;                                      ///< 是否启用运动门控
    uint32_t frame_height = 100;                              ///< 降采样基准高度（像素，默认 100）
    uint32_t threshold = 25;                                  ///< 灰度差分阈值 (1-255，默认 25)
    uint32_t contour_area = 50;                               ///< 最小变化像素数量（默认 50）
    float frame_alpha = 0.05f;                                ///< 背景学习平滑系数 (0.01-1.0，默认 0.05)
    std::chrono::milliseconds keepalive_interval{2000};        ///< 静止保活推理间隔（默认 2000ms）
    std::vector<std::vector<av_point>> masks;                 ///< 归一化排除 Mask 多边形列表
};

/**
 * @brief 运动门控遥测统计
 */
struct MotionGateStats {
    uint64_t total_frames = 0;       ///< 评估总帧数
    uint64_t motion_passed = 0;      ///< 因运动放行的帧数
    uint64_t keepalive_passed = 0;   ///< 因保活放行的帧数
    uint64_t skipped_frames = 0;     ///< 跳过推理的帧数
    uint64_t passthrough_frames = 0; ///< 降级/直接放行的帧数
    uint32_t last_changed_pixels = 0;///< 最近单帧 mask 后的变化像素数
    uint32_t max_changed_pixels = 0; ///< 采样窗口内最大变化像素数
};

/**
 * @brief 运动检测门控实现类
 */
class MotionGate {
public:
    explicit MotionGate(MotionGateConfig config = {});
    ~MotionGate() = default;

    /**
     * @brief 更新门控配置
     * @note 当尺寸、Mask 或启用状态改变时会自动重置背景模型
     */
    void update_config(const MotionGateConfig& config);

    /**
     * @brief 从 JSON 字符串解析并更新门控配置
     */
    void update_config_from_json(const std::string& params_json);

    /**
     * @brief 同步规则列表中的 AV_RULE_MASK 排除多边形
     * @param rules 规则数组
     * @param count 规则数量
     */
    void sync_rule_masks(const av_rule* rules, size_t count);

    /**
     * @brief 评估输入帧
     * @param frame 视频帧描述符
     * @param now 单调时钟时间点（支持测试注入 fake clock）
     * @return 运动决策
     */
    MotionDecision evaluate(
        const av_frame_desc& frame,
        std::chrono::steady_clock::time_point now = std::chrono::steady_clock::now()
    );

    /**
     * @brief 重置背景模型与保活计时状态
     */
    void reset();

    [[nodiscard]] const MotionGateConfig& get_config() const { return config_; }
    [[nodiscard]] MotionGateStats get_stats() const { return stats_; }

private:
    void rebuild_mask_map();
    bool extract_lowres_luma(const av_frame_desc& frame, std::vector<uint8_t>& out_luma,
                             uint32_t out_w, uint32_t out_h);

    MotionGateConfig config_;
    bool bg_initialized_ = false;
    uint32_t motion_width_ = 0;
    uint32_t motion_height_ = 0;
    uint32_t last_src_width_ = 0;
    uint32_t last_src_height_ = 0;
    uint32_t last_pixel_format_ = 0;

    std::vector<float> bg_model_;        ///< 浮点背景模型，用于滑动平均更新
    std::vector<uint8_t> curr_luma_;     ///< 当前采样灰度图
    std::vector<uint8_t> mask_map_;      ///< 二值排除掩码：255=排除，0=参与检测

    std::chrono::steady_clock::time_point last_infer_time_{};
    MotionGateStats stats_;
};

} // namespace argus::core
