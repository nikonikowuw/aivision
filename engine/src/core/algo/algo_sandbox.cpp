#include "aivision/core/algo_sandbox.hpp"
#include "aivision/algo.h"

#include <algorithm>
#include <array>
#include <cctype>
#include <chrono>
#include <cerrno>
#include <dlfcn.h>
#include <fcntl.h>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <initializer_list>
#include <iostream>
#include <nlohmann/json.hpp>
#include <regex>
#include <set>
#include <poll.h>
#include <spawn.h>
#include <sstream>
#include <string>
#include <string_view>
#include <thread>
#include <unistd.h>
#include <utility>
#include <vector>
#include <sys/stat.h>
#include <sys/wait.h>
#include <signal.h>

extern char** environ;

namespace fs = std::filesystem;

namespace aivision::core {
namespace {

struct StagingDirectory {
    fs::path path;
    ~StagingDirectory() {
        if (!path.empty()) {
            std::error_code error;
            fs::remove_all(path, error);
        }
    }
};

struct SelfTestCapture {
    uint32_t callback_count = 0;
    bool valid = false;
    std::string json;
};

bool spawn_process(const std::vector<std::string>& args,
                   posix_spawn_file_actions_t* actions,
                   pid_t* out_pid) {
    std::vector<char*> argv;
    argv.reserve(args.size() + 1);
    for (const std::string& arg : args) argv.push_back(const_cast<char*>(arg.c_str()));
    argv.push_back(nullptr);

    posix_spawnattr_t attributes;
    if (posix_spawnattr_init(&attributes) != 0) return false;
    const short flags = POSIX_SPAWN_SETPGROUP;
    const bool configured = posix_spawnattr_setflags(&attributes, flags) == 0 &&
                            posix_spawnattr_setpgroup(&attributes, 0) == 0;
    pid_t pid = 0;
    const int status = configured
        ? posix_spawnp(&pid, args[0].c_str(), actions, &attributes, argv.data(), environ)
        : EINVAL;
    posix_spawnattr_destroy(&attributes);
    if (status != 0) return false;
    *out_pid = pid;
    return true;
}

bool wait_with_deadline(pid_t pid, int* out_status, std::chrono::steady_clock::time_point deadline) {
    for (;;) {
        const pid_t result = ::waitpid(pid, out_status, WNOHANG);
        if (result == pid) return true;
        if (result < 0) return false;
        if (std::chrono::steady_clock::now() >= deadline) {
            ::kill(-pid, SIGKILL);
            ::kill(pid, SIGKILL);
            return ::waitpid(pid, out_status, 0) == pid;
        }
        std::this_thread::sleep_for(std::chrono::milliseconds(20));
    }
}

bool run_command(const std::vector<std::string>& args, const fs::path* stdout_path = nullptr) {
    if (args.empty()) return false;
    posix_spawn_file_actions_t actions;
    posix_spawn_file_actions_t* actions_ptr = nullptr;
    if (stdout_path) {
        if (posix_spawn_file_actions_init(&actions) != 0) return false;
        if (posix_spawn_file_actions_addopen(&actions, STDOUT_FILENO, stdout_path->c_str(),
                                             O_WRONLY | O_CREAT | O_TRUNC, 0600) != 0) {
            posix_spawn_file_actions_destroy(&actions);
            return false;
        }
        actions_ptr = &actions;
    }

    pid_t pid = 0;
    const bool spawned = spawn_process(args, actions_ptr, &pid);
    if (actions_ptr) posix_spawn_file_actions_destroy(&actions);
    if (!spawned) return false;

    int wait_status = 0;
    if (!wait_with_deadline(pid, &wait_status, std::chrono::steady_clock::now() + std::chrono::seconds(30))) {
        return false;
    }
    return WIFEXITED(wait_status) && WEXITSTATUS(wait_status) == 0;
}

bool capture_command(const std::vector<std::string>& args, std::string& output,
                     size_t max_output = 1024 * 1024, bool merge_stderr = false) {
    if (args.empty()) return false;
    int pipe_fds[2] = {-1, -1};
    if (::pipe(pipe_fds) != 0) return false;
    const int flags = ::fcntl(pipe_fds[0], F_GETFL, 0);
    if (flags < 0 || ::fcntl(pipe_fds[0], F_SETFL, flags | O_NONBLOCK) != 0) {
        ::close(pipe_fds[0]);
        ::close(pipe_fds[1]);
        return false;
    }

    posix_spawn_file_actions_t actions;
    if (posix_spawn_file_actions_init(&actions) != 0) {
        ::close(pipe_fds[0]);
        ::close(pipe_fds[1]);
        return false;
    }
    if (posix_spawn_file_actions_adddup2(&actions, pipe_fds[1], STDOUT_FILENO) != 0 ||
        (merge_stderr && posix_spawn_file_actions_adddup2(&actions, pipe_fds[1], STDERR_FILENO) != 0) ||
        posix_spawn_file_actions_addclose(&actions, pipe_fds[0]) != 0) {
        posix_spawn_file_actions_destroy(&actions);
        ::close(pipe_fds[0]);
        ::close(pipe_fds[1]);
        return false;
    }
    pid_t pid = 0;
    const bool spawned = spawn_process(args, &actions, &pid);
    posix_spawn_file_actions_destroy(&actions);
    ::close(pipe_fds[1]);
    if (!spawned) {
        ::close(pipe_fds[0]);
        return false;
    }

    output.clear();
    std::array<char, 4096> buffer{};
    bool pipe_open = true;
    bool valid = true;
    const auto deadline = std::chrono::steady_clock::now() + std::chrono::seconds(30);
    while (pipe_open) {
        struct pollfd descriptor{pipe_fds[0], POLLIN | POLLHUP, 0};
        const int poll_status = ::poll(&descriptor, 1, 100);
        if (poll_status < 0 && errno != EINTR) {
            valid = false;
            break;
        }
        if (poll_status > 0 && (descriptor.revents & (POLLIN | POLLHUP))) {
            for (;;) {
                const ssize_t count = ::read(pipe_fds[0], buffer.data(), buffer.size());
                if (count == 0) {
                    pipe_open = false;
                    break;
                }
                if (count < 0) {
                    if (errno == EAGAIN || errno == EINTR) break;
                    valid = false;
                    pipe_open = false;
                    break;
                }
                if (output.size() + static_cast<size_t>(count) > max_output) {
                    valid = false;
                    pipe_open = false;
                    ::kill(-pid, SIGKILL);
                    ::kill(pid, SIGKILL);
                    break;
                }
                output.append(buffer.data(), static_cast<size_t>(count));
            }
        }
        if (std::chrono::steady_clock::now() >= deadline) {
            valid = false;
            ::kill(-pid, SIGKILL);
            ::kill(pid, SIGKILL);
            break;
        }
    }
    ::close(pipe_fds[0]);
    int wait_status = 0;
    if (!wait_with_deadline(pid, &wait_status, deadline)) return false;
    return valid && WIFEXITED(wait_status) && WEXITSTATUS(wait_status) == 0;
}


bool validate_exported_symbols(const fs::path& library_path, std::string& error) {
    std::string output;
#if defined(__APPLE__)
    const std::vector<std::string> command = {"nm", "-gU", library_path.string()};
#else
    const std::vector<std::string> command = {"nm", "-g", "--defined-only", library_path.string()};
#endif
    if (!capture_command(command, output)) {
        error = "cannot inspect dynamic library exports";
        return false;
    }
    std::istringstream lines(output);
    std::string line;
    size_t symbol_count = 0;
    while (std::getline(lines, line)) {
        std::istringstream fields(line);
        std::string address;
        std::string type;
        std::string symbol;
        if (!(fields >> address >> type >> symbol)) continue;
        if (!symbol.empty() && symbol.front() == '_') symbol.erase(symbol.begin());
        if (symbol != AV_ALGO_GET_ABI_SYMBOL) {
            error = "dynamic library exports an unexpected symbol: " + symbol;
            return false;
        }
        ++symbol_count;
    }
    if (symbol_count != 1) {
        error = "dynamic library must export exactly av_algo_get_abi";
        return false;
    }
    return true;
}

bool safe_relative_path(const std::string& value) {
    if (value.empty() || value.find('\\') != std::string::npos || value.find('\0') != std::string::npos) return false;
    const fs::path path(value);
    if (path.is_absolute()) return false;
    for (const auto& part : path) {
        if (part == "." || part == ".." || part.empty()) return false;
    }
    return true;
}

bool safe_identifier(const std::string& value) {
    if (value.empty() || value.size() > 128) return false;
    for (const unsigned char ch : value) {
        if (!(std::isalnum(ch) || ch == '-' || ch == '_' || ch == '.')) return false;
    }
    return true;
}

std::string lowercase(std::string value) {
    std::transform(value.begin(), value.end(), value.begin(), [](unsigned char c) {
        return static_cast<char>(std::tolower(c));
    });
    return value;
}

class Sha256 {
public:
    Sha256() : state_{0x6a09e667u, 0xbb67ae85u, 0x3c6ef372u, 0xa54ff53au,
                       0x510e527fu, 0x9b05688cu, 0x1f83d9abu, 0x5be0cd19u} {}

