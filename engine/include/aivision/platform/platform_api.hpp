#pragma once

/**
 * @file platform_api.hpp
 * @brief 平台硬件与系统抽象层接口定义
 * 
 * 包含平台能力档案、硬件解码器（IDecoder）、图像原语处理器（IImageProcessor）、
 * 系统遥测采集器（ITelemetry）以及平台适配器与注册表（IPlatformAdapter / PlatformRegistry）。
 * 遵循跨平台解耦设计，隔离具体硬件（macOS VideoToolbox、RKNN/RGA、Atlas DVPP 等）实现细节。
 */

#include <string>
#include <vector>
#include <memory>
#include <unordered_map>
#include <cstdint>
#include <limits>
#include "aivision/types.h"
#include "aivision/result.h"

namespace aivision::platform {

/**
 * @brief 平台功能/能力可用性状态枚举
 */
enum class CapabilityStatus {
    UNSPECIFIED = 0, ///< 未指定
    AVAILABLE = 1,   ///< 完全可用
    DEGRADED = 2,    ///< 降级可用（如软件模拟、降频）
    UNSUPPORTED = 3, ///< 不支持
    UNAVAILABLE = 3, ///< 不可用（兼容别名）
};

/**
 * @brief 单项能力评估详情
 */
struct CapabilityItem {
    CapabilityStatus status = CapabilityStatus::UNAVAILABLE; ///< 状态
    std::string reason;                                      ///< 降级或不可用原因说明
};

/**
 * @brief 平台能力与资源档案
 */
struct PlatformProfile {
    std::string platform_id;                 ///< 平台唯一标识，如 "macos-arm64", "rk3576-linux"
    uint32_t platform_tag = 0;               ///< 平台二进制标签（与 C ABI av_platform_tag 对齐）
    std::string profile_version = "1.0.0";   ///< Profile 协议版本

    uint32_t total_compute_units = 1000;     ///< 总算力单元（默认1000点）
    uint32_t reserved_compute_units = 100;   ///< 保留算力单元（供系统/视频编解码等开销）
    uint64_t min_free_memory_bytes = 256 * 1024 * 1024; ///< 允许新实例启动的最小空闲内存阈值（默认 256MB）

    CapabilityItem hardware_decode;          ///< 硬件解码支持度
    CapabilityItem vector_image_ops;         ///< 图像原语加速支持度（NEON/RGA/vImage等）
    CapabilityItem telemetry_metrics;        ///< 硬件遥测支持度（NPU/温度/CPU/内存）
};

/**
 * @brief 视频硬件/软件解码器抽象接口
 */
class IDecoder {
public:
    virtual ~IDecoder() = default;

    /**
     * @brief 送入待解码的编码数据包
     * @param data 字节流首地址
     * @param size 字节流长度
     * @param pts_us 显示时间戳（微秒）
     * @param is_keyframe 是否为关键帧
     * @return av_status 操作结果状态
     */
    virtual av_status send_packet(const uint8_t* data, size_t size, int64_t pts_us, bool is_keyframe) = 0;

    /**
     * @brief 提取已解码的帧描述符
     * @param out_frame 输出填充的 av_frame_desc
     * @return av_status 成功返回 AV_OK，若无输出则返回 AV_ERR_RESOURCE_BUSY / AV_ERR_INVALID_STATE
     */
    virtual av_status receive_frame(av_frame_desc* out_frame) = 0;

    /**
     * @brief 刷新解码器管线内部缓存帧
     */
    virtual void flush() = 0;

    /**
     * @brief 重置解码器上下文与会话状态
     */
    virtual void reset() = 0;
};

/**
 * @brief 图像几何变换与编解码处理接口
 */
class IImageProcessor {
public:
    virtual ~IImageProcessor() = default;

    /**
     * @brief 图像缩放
     */
    virtual av_status resize(const av_frame_desc* src, av_image_view* dst) = 0;

    /**
     * @brief 保持宽高比填充缩放（Letterbox）
     */
    virtual av_status letterbox(const av_frame_desc* src, av_image_view* dst, float* pad_w, float* pad_h, float* scale) = 0;

    /**
     * @brief 颜色空间转换（如 NV12 转 RGB/BGR/Planar）
     */
    virtual av_status convert_color(const av_image_view* src, av_image_view* dst) = 0;

