#pragma once

#include <string>
#include <functional>
#include <memory>
#include <vector>
#include <cstdint>
#include <atomic>
#include "aivision/types.h"

namespace aivision::media {

struct EncodedPacket {
    const uint8_t* data = nullptr;
    size_t size = 0;
    int64_t pts_us = 0;
    int64_t dts_us = 0;
    bool is_keyframe = false;
    std::string codec_name; // "H264" or "H265"
    std::shared_ptr<const std::vector<uint8_t>> storage;

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

using PacketCallback = std::function<void(const EncodedPacket& packet)>;
using StatusCallback = std::function<void(const std::string& status_msg, bool is_error)>;

class IMediaSource {
public:
    virtual ~IMediaSource() = default;
    virtual av_status start(const std::string& rtsp_url, PacketCallback on_packet, StatusCallback on_status) = 0;
    virtual void stop() = 0;
    virtual bool is_connected() const = 0;
};
class IMediaBackend {
public:
    virtual ~IMediaBackend() = default;
    virtual std::unique_ptr<IMediaSource> create_source(const std::string& source_id) = 0;
    [[nodiscard]] virtual const char* name() const { return "unknown"; }
};

std::shared_ptr<IMediaBackend> create_zlm_backend();
std::shared_ptr<IMediaBackend> create_mock_backend();

} // namespace aivision::media
