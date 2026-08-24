#pragma once

#include <string>
#include <atomic>
#include <chrono>
#include <sstream>
#include <regex>

namespace aivision::utils {

class EventIdGenerator {
public:
    static std::string next_event_id(uint32_t instance_seq = 1) {
        static std::atomic<uint64_t> counter{1};
        auto now = std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::system_clock::now().time_since_epoch()
        ).count();
        uint64_t seq = counter.fetch_add(1);

        std::ostringstream oss;
        oss << "evt-" << now << "-" << instance_seq << "-" << seq;
        return oss.str();
    }

    static bool is_valid(const std::string& event_id) {
        if (event_id.empty() || event_id.length() > 128) return false;
        // Strictly exclude '/' to prevent ambiguous global event_id splitting
        static const std::regex kPattern("^[A-Za-z0-9._-]+$");
        return std::regex_match(event_id, kPattern);
    }
};

} // namespace aivision::utils
