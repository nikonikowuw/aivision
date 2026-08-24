#include "aivision/platform/platform_api.hpp"

namespace aivision::platform {

PlatformRegistry& PlatformRegistry::instance() {
    static PlatformRegistry registry;
    return registry;
}

void PlatformRegistry::register_adapter(const std::string& platform_id,
                                        std::shared_ptr<IPlatformAdapter> adapter) {
    adapters_[platform_id] = std::move(adapter);
}

std::shared_ptr<IPlatformAdapter> PlatformRegistry::get_adapter(const std::string& platform_id) {
    const auto it = adapters_.find(platform_id);
    return it == adapters_.end() ? nullptr : it->second;
}

void PlatformRegistry::set_active_platform(const std::string& platform_id) {
    active_platform_id_ = platform_id;
    active_adapter_ = get_adapter(platform_id);
}

std::shared_ptr<IPlatformAdapter> PlatformRegistry::get_active_adapter() {
    return active_adapter_;
}

} // namespace aivision::platform