    void update(const uint8_t* data, size_t size) {
        total_bytes_ += size;
        while (size > 0) {
            const size_t copy_size = std::min(size, buffer_.size() - buffer_size_);
            std::copy(data, data + copy_size, buffer_.begin() + static_cast<std::ptrdiff_t>(buffer_size_));
            buffer_size_ += copy_size;
            data += copy_size;
            size -= copy_size;
            if (buffer_size_ == buffer_.size()) {
                transform(buffer_.data());
                buffer_size_ = 0;
            }
        }
    }

    std::string hex_digest() const {
        Sha256 copy = *this;
        copy.finish();
        std::ostringstream output;
        output << std::hex << std::setfill('0');
        for (const uint32_t word : copy.state_) output << std::setw(8) << word;
        return output.str();
    }

private:
    static uint32_t rotate_right(uint32_t value, uint32_t count) {
        return (value >> count) | (value << (32 - count));
    }

    void finish() {
        const uint64_t bit_length = total_bytes_ * 8;
        buffer_[buffer_size_++] = 0x80;
        if (buffer_size_ > 56) {
            while (buffer_size_ < buffer_.size()) buffer_[buffer_size_++] = 0;
            transform(buffer_.data());
            buffer_size_ = 0;
        }
        while (buffer_size_ < 56) buffer_[buffer_size_++] = 0;
        for (int shift = 56; shift >= 0; shift -= 8) buffer_[buffer_size_++] = static_cast<uint8_t>(bit_length >> shift);
        transform(buffer_.data());
        buffer_size_ = 0;
    }

