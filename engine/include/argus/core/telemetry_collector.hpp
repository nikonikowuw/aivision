#pragma once

/**
 * @file telemetry_collector.hpp
 * @brief 系统遥测与性能指标采集封装
 * 
 * 屏蔽平台差异，通过当前激活的 platform_adapter 定期采集 CPU/内存/磁盘/NPU 温度与算力指标。
 */

#include <memory>
#include "argus/platform/platform_api.hpp"

namespace argus::core {

/**
 * @brief 遥测指标收集器
 */
class TelemetryCollector {
public:
    explicit TelemetryCollector(std::shared_ptr<platform::IPlatformAdapter> adapter);

    /**
     * @brief 采集当前的系统指标
     */
    [[nodiscard]] platform::SystemMetrics collect() const;

    /**
     * @brief 检查当前平台是否支持指标遥测
     */
    [[nodiscard]] bool available() const;

private:
    std::shared_ptr<platform::IPlatformAdapter> adapter_;
};

} // namespace argus::core

