#pragma once

#include <memory>
#include "aivision/platform/platform_api.hpp"

namespace aivision::core {

class TelemetryCollector {
public:
    explicit TelemetryCollector(std::shared_ptr<platform::IPlatformAdapter> adapter);

    [[nodiscard]] platform::SystemMetrics collect() const;
    [[nodiscard]] bool available() const;

private:
    std::shared_ptr<platform::IPlatformAdapter> adapter_;
};

} // namespace aivision::core