    void transform(const uint8_t* block) {
        static constexpr uint32_t k[64] = {
            0x428a2f98u, 0x71374491u, 0xb5c0fbcfu, 0xe9b5dba5u, 0x3956c25bu, 0x59f111f1u, 0x923f82a4u, 0xab1c5ed5u,
            0xd807aa98u, 0x12835b01u, 0x243185beu, 0x550c7dc3u, 0x72be5d74u, 0x80deb1feu, 0x9bdc06a7u, 0xc19bf174u,
            0xe49b69c1u, 0xefbe4786u, 0x0fc19dc6u, 0x240ca1ccu, 0x2de92c6fu, 0x4a7484aau, 0x5cb0a9dcu, 0x76f988dau,
            0x983e5152u, 0xa831c66du, 0xb00327c8u, 0xbf597fc7u, 0xc6e00bf3u, 0xd5a79147u, 0x06ca6351u, 0x14292967u,
            0x27b70a85u, 0x2e1b2138u, 0x4d2c6dfcu, 0x53380d13u, 0x650a7354u, 0x766a0abbu, 0x81c2c92eu, 0x92722c85u,
            0xa2bfe8a1u, 0xa81a664bu, 0xc24b8b70u, 0xc76c51a3u, 0xd192e819u, 0xd6990624u, 0xf40e3585u, 0x106aa070u,
            0x19a4c116u, 0x1e376c08u, 0x2748774cu, 0x34b0bcb5u, 0x391c0cb3u, 0x4ed8aa4au, 0x5b9cca4fu, 0x682e6ff3u,
            0x748f82eeu, 0x78a5636fu, 0x84c87814u, 0x8cc70208u, 0x90befffau, 0xa4506cebu, 0xbef9a3f7u, 0xc67178f2u
        };
        uint32_t words[64]{};
        for (size_t i = 0; i < 16; ++i) {
            words[i] = (static_cast<uint32_t>(block[i * 4]) << 24) |
                       (static_cast<uint32_t>(block[i * 4 + 1]) << 16) |
                       (static_cast<uint32_t>(block[i * 4 + 2]) << 8) |
                       static_cast<uint32_t>(block[i * 4 + 3]);
        }
        for (size_t i = 16; i < 64; ++i) {
            const uint32_t s0 = rotate_right(words[i - 15], 7) ^ rotate_right(words[i - 15], 18) ^ (words[i - 15] >> 3);
            const uint32_t s1 = rotate_right(words[i - 2], 17) ^ rotate_right(words[i - 2], 19) ^ (words[i - 2] >> 10);
            words[i] = words[i - 16] + s0 + words[i - 7] + s1;
        }
        uint32_t a = state_[0], b = state_[1], c = state_[2], d = state_[3];
        uint32_t e = state_[4], f = state_[5], g = state_[6], h = state_[7];
        for (size_t i = 0; i < 64; ++i) {
            const uint32_t s1 = rotate_right(e, 6) ^ rotate_right(e, 11) ^ rotate_right(e, 25);
            const uint32_t choose = (e & f) ^ (~e & g);
            const uint32_t temp1 = h + s1 + choose + k[i] + words[i];
            const uint32_t s0 = rotate_right(a, 2) ^ rotate_right(a, 13) ^ rotate_right(a, 22);
            const uint32_t majority = (a & b) ^ (a & c) ^ (b & c);
            const uint32_t temp2 = s0 + majority;
            h = g; g = f; f = e; e = d + temp1; d = c; c = b; b = a; a = temp1 + temp2;
        }
        state_[0] += a; state_[1] += b; state_[2] += c; state_[3] += d;
        state_[4] += e; state_[5] += f; state_[6] += g; state_[7] += h;
    }

