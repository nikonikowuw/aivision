#pragma once

/**
 * @file media_api.hpp
 * @brief 媒体接入与解封装抽象接口定义
 * 
 * 规定了 EncodedPacket 结构以及底层媒体拉流源（IMediaSource）和媒体后端（IMediaBackend）的抽象。
 * 遵循依赖倒置原则，核心模块仅依赖本接口，不直接耦合具体的流媒体库（如 ZLMediaKit）。
 */

#include <string>
#include <functional>
#include <memory>
#include <vector>
#include <cstdint>
#include <atomic>
#include <chrono>
#include "argus/types.h"

namespace argus::media {

/**
 * @brief 编码数据包（H.264/H.265 NALU / 帧包）
 */
struct EncodedPacket {
    const uint8_t* data = nullptr;                        ///< 指向编码字节流起始地址
    size_t size = 0;                                     ///< 字节流长度
    int64_t pts_us = 0;                                  ///< 显示时间戳（微秒）
    int64_t dts_us = 0;                                  ///< 解码时间戳（微秒）
    bool is_keyframe = false;                            ///< 是否为关键帧（IDR / IRAP）
    std::string codec_name;                              ///< 编码类型："H264" 或 "H265"
    std::shared_ptr<const std::vector<uint8_t>> storage; ///< 内存所有权载体（当数据包转为自主持有时非空）

    /**
     * @brief 深度拷贝并转为自主管理内存的 EncodedPacket 副本
     */
    [[nodiscard]] EncodedPacket clone_owned() const {
        EncodedPacket copy = *this;
        if (data && size > 0) {
            auto bytes = std::make_shared<std::vector<uint8_t>>(data, data + size);
            copy.storage = std::move(bytes);
            copy.data = copy.storage->data();
            copy.size = copy.storage->size();
        }
        return copy;
    }
};

/// 编码包到达回调函数类型
using PacketCallback = std::function<void(const EncodedPacket& packet)>;
/// 媒体源连接状态变化回调函数类型 (status_msg: 状态信息, is_error: 是否错误)
using StatusCallback = std::function<void(const std::string& status_msg, bool is_error)>;

/// 测活传输方式（RTSP 传输策略；正式运行固定 TCP 优先）
enum class Transport : uint8_t {
    TCP = 0, ///< RTP over TCP
    UDP = 1, ///< RTP over UDP
};

/// 单次测活尝试结果（由媒体后端填充稳定失败码与可获得时的媒体元数据）
struct ProbeOutcome {
    bool success = false;         ///< 是否收到首个有效编码视频帧
    std::string failure_code;     ///< 失败时的稳定机器码（如 RTSP_CONNECT_FAILED）；成功时为空
    std::string codec;            ///< 编码格式（如 "H264" / "H265"，可获得时）
    uint32_t width = 0;           ///< 视频宽度（可获得时）
    uint32_t height = 0;          ///< 视频高度（可获得时）
    double fps = 0.0;             ///< 视频帧率（可获得时）
};

/**
 * @brief 媒体拉流源抽象接口
 */
class IMediaSource {
public:
    virtual ~IMediaSource() = default;

    /**
     * @brief 启动 RTSP 拉流
     * @param rtsp_url RTSP 流地址
     * @param on_packet 数据包回调
     * @param on_status 状态变更回调
     * @return av_status 启动状态码
     */
    virtual av_status start(const std::string& rtsp_url, PacketCallback on_packet, StatusCallback on_status) = 0;

    /**
     * @brief 停止拉流并释放底层网络和解封装资源
     */
    virtual void stop() = 0;

    /**
     * @brief 检查当前是否处于连接状态
     */
    virtual bool is_connected() const = 0;

    /**
     * @brief 一次性同步测活：按指定传输方式拉流并等待首个有效编码视频帧
     * @param url 完整 RTSP URL（可含百分号编码 userinfo）
     * @param transport 本次尝试的传输方式（TCP / UDP）
     * @param timeout 等待首个视频帧/失败的有界超时
     * @return 测活结果（成功时含编码格式与媒体元数据；失败时含稳定失败码）
     * @note 无论成功失败，实现必须在本方法返回前释放底层临时媒体源，
     *       不得在 EventPoller 线程上执行阻塞等待
     */
    virtual ProbeOutcome probe(const std::string& url, Transport transport,
                               std::chrono::milliseconds timeout) = 0;
};

/**
 * @brief 媒体后端工厂接口
 */
class IMediaBackend {
public:
    virtual ~IMediaBackend() = default;

    /**
     * @brief 为指定源标识创建独立的拉流源实例
     * @param source_id 摄像头或数据源唯一标识
     */
    virtual std::unique_ptr<IMediaSource> create_source(const std::string& source_id) = 0;

    /**
     * @brief 获取当前媒体后端名称
     */
    [[nodiscard]] virtual const char* name() const { return "unknown"; }
};

/**
 * @brief 创建基于 ZLMediaKit 的媒体后端实现
 */
std::shared_ptr<IMediaBackend> create_zlm_backend();

/**
 * @brief 创建用于单元测试的 Mock 媒体后端实现
 */
std::shared_ptr<IMediaBackend> create_mock_backend();

} // namespace argus::media

