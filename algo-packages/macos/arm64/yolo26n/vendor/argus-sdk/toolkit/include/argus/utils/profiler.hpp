#pragma once

#include <chrono>
#include <iostream>
#include <string>
#include <vector>
#include <numeric>
#include <algorithm>

namespace argus::utils {

class ScopedTimer {
public:
    explicit ScopedTimer(double* out_ms) : out_ms_(out_ms), start_(std::chrono::high_resolution_clock::now()) {}
    ~ScopedTimer() {
        if (out_ms_) {
            auto end = std::chrono::high_resolution_clock::now();
            *out_ms_ = std::chrono::duration<double, std::milli>(end - start_).count();
        }
    }
private:
    double* out_ms_;
    std::chrono::time_point<std::chrono::high_resolution_clock> start_;
};

struct BenchmarkStats {
    double avg_ms = 0.0;
    double p50_ms = 0.0;
    double p99_ms = 0.0;
    double fps = 0.0;

    static BenchmarkStats compute(std::vector<double> samples) {
        if (samples.empty()) return {};
        std::sort(samples.begin(), samples.end());
        double sum = std::accumulate(samples.begin(), samples.end(), 0.0);
        BenchmarkStats stats;
        stats.avg_ms = sum / samples.size();
        stats.p50_ms = samples[samples.size() * 50 / 100];
        stats.p99_ms = samples[samples.size() * 99 / 100];
        stats.fps = (stats.avg_ms > 0.0) ? (1000.0 / stats.avg_ms) : 0.0;
        return stats;
    }
};

} // namespace argus::utils