    std::array<uint32_t, 8> state_;
    std::array<uint8_t, 64> buffer_{};
    size_t buffer_size_ = 0;
    uint64_t total_bytes_ = 0;
};

std::string sha256_file(const fs::path& path) {
    std::ifstream input(path, std::ios::binary);
    if (!input) return {};
    Sha256 digest;
    std::array<char, 64 * 1024> buffer{};
    while (input) {
        input.read(buffer.data(), static_cast<std::streamsize>(buffer.size()));
        const std::streamsize count = input.gcount();
        if (count > 0) digest.update(reinterpret_cast<const uint8_t*>(buffer.data()), static_cast<size_t>(count));
    }
    return input.bad() ? std::string{} : digest.hex_digest();
}

bool is_sha256(const std::string& value) {
    return value.size() == 64 && lowercase(value) == value &&
           std::all_of(value.begin(), value.end(), [](unsigned char ch) {
               return std::isxdigit(ch) != 0;
           });
}

bool read_external_package_sha256(const fs::path& package_path, std::string& expected, std::string& error) {
    const fs::path checksum_path = package_path.string() + ".sha256";
    std::error_code status_error;
    const bool checksum_exists = fs::exists(checksum_path, status_error);
    if (status_error) {
        error = "cannot inspect package checksum sidecar";
        return false;
    }
    if (!checksum_exists) {
        error = "package checksum sidecar is missing";
        return false;
    }
    if (status_error || !fs::is_regular_file(checksum_path, status_error)) {
        error = "package checksum sidecar is not a regular file";
        return false;
    }

    std::ifstream input(checksum_path);
    if (!(input >> expected) || !is_sha256(expected)) {
        error = "package checksum sidecar contains an invalid SHA-256";
        return false;
    }
    return true;
}

bool write_package_sha256(const fs::path& package_root, const std::string& digest, std::string& error) {
    if (digest.empty()) return true;
    std::ofstream output(package_root / "package.sha256", std::ios::trunc);
    if (!output) {
        error = "cannot create installed package checksum record";
        return false;
    }
    output << digest << '\n';
    output.flush();
    if (!output) {
        error = "cannot write installed package checksum record";
        return false;
    }
    return true;
}

bool validate_zip_entries(const fs::path& zip_path, const fs::path& listing_path, std::string& error) {
    std::ifstream listing(listing_path);
    if (!listing) {
        error = "cannot read zip entry list";
        return false;
    }
    std::set<std::string> entries;
    std::set<std::string> case_insensitive_entries;
    std::string entry;
    size_t file_count = 0;
    while (std::getline(listing, entry)) {
        if (!entry.empty() && entry.back() == '\r') entry.pop_back();
        if (!entry.empty() && entry.back() == '/') entry.pop_back();
        if (!safe_relative_path(entry)) {
            error = "zip contains an unsafe path: " + entry;
            return false;
        }
        if (!entries.insert(entry).second || !case_insensitive_entries.insert(lowercase(entry)).second) {
            error = "zip contains duplicate or case-colliding paths: " + entry;
            return false;
        }
        if (++file_count > 10000) {
            error = "zip contains too many entries";
            return false;
        }
    }
    (void)zip_path;
    return true;
}

bool validate_zip_uncompressed_size(const fs::path& zip_path, std::string& error) {
    std::error_code size_error;
    constexpr uint64_t kMaxCompressedBytes = 512ULL * 1024 * 1024;
    constexpr uint64_t kMaxUncompressedBytes = 2ULL * 1024 * 1024 * 1024;
    const uint64_t compressed_bytes = fs::file_size(zip_path, size_error);
    if (size_error || compressed_bytes > kMaxCompressedBytes) {
        error = "zip compressed size exceeds the package limit";
        return false;
    }

    std::string listing;
    if (!capture_command({"unzip", "-Z", "-l", zip_path.string()}, listing, 4 * 1024 * 1024)) {
        error = "cannot inspect zip entry sizes";
        return false;
    }
    uint64_t total_bytes = 0;
    std::istringstream lines(listing);
    std::string line;
    while (std::getline(lines, line)) {
        std::istringstream fields(line);
        std::string permissions;
        std::string version;
        std::string platform;
        uint64_t entry_bytes = 0;
        if (!(fields >> permissions >> version >> platform >> entry_bytes)) continue;
        if (permissions.empty() || (permissions.front() != '-' && permissions.front() != 'd')) continue;
        if (entry_bytes > kMaxUncompressedBytes - total_bytes) {
            error = "zip uncompressed size exceeds the package limit";
            return false;
        }
        total_bytes += entry_bytes;
    }
    return true;
}

bool extract_zip(const fs::path& zip_path, StagingDirectory& staging, std::string& error) {

    const fs::path base = fs::temp_directory_path() / ("aivision-validator-" + std::to_string(static_cast<long long>(getpid())));
    for (int attempt = 0; attempt < 100; ++attempt) {
        const fs::path candidate = base.string() + "-" + std::to_string(attempt);
        std::error_code create_error;
        if (fs::create_directory(candidate, create_error)) {
            staging.path = candidate;
            break;
        }
    }
    if (staging.path.empty()) {
        error = "cannot create zip staging directory";
        return false;
    }

    const fs::path listing_path = staging.path / ".zip-entries";
    if (!run_command({"unzip", "-Z1", zip_path.string()}, &listing_path)) {
        error = "cannot inspect zip entries";
        return false;
    }
    if (!validate_zip_entries(zip_path, listing_path, error) ||
        !validate_zip_uncompressed_size(zip_path, error)) return false;
    std::error_code remove_error;
    fs::remove(listing_path, remove_error);
    if (!run_command({"unzip", "-qq", "-o", zip_path.string(), "-d", staging.path.string()})) {
        error = "cannot extract zip package";
        return false;
    }

    for (const auto& item : fs::recursive_directory_iterator(staging.path)) {
        if (item.path().filename() == ".zip-entries") continue;
        const auto status = item.symlink_status();
        if (fs::is_symlink(status) || (!fs::is_directory(status) && !fs::is_regular_file(status))) {
            error = "zip contains a non-regular extracted entry";
            return false;
        }
    }
    return true;
}

bool is_zip_path(const fs::path& path) {
    std::string extension = path.extension().string();
    std::transform(extension.begin(), extension.end(), extension.begin(), [](unsigned char c) {
        return static_cast<char>(std::tolower(c));
    });
    return extension == ".zip";
}

bool validate_package_tree(const fs::path& root, std::string& error) {
    std::error_code iterator_error;
    size_t entry_count = 0;
    for (fs::recursive_directory_iterator it(root, iterator_error), end; it != end; it.increment(iterator_error)) {
        if (iterator_error) {
            error = "cannot inspect package directory";
            return false;
        }
        const auto relative = it->path().lexically_relative(root).generic_string();
        if (!safe_relative_path(relative)) {
            error = "package contains an unsafe path: " + relative;
            return false;
        }
        const auto status = it->symlink_status();
        if (fs::is_symlink(status) || (!fs::is_directory(status) && !fs::is_regular_file(status))) {
            error = "package contains a non-regular entry: " + relative;
            return false;
        }
        if (++entry_count > 10000) {
            error = "package contains too many entries";
            return false;
        }
    }
    return true;
}


void self_test_callback(const av_algo_result* result, void* user) noexcept {
    auto* capture = static_cast<SelfTestCapture*>(user);
    if (!capture) return;
    ++capture->callback_count;
    if (!result || result->size < sizeof(av_algo_result) || result->api_version != AV_ALGO_API_VERSION ||
        result->kind != AV_RESULT_SELF_TEST || !result->json || result->json_len > AV_MAX_RESULT_JSON_BYTES ||
        result->image_count != 0 || result->images != nullptr) {
        return;
    }
    capture->json.assign(result->json, result->json_len);
    capture->valid = true;
}

bool validate_self_test_json(const std::string& json) {
    try {
        const auto value = nlohmann::json::parse(json);
        if (value.value("status", "") != "ok" || !value.contains("stages") || !value["stages"].is_array() ||
            value["stages"].empty() || !value.contains("object_count") || !value["object_count"].is_number_unsigned()) {
            return false;
        }
        for (const auto& stage : value["stages"]) {
            if (!stage.is_string() || stage.get<std::string>().empty()) return false;
        }
        return true;
    } catch (...) {
        return false;
    }
}

bool validate_object_keys(const nlohmann::json& object,
                          std::initializer_list<const char*> allowed,
                          std::string& error) {
    if (!object.is_object()) {
        error = "manifest field must be an object";
        return false;
    }
    for (const auto& item : object.items()) {
        bool known = false;
        for (const char* key : allowed) {
            if (item.key() == key) {
                known = true;
                break;
            }
        }
        if (!known) {
            error = "manifest contains an unknown field: " + item.key();
            return false;
        }
    }
    return true;
}

bool is_semver(const std::string& value) {
    static const std::regex pattern(R"(^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$)");
    return std::regex_match(value, pattern);
}

bool validate_manifest_files(const fs::path& root, const nlohmann::json& manifest, std::string& error) {
    if (!manifest.is_object()) {
        error = "manifest must be an object";
        return false;
    }
    const std::set<std::string> allowed_top_level = {
        "manifest_version", "algorithm_id", "version", "name", "description", "algorithm_type",
        "alarm_type_id", "platform_id", "min_adapter_version", "runtime_constraints", "resource_profile",
        "entry_library", "config_schema_file", "test_image_file", "self_test", "files"
    };
    for (const auto& item : manifest.items()) {
        if (!allowed_top_level.contains(item.key())) {
            error = "manifest contains an unknown top-level field: " + item.key();
            return false;
        }
    }

    const auto required_string = [&](const char* key, std::string& value) {
        if (!manifest.contains(key) || !manifest.at(key).is_string()) {
            error = std::string("manifest field must be a string: ") + key;
            return false;
        }
        value = manifest.at(key).get<std::string>();
        return true;
    };
    if (!manifest.contains("manifest_version") || !manifest.at("manifest_version").is_number_integer() ||
        manifest.at("manifest_version") != 1) {
        error = "manifest_version must be 1";
        return false;
    }

    std::string algorithm_id;
    std::string version;
    std::string name;
    std::string algorithm_type;
    std::string alarm_type_id;
    std::string platform_id;
    std::string min_adapter_version;
    std::string entry_library;
    std::string config_schema_file;
    std::string test_image_file;
    if (!required_string("algorithm_id", algorithm_id) || !required_string("version", version) ||
        !required_string("name", name) || !required_string("algorithm_type", algorithm_type) ||
        !required_string("alarm_type_id", alarm_type_id) || !required_string("platform_id", platform_id) ||
        !required_string("min_adapter_version", min_adapter_version) || !required_string("entry_library", entry_library) ||
        !required_string("config_schema_file", config_schema_file) || !required_string("test_image_file", test_image_file)) {
        return false;
    }
    if (algorithm_id.size() < 3 || algorithm_id.size() > 32 ||
        !std::regex_match(algorithm_id, std::regex(R"(^[a-z0-9_-]+$)")) || !is_semver(version) ||
        name.empty() || name.size() > 64 || algorithm_type != "object_detection" ||
        alarm_type_id.size() < 3 || alarm_type_id.size() > 32 ||
        !std::regex_match(alarm_type_id, std::regex(R"(^[a-z0-9_]+$)")) ||
        (platform_id != "mock" &&
         !std::regex_match(platform_id, std::regex(R"(^[a-z0-9]+(?:-[a-z0-9]+)+$)"))) ||
        !is_semver(min_adapter_version)) {
        error = "manifest identifier, version, or algorithm type is invalid";
        return false;
    }
    if (manifest.contains("description") &&
        (!manifest.at("description").is_string() || manifest.at("description").get<std::string>().size() > 256)) {
        error = "manifest description is invalid";
        return false;
    }

    if (!manifest.contains("runtime_constraints") ||
        !validate_object_keys(manifest.at("runtime_constraints"), {"min_os_version"}, error) ||
        !manifest.at("runtime_constraints").contains("min_os_version") ||
        !manifest.at("runtime_constraints").at("min_os_version").is_string() ||
        !std::regex_match(manifest.at("runtime_constraints").at("min_os_version").get<std::string>(),
                          std::regex(R"(^[0-9]+\.[0-9]+$)"))) {
        error = "runtime_constraints.min_os_version is invalid";
        return false;
    }

    if (!manifest.contains("resource_profile") ||
        !validate_object_keys(manifest.at("resource_profile"), {"min_free_memory_mb", "fps_tiers"}, error) ||
        !manifest.at("resource_profile").contains("min_free_memory_mb") ||
        !manifest.at("resource_profile").at("min_free_memory_mb").is_number_unsigned() ||
        manifest.at("resource_profile").at("min_free_memory_mb") == 0 ||
        !manifest.at("resource_profile").contains("fps_tiers") ||
        !manifest.at("resource_profile").at("fps_tiers").is_array() ||
        manifest.at("resource_profile").at("fps_tiers").empty()) {
        error = "resource_profile is invalid";
        return false;
    }
    uint64_t previous_fps = 0;
    for (const auto& tier : manifest.at("resource_profile").at("fps_tiers")) {
        if (!tier.is_object() || tier.size() != 2 || !tier.contains("fps") || !tier.contains("units") ||
            !tier.at("fps").is_number_unsigned() || !tier.at("units").is_number_unsigned() ||
            tier.at("fps") == 0 || tier.at("units") == 0 || tier.at("units") > 1000 ||
            tier.at("fps") <= previous_fps) {
            error = "resource_profile.fps_tiers is invalid";
            return false;
        }
        previous_fps = tier.at("fps").get<uint64_t>();
    }

    if (!manifest.contains("self_test") ||
        !validate_object_keys(manifest.at("self_test"), {"timeout_ms", "input_mode"}, error) ||
        manifest.at("self_test").size() != 2 || !manifest.at("self_test").contains("timeout_ms") ||
        !manifest.at("self_test").at("timeout_ms").is_number_unsigned() ||
        manifest.at("self_test").at("timeout_ms") < 1 || manifest.at("self_test").at("timeout_ms") > 60000 ||
        !manifest.at("self_test").contains("input_mode") ||
        manifest.at("self_test").at("input_mode") != "test_image") {
        error = "self_test is invalid";
        return false;
    }

    if (!safe_relative_path(entry_library) || !safe_relative_path(config_schema_file) ||
        !safe_relative_path(test_image_file) || !fs::is_regular_file(root / config_schema_file) ||
        !fs::is_regular_file(root / test_image_file)) {
        error = "manifest entry, schema, or test image path is invalid";
        return false;
    }
    try {
        std::ifstream schema_input(root / config_schema_file);
        const auto schema = nlohmann::json::parse(schema_input);
        if (!schema.is_object() || schema.value("type", "") != "object" ||
            !schema.contains("additionalProperties") || !schema.at("additionalProperties").is_boolean() ||
            schema.at("additionalProperties") != false) {
            error = "config schema must be an object with additionalProperties=false";
            return false;
        }
    } catch (const std::exception& exception) {
        error = std::string("config schema is invalid: ") + exception.what();
        return false;
    }

    if (!manifest.contains("files") || !manifest.at("files").is_array() || manifest.at("files").size() < 3) {
        error = "manifest files list is invalid";
        return false;
    }
    std::set<std::string> paths;
    size_t library_count = 0;
    size_t schema_count = 0;
    size_t test_image_count = 0;
    std::string library_path;
    for (const auto& file : manifest.at("files")) {
        if (!file.is_object() || file.size() != 3 || !file.contains("path") || !file.contains("kind") ||
            !file.contains("sha256") || !file.at("path").is_string() || !file.at("kind").is_string() ||
            !file.at("sha256").is_string()) {
            error = "manifest files entry is invalid";
            return false;
        }
        const std::string path = file.at("path").get<std::string>();
        const std::string kind = file.at("kind").get<std::string>();
        const std::string expected = file.at("sha256").get<std::string>();
        if (!safe_relative_path(path) || !paths.insert(path).second || !fs::is_regular_file(root / path) ||
            (kind != "library" && kind != "config_schema" && kind != "test_image" && kind != "model") ||
            expected.size() != 64 || lowercase(expected) != expected || sha256_file(root / path) != expected) {
            error = "manifest file path, kind, or sha256 is invalid: " + path;
            return false;
        }
        if (kind == "library") {
            ++library_count;
            library_path = path;
        } else if (kind == "config_schema") {
            ++schema_count;
            if (path != config_schema_file) {
                error = "config_schema entry does not match config_schema_file";
                return false;
            }
        } else if (kind == "test_image") {
            ++test_image_count;
            if (path != test_image_file) {
                error = "test_image entry does not match test_image_file";
                return false;
            }
        }
    }
    if (library_count != 1 || schema_count != 1 || test_image_count != 1 || library_path != entry_library) {
        error = "manifest must contain exactly one library, schema, and test image entry";
        return false;
    }
    return true;
}

} // namespace

ValidationResult PackageValidator::validate_and_extract(const std::string& package_zip_or_dir,
                                                         const std::string& install_base_dir,
                                                         SelfTestFrameFactory frame_factory,
                                                         SelfTestFrameReleaser frame_releaser) {
    ValidationResult result;
    const fs::path input_path(package_zip_or_dir);
    if (!fs::exists(input_path)) {
        result.error_stage = "structure";
        result.error_message = "Package path does not exist: " + package_zip_or_dir;
        return result;
    }
    std::error_code input_error;
    const auto input_status = fs::symlink_status(input_path, input_error);
    if (input_error || fs::is_symlink(input_status)) {
        result.error_stage = "structure";
        result.error_message = "Package path must not be a symbolic link";
        return result;
    }

    StagingDirectory staging;
    fs::path working_dir = input_path;
    if (fs::is_regular_file(input_path) && is_zip_path(input_path)) {
        result.package_sha256 = sha256_file(input_path);
        if (!is_sha256(result.package_sha256)) {
            result.error_code = "PACKAGE_CHECKSUM_MISMATCH";
            result.error_stage = "checksum";
            result.error_message = "cannot compute package SHA-256";
            return result;
        }
        std::string expected_sha256;
        std::string checksum_error;
        if (!read_external_package_sha256(input_path, expected_sha256, checksum_error)) {
            result.error_code = "PACKAGE_CHECKSUM_MISMATCH";
            result.error_stage = "checksum";
            result.error_message = checksum_error;
            return result;
        }
        if (!expected_sha256.empty() && expected_sha256 != result.package_sha256) {
            result.error_code = "PACKAGE_CHECKSUM_MISMATCH";
            result.error_stage = "checksum";
            result.error_message = "package SHA-256 does not match the external checksum";
            return result;
        }

        std::string error;
        if (!extract_zip(input_path, staging, error)) {
            result.error_stage = "extract";
            result.error_message = error;
            return result;
        }
        working_dir = staging.path;
    }
    if (!fs::is_directory(working_dir)) {
        result.error_stage = "structure";
        result.error_message = "Package must be a directory or .zip archive";
        return result;
    }

    if (!validate_package_tree(working_dir, result.error_message)) {
        result.error_stage = "structure";
        return result;
    }

    const fs::path manifest_path = working_dir / "manifest.json";
    if (!fs::is_regular_file(manifest_path)) {
        result.error_stage = "manifest";
        result.error_message = "manifest.json not found in package root";
        return result;
    }

    nlohmann::json manifest;
    try {
        std::ifstream input(manifest_path);
        manifest = nlohmann::json::parse(input);
        result.manifest.algorithm_id = manifest.value("algorithm_id", "");
        result.manifest.version = manifest.value("version", "");
        result.manifest.platform_id = manifest.value("platform_id", "");
        result.manifest.algorithm_type = manifest.value("algorithm_type", "");
        result.manifest.alarm_type_id = manifest.value("alarm_type_id", "");
        result.manifest.min_engine_version = manifest.value("min_adapter_version", "1.0.0");
        result.manifest.library_name = manifest.value("entry_library", "");
        if (result.manifest.library_name.empty()) result.manifest.library_name = manifest.value("library_name", "");
        if (result.manifest.algorithm_id.empty() || result.manifest.version.empty() ||
            result.manifest.platform_id.empty() || result.manifest.algorithm_type.empty() ||
            result.manifest.alarm_type_id.empty() || result.manifest.library_name.empty()) {
            result.error_stage = "manifest";
            result.error_message = "Required manifest fields are missing";
            return result;
        }
        if (result.manifest.algorithm_type != "object_detection") {
            result.error_stage = "manifest";
            result.error_message = "Only object_detection packages are supported by this engine";
            return result;
        }
        if (!safe_relative_path(result.manifest.library_name)) {
            result.error_stage = "manifest";
            result.error_message = "Manifest entry library path is unsafe";
            return result;
        }
        if (!safe_identifier(result.manifest.algorithm_id) || !safe_identifier(result.manifest.version) ||
            !safe_identifier(result.manifest.platform_id)) {
            result.error_stage = "manifest";
            result.error_message = "Manifest identifiers contain unsafe characters";
            return result;
        }
        std::string file_error;
        if (!validate_manifest_files(working_dir, manifest, file_error)) {
            result.error_stage = "manifest";
            result.error_message = file_error;
            return result;
        }
    } catch (const std::exception& error) {
        result.error_stage = "manifest";
        result.error_message = std::string("Failed to parse manifest.json: ") + error.what();
        return result;
    }

    fs::path test_image_path;
    try {
        test_image_path = manifest.value("test_image_file", std::string("testimage.jpg"));
    } catch (const std::exception& error) {
        result.error_stage = "manifest";
        result.error_message = std::string("Manifest test_image_file is invalid: ") + error.what();
        return result;
    }
    if (!safe_relative_path(test_image_path.string()) || !fs::is_regular_file(working_dir / test_image_path)) {
        result.error_stage = "manifest";
        result.error_message = "Manifest test_image_file is invalid or missing";
        return result;
    }

    const fs::path library_path = working_dir / result.manifest.library_name;
    if (!fs::is_regular_file(library_path)) {
        result.error_stage = "dlopen";
        result.error_message = "Dynamic library file not found: " + library_path.string();
        return result;
    }

    if (!validate_exported_symbols(library_path, result.error_message)) {
        result.error_stage = "symbol_audit";
        return result;
    }

    void* handle = dlopen(library_path.c_str(), RTLD_NOW | RTLD_LOCAL);
    if (!handle) {
        const char* error = dlerror();
        result.error_stage = "dlopen";
        result.error_message = std::string("dlopen failed: ") + (error ? error : "unknown error");
        return result;
    }
    auto close_library = [&] { dlclose(handle); };

    auto get_abi = reinterpret_cast<av_algo_get_abi_fn>(dlsym(handle, AV_ALGO_GET_ABI_SYMBOL));
    if (!get_abi) {
        close_library();
        result.error_stage = "abi";
        result.error_message = "Missing entry point symbol: av_algo_get_abi";
        return result;
    }
    const av_algo_abi* abi = get_abi(AV_ALGO_API_VERSION);
    if (!abi || abi->size < sizeof(av_algo_abi) || abi->api_version != AV_ALGO_API_VERSION ||
        !abi->library_open || !abi->library_query || !abi->library_close || !abi->instance_create ||
        !abi->instance_negotiate || !abi->instance_update_config || !abi->instance_set_rules ||
        !abi->instance_process || !abi->instance_flush || !abi->instance_destroy || !abi->last_error) {
        close_library();
        result.error_stage = "abi";
        result.error_message = "Algorithm ABI is incomplete or incompatible";
        return result;
    }

    av_algo_library_args library_args{};
    library_args.size = sizeof(library_args);
    library_args.api_version = AV_ALGO_API_VERSION;
    library_args.package_root = working_dir.c_str();
    library_args.platform_id = result.manifest.platform_id.c_str();
    av_algo_library library = nullptr;
    if (abi->library_open(&library_args, &library) != AV_OK || !library) {
        close_library();
        result.error_stage = "library_open";
        result.error_message = "library_open failed";
        return result;
    }

    av_algo_library_info info{};
    info.size = sizeof(info);
    info.api_version = AV_ALGO_API_VERSION;
    if (abi->library_query(library, &info) != AV_OK || std::string(info.algorithm_id) != result.manifest.algorithm_id ||
        std::string(info.version) != result.manifest.version || std::string(info.algorithm_type) != "object_detection" ||
        std::string(info.alarm_type_id) != result.manifest.alarm_type_id) {
        abi->library_close(library);
        close_library();
        result.error_stage = "library_query";
        result.error_message = "Library metadata does not match manifest";
        return result;
    }

    SelfTestCapture capture;
    av_algo_instance_args instance_args{};
    instance_args.size = sizeof(instance_args);
    instance_args.api_version = AV_ALGO_API_VERSION;
    instance_args.mode = AV_INSTANCE_INSTALL_SELF_TEST;
    instance_args.instance_id = "self_test_inst";
    instance_args.instance_run_id = "self_test_run";
    instance_args.on_result = self_test_callback;
    instance_args.result_user = &capture;
    av_algo_instance instance = nullptr;
    if (abi->instance_create(library, &instance_args, &instance) != AV_OK || !instance) {
        abi->library_close(library);
        close_library();
        result.error_stage = "instance_create";
        result.error_message = "instance_create for self-test failed";
        return result;
    }

    av_frame_desc test_frame{};
    test_frame.size = sizeof(test_frame);
    test_frame.api_version = AV_ALGO_API_VERSION;
    test_frame.width = 640;
    test_frame.height = 640;
    test_frame.alloc_width = 640;
    test_frame.alloc_height = 640;
    test_frame.pixel_format = AV_PIX_NV12;
    test_frame.memory_type = AV_MEM_PLATFORM_SURFACE;
    test_frame.layout = AV_LAYOUT_PLATFORM_NATIVE;
    test_frame.opaque_kind = AV_OPAQUE_CVPIXELBUFFER;
    test_frame.plane_count = 2;
    test_frame.stride[0] = 640;
    test_frame.stride[1] = 640;
    test_frame.color_primaries = AV_COLOR_PRIM_BT709;
    test_frame.color_transfer = AV_COLOR_TRC_BT709;
    test_frame.color_matrix = AV_COLOR_MAT_BT709;
    test_frame.color_range = AV_COLOR_RANGE_LIMITED;
    void* frame_owner = nullptr;
    const std::string test_image_file = test_image_path.string();
    if (frame_factory && !frame_factory(working_dir.c_str(), test_image_file.c_str(), &test_frame, &frame_owner)) {
        abi->instance_destroy(instance);
        abi->library_close(library);
        close_library();
        result.error_stage = "self_test_frame";
        result.error_message = "Could not construct the platform test frame";
        return result;
    }

    av_frame_caps offered{};
    offered.size = sizeof(offered);
    offered.api_version = AV_ALGO_API_VERSION;
    offered.pixel_format_count = 1;
    offered.pixel_formats[0] = AV_PIX_NV12;
    offered.memory_type_count = 1;
    offered.memory_types[0] = AV_MEM_PLATFORM_SURFACE;
    offered.required_opaque_kind = AV_OPAQUE_CVPIXELBUFFER;
    offered.min_width = test_frame.width;
    offered.min_height = test_frame.height;
    offered.max_width = test_frame.width;
    offered.max_height = test_frame.height;
    av_frame_caps accepted{};
    accepted.size = sizeof(accepted);
    accepted.api_version = AV_ALGO_API_VERSION;
    const int negotiate_status = abi->instance_negotiate(instance, &offered, &accepted);
    const int process_status = negotiate_status == AV_OK ? abi->instance_process(instance, &test_frame) : negotiate_status;
    if (frame_releaser && frame_owner) frame_releaser(frame_owner);
    const int destroy_status = abi->instance_destroy(instance);
    const int close_status = abi->library_close(library);
    close_library();

    if (negotiate_status != AV_OK || process_status != AV_OK || destroy_status != AV_OK || close_status != AV_OK ||
        capture.callback_count != 1 || !capture.valid || !validate_self_test_json(capture.json)) {
        result.error_stage = "self_test";
        result.error_message = "self-test did not produce exactly one valid result";
        return result;
    }

    if (!install_base_dir.empty()) {
        const fs::path target = fs::path(install_base_dir) / result.manifest.algorithm_id / result.manifest.version;
        const fs::path parent = target.parent_path();
        const fs::path temporary = parent / (target.filename().string() + ".part-" + std::to_string(getpid()));
        const fs::path backup = parent / (target.filename().string() + ".old-" + std::to_string(getpid()));
        std::error_code install_error;
        fs::create_directories(parent, install_error);
        if (install_error) {
            result.error_stage = "install";
            result.error_message = "Cannot create install directory: " + install_error.message();
            return result;
        }
        fs::remove_all(temporary, install_error);
        fs::remove_all(backup, install_error);
        fs::copy(working_dir, temporary, fs::copy_options::recursive | fs::copy_options::overwrite_existing, install_error);
        if (install_error) {
            fs::remove_all(temporary, install_error);
            result.error_stage = "install";
            result.error_message = install_error.message();
            return result;
        }
        std::string checksum_error;
        if (!write_package_sha256(temporary, result.package_sha256, checksum_error)) {
            fs::remove_all(temporary, install_error);
            result.error_stage = "install";
            result.error_message = checksum_error;
            return result;
        }

        bool had_previous = false;
        const bool target_exists = fs::exists(target, install_error);
        if (install_error) {
            fs::remove_all(temporary, install_error);
            result.error_stage = "install";
            result.error_message = "Cannot inspect previous package version";
            return result;
        }
        if (target_exists) {
            fs::rename(target, backup, install_error);
            if (install_error) {
                fs::remove_all(temporary, install_error);
                result.error_stage = "install";
                result.error_message = "Cannot stage previous package version";
                return result;
            }
            had_previous = true;
        }
        fs::rename(temporary, target, install_error);
        if (install_error) {
            if (had_previous) {
                std::error_code restore_error;
                fs::rename(backup, target, restore_error);
            }
            fs::remove_all(temporary, install_error);
            result.error_stage = "install";
            result.error_message = "Cannot activate validated package";
            return result;
        }
        if (had_previous) fs::remove_all(backup, install_error);
    }

    result.success = true;
    return result;
}

ValidationResult PackageValidator::run_sandbox_validator(const std::string& validator_bin_path,
                                                         const std::string& package_path,
                                                         const std::string& install_base_dir) {
    if (validator_bin_path.empty() ||
        (validator_bin_path.find('/') != std::string::npos && !fs::is_regular_file(validator_bin_path))) {
        ValidationResult result;
        result.error_stage = "sandbox_spawn";
        result.error_message = "sandbox validator binary is unavailable";
        return result;
    }

    std::string output;
    if (!capture_command({validator_bin_path, package_path, install_base_dir}, output, 1024 * 1024, true)) {
        ValidationResult result;
        const std::string error_prefix = "Error code: ";
        const size_t error_marker = output.find(error_prefix);
        if (error_marker != std::string::npos) {
            const size_t error_start = error_marker + error_prefix.size();
            const size_t error_end = output.find_first_of("\r\n", error_start);
            result.error_code = output.substr(
                error_start, error_end == std::string::npos ? std::string::npos : error_end - error_start);
        }
        result.error_stage = "sandbox_process";
        result.error_message = "Validator process exited with failure or exceeded its deadline";
        return result;
    }

    constexpr std::string_view prefix = "Successfully validated package: ";
    const size_t marker = output.find(prefix);
    if (marker == std::string::npos) {
        ValidationResult result;
        result.error_stage = "sandbox_output";
        result.error_message = "Validator output did not contain a validated package identity";
        return result;
    }
    const size_t start = marker + prefix.size();
    const size_t end = output.find_first_of("\r\n", start);
    const std::string identity = output.substr(start, end == std::string::npos ? std::string::npos : end - start);
    const size_t separator = identity.rfind('@');
    if (separator == std::string::npos || separator == 0 || separator + 1 >= identity.size()) {
        ValidationResult result;
        result.error_stage = "sandbox_output";
        result.error_message = "Validator output contained an invalid package identity";
        return result;
    }

    std::string package_sha256;
    constexpr std::string_view checksum_prefix = "Package SHA-256: ";
    const size_t checksum_marker = output.find(checksum_prefix);
    if (checksum_marker != std::string::npos) {
        const size_t checksum_start = checksum_marker + checksum_prefix.size();
        const size_t checksum_end = output.find_first_of("\r\n", checksum_start);
        package_sha256 = output.substr(
            checksum_start, checksum_end == std::string::npos ? std::string::npos : checksum_end - checksum_start);
        if (!is_sha256(package_sha256)) {
            ValidationResult result;
            result.error_stage = "sandbox_output";
            result.error_message = "Validator output contained an invalid package SHA-256";
            return result;
        }
    } else if (is_zip_path(fs::path(package_path))) {
        ValidationResult result;
        result.error_stage = "sandbox_output";
        result.error_message = "Validator output did not contain the package SHA-256";
        return result;
    }

    ValidationResult result;
    result.success = true;
    result.package_sha256 = std::move(package_sha256);
    result.manifest.algorithm_id = identity.substr(0, separator);
    result.manifest.version = identity.substr(separator + 1);
    return result;
}

} // namespace aivision::core
