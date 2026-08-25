/**
 * @file macos_telemetry.cpp
 * @brief macOS 系统性能与硬件指标监控实现
 * 
 * 基于 Mach 内核接口（host_processor_info / host_statistics64）与 sysctl / statfs
 * 采集 CPU 使用率、内存占用、磁盘空间及系统运行时长。
 */

#include "aivision/platform/macos_platform.hpp"

#include <mach/mach.h>
#include <mach/mach_host.h>
#include <mach/mach_init.h>
#include <mach/vm_statistics.h>
#include <algorithm>
#include <sys/mount.h>
#include <sys/sysctl.h>
#include <chrono>
#include <mutex>


namespace aivision::platform {
namespace {

uint64_t sysctl_u64(const char* name) {
    uint64_t value = 0;
    size_t size = sizeof(value);
    return sysctlbyname(name, &value, &size, nullptr, 0) == 0 ? value : 0;
}

class MacosTelemetry final : public ITelemetry {
public:
    // 采集 macOS 系统性能指标（Uptime, Memory, Disk, CPU Load）
    SystemMetrics collect_metrics() override {
        SystemMetrics metrics;
        const auto now = std::chrono::system_clock::now();

        // 1. 获取系统启动时间计算 Uptime
        timeval boot_time{};
        size_t boot_size = sizeof(boot_time);
        if (sysctlbyname("kern.boottime", &boot_time, &boot_size, nullptr, 0) == 0) {
            const auto boot = std::chrono::system_clock::time_point(std::chrono::seconds(boot_time.tv_sec));
            metrics.uptime_seconds = std::chrono::duration_cast<std::chrono::seconds>(now - boot).count();
        }

        // 2. 通过 Mach VM Statistics 统计物理内存使用率
        vm_statistics64_data_t vm_statistics{};
        mach_msg_type_number_t vm_count = HOST_VM_INFO64_COUNT;
        if (host_statistics64(mach_host_self(), HOST_VM_INFO64,
                              reinterpret_cast<host_info64_t>(&vm_statistics), &vm_count) == KERN_SUCCESS) {
            const uint64_t total_memory = sysctl_u64("hw.memsize");
            const uint64_t available_memory = static_cast<uint64_t>(
                vm_statistics.free_count + vm_statistics.inactive_count + vm_statistics.speculative_count) * vm_page_size;
            if (total_memory > 0) {
                const uint64_t used_memory = total_memory > available_memory ? total_memory - available_memory : 0;
                metrics.memory_usage_percent = static_cast<float>(used_memory) * 100.0f / total_memory;
            }
        }

        // 3. 通过 statfs 查询根分区磁盘使用率
        struct statfs disk{};
        if (::statfs("/", &disk) == 0 && disk.f_blocks > 0) {
            const uint64_t total_blocks = static_cast<uint64_t>(disk.f_blocks);
            const uint64_t free_blocks = static_cast<uint64_t>(disk.f_bavail);
            metrics.disk_usage_percent = static_cast<float>(total_blocks - std::min(total_blocks, free_blocks)) * 100.0f /
                                         static_cast<float>(total_blocks);
        }

        // 4. 计算前后两次采集的 CPU 滴答差值得出总体 CPU 占用率
        update_cpu_usage(metrics);
        metrics.accelerator_supported = false;
        metrics.temperature_supported = false;
        return metrics;
    }

private:
    void update_cpu_usage(SystemMetrics& metrics) {
        natural_t processor_count = 0;
        processor_info_array_t load_info = nullptr;
        mach_msg_type_number_t info_count = 0;
        if (host_processor_info(mach_host_self(), PROCESSOR_CPU_LOAD_INFO, &processor_count,
                                &load_info, &info_count) != KERN_SUCCESS || !load_info) return;

        uint64_t user = 0;
        uint64_t system = 0;
        uint64_t idle = 0;
        uint64_t nice = 0;
        auto* cpu_load = reinterpret_cast<processor_cpu_load_info_t>(load_info);
        for (natural_t cpu = 0; cpu < processor_count; ++cpu) {
            user += cpu_load[cpu].cpu_ticks[CPU_STATE_USER];
            system += cpu_load[cpu].cpu_ticks[CPU_STATE_SYSTEM];
            idle += cpu_load[cpu].cpu_ticks[CPU_STATE_IDLE];
            nice += cpu_load[cpu].cpu_ticks[CPU_STATE_NICE];
        }
        vm_deallocate(mach_task_self(), reinterpret_cast<vm_address_t>(load_info),
                      static_cast<vm_size_t>(info_count) * sizeof(integer_t));

        std::lock_guard<std::mutex> lock(cpu_mutex_);
        const uint64_t total = user + system + idle + nice;
        if (has_previous_ && total > previous_total_) {
            const uint64_t idle_delta = idle >= previous_idle_ ? idle - previous_idle_ : 0;
            const uint64_t total_delta = total - previous_total_;
            metrics.cpu_usage_percent = static_cast<float>(total_delta - std::min(total_delta, idle_delta)) * 100.0f /
                                         static_cast<float>(total_delta);
        }
        previous_total_ = total;
        previous_idle_ = idle;
        has_previous_ = true;
    }

    std::mutex cpu_mutex_;
    uint64_t previous_total_ = 0;
    uint64_t previous_idle_ = 0;
    bool has_previous_ = false;
};

MacosTelemetry g_macos_telemetry;

} // namespace

ITelemetry* MacosPlatformAdapter::get_telemetry() {
    return &g_macos_telemetry;
}

} // namespace aivision::platform
