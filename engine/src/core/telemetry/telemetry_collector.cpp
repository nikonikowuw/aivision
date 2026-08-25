/**
 * @file telemetry_collector.cpp
 * @brief 遥测指标收集器实现
 */

#include "aivision/core/telemetry_collector.hpp"

#include <utility>

namespace aivision::core {

TelemetryCollector::TelemetryCollector(std::shared_ptr<platform::IPlatformAdapter> adapter)
    : adapter_(std::move(adapter)) {}

platform::SystemMetrics TelemetryCollector::collect() const {
    if (!adapter_ || !adapter_->get_telemetry()) return {};
    return adapter_->get_telemetry()->collect_metrics();
}

bool TelemetryCollector::available() const {
    return adapter_ && adapter_->get_telemetry() != nullptr;
}

} // namespace aivision::core