    /**
     * @brief 抓拍帧或 ROI 区域编码为 JPEG 二进制字节流
     */
    virtual av_status encode_jpeg(const av_frame_desc* src, const av_rect* crop_roi, int quality, std::vector<uint8_t>& out_jpeg) = 0;

    /**
     * @brief 抓拍帧编码为指定目标最大宽度的缩略图 JPEG 二进制字节流
     */
    virtual av_status encode_thumbnail_jpeg(const av_frame_desc* src, int /*max_width*/, int quality, std::vector<uint8_t>& out_jpeg) {
        // 默认回退实现：直接使用原图编码
        return encode_jpeg(src, nullptr, quality, out_jpeg);
    }
};

/**
 * @brief 系统与硬件性能指标
 */
struct SystemMetrics {
    int64_t uptime_seconds = 0;                                           ///< 系统运行时间（秒）
    float cpu_usage_percent = 0.0f;                                       ///< CPU 使用率（0.0 ~ 100.0%）
    float memory_usage_percent = 0.0f;                                    ///< 内存使用率（0.0 ~ 100.0%）
    float disk_usage_percent = 0.0f;                                      ///< 存储/磁盘使用率（0.0 ~ 100.0%）
    float accelerator_usage_percent = std::numeric_limits<float>::quiet_NaN(); ///< NPU/GPU 加速器使用率（NaN 表示不支持）
    bool accelerator_supported = false;                                   ///< 是否支持加速器监控
    float temperature_celsius = std::numeric_limits<float>::quiet_NaN();  ///< 芯片温度（摄氏度）
    bool temperature_supported = false;                                   ///< 是否支持温度读取
};

/**
 * @brief 系统与硬件遥测监控接口
 */
class ITelemetry {
public:
    virtual ~ITelemetry() = default;

    /**
     * @brief 采集当前的系统遥测指标
     */
    virtual SystemMetrics collect_metrics() = 0;
};

/// 平台私有底层句柄（opaque）的析构释放函数签名
using OpaqueReleaseFn = void (*)(void* opaque);

/**
 * @brief 平台统一适配器接口，聚合各底层子系统
 */
class IPlatformAdapter {
public:
    virtual ~IPlatformAdapter() = default;

    /**
     * @brief 获取平台能力档案配置
     */
    [[nodiscard]] virtual const PlatformProfile& get_profile() const = 0;

    /**
     * @brief 创建与指定编码类型匹配的硬件解码器
     */
    [[nodiscard]] virtual std::unique_ptr<IDecoder> create_decoder(const std::string& codec_type) = 0;

    /**
     * @brief 获取图像原语处理器单例
     */
    [[nodiscard]] virtual IImageProcessor* get_image_processor() = 0;

    /**
     * @brief 获取遥测指标采集器单例
     */
    [[nodiscard]] virtual ITelemetry* get_telemetry() = 0;

    /**
     * @brief 获取暴露给 C ABI 的纯 C av_image_ops 函数表
     */
    [[nodiscard]] virtual const av_image_ops* get_c_image_ops() = 0;

    /**
     * @brief 获取释放底层 opaque 句柄（如 CVPixelBufferRef）的释放回调
     */
    [[nodiscard]] virtual OpaqueReleaseFn get_opaque_release() const = 0;
};

/**
 * @brief 平台适配器全局注册表（单例）
 */
class PlatformRegistry {
public:
    static PlatformRegistry& instance();

    /**
     * @brief 注册特定平台的适配器
     */
    void register_adapter(const std::string& platform_id, std::shared_ptr<IPlatformAdapter> adapter);

    /**
     * @brief 根据平台标识获取对应适配器
     */
    std::shared_ptr<IPlatformAdapter> get_adapter(const std::string& platform_id);

    /**
     * @brief 设置当前引擎激活使用的平台适配器
     */
    void set_active_platform(const std::string& platform_id);

    /**
     * @brief 获取当前激活的平台适配器
     */
    std::shared_ptr<IPlatformAdapter> get_active_adapter();

private:
    PlatformRegistry() = default;
    std::unordered_map<std::string, std::shared_ptr<IPlatformAdapter>> adapters_;
    std::shared_ptr<IPlatformAdapter> active_adapter_;
    std::string active_platform_id_;
};

} // namespace aivision::platform

