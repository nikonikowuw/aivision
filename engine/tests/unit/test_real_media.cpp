/**
 * @file test_real_media.cpp
 * @brief 真实流媒体端到端集成测试（拉起 rtsp_server 测试 RTSP 拉流、VideoToolbox 解码与算法推理）
 */

#include <cerrno>
#include <chrono>
#include <csignal>
#include <cstdlib>
#include <dlfcn.h>
#include <filesystem>
#include <fcntl.h>
#include <gtest/gtest.h>
#include <netinet/in.h>
#include <spawn.h>
#include <string>
#include <sys/socket.h>
#include <sys/wait.h>
#include <thread>
#include <unistd.h>
#include <vector>


#include "aivision/core/algo_instance.hpp"
#include "aivision/core/camera_task.hpp"
#include "aivision/core/frame_pool.hpp"
#include "aivision/media/media_api.hpp"
#include "aivision/platform/platform_api.hpp"

#ifdef AIVISION_PLATFORM_MACOS
#include "aivision/platform/macos_platform.hpp"
#endif

extern char** environ;

namespace fs = std::filesystem;

#if defined(AIVISION_PLATFORM_MACOS) && defined(AIVISION_USE_ZLM) && !defined(AIVISION_SKIP_REAL_MEDIA_TESTS)
namespace {

std::string shell_quote(const std::string& value) {
    std::string quoted{"'"};
    for (const char character : value) {
        if (character == '\'') {
            quoted += "'\\''";
        } else {
            quoted += character;
        }
    }
    quoted += '\'';
    return quoted;
}

} // namespace

class RealMediaIntegrationTest : public ::testing::Test {
protected:
    static fs::path fixture_dir() {
        return fs::path(AIVISION_BINARY_DIR) / "tests" / "fixtures" / "media";
    }

    static void SetUpTestSuite() {
        const fs::path root_dir = AIVISION_SOURCE_DIR;
        const fs::path script_path = root_dir / "tests" / "media" / "generate_fixtures.sh";
        const std::string command = "bash " + shell_quote(script_path.string()) + " " +
                                    shell_quote(fixture_dir().string());
        ASSERT_EQ(std::system(command.c_str()), 0);
    }

    void SetUp() override {
        port_ = 8556;
        server_pid_ = -1;
    }

    void TearDown() override {
        stop_server();
    }

    std::shared_ptr<aivision::core::AlgorithmInstance> create_mock_instance(
        const std::string& instance_id, const std::string& camera_id, int32_t target_fps = 25) {
        const std::filesystem::path library_path =
            std::filesystem::path(AIVISION_FIXTURE_PACKAGE_DIR) / "lib/libmock-detector.dylib";
        void* library = dlopen(library_path.c_str(), RTLD_NOW | RTLD_LOCAL);
        if (!library) return nullptr;
        auto get_abi = reinterpret_cast<av_algo_get_abi_fn>(dlsym(library, AV_ALGO_GET_ABI_SYMBOL));
        if (!get_abi) return nullptr;
        const av_algo_abi* abi = get_abi(AV_ALGO_API_VERSION);
        if (!abi) return nullptr;

        av_algo_library_args library_args{};
        library_args.size = sizeof(library_args);
        library_args.api_version = AV_ALGO_API_VERSION;
        av_algo_library algorithm_library = nullptr;
        if (abi->library_open(&library_args, &algorithm_library) != AV_OK || !algorithm_library) {
            return nullptr;
        }

        return std::make_shared<aivision::core::AlgorithmInstance>(
            instance_id, camera_id, "mock-detector", "1.0.0", target_fps, "{}", abi, algorithm_library);
    }

    bool start_server(const fs::path& media_file, const std::string& stream_name) {
        const fs::path server_exe = fs::path(AIVISION_BINARY_DIR) / "tests" / "test_rtsp_server";
        if (!fs::exists(server_exe) || !fs::exists(media_file)) return false;

        const std::string port = std::to_string(port_);
        const std::string executable = server_exe.string();
        const std::string media_path = media_file.string();
        std::vector<char*> arguments{
            const_cast<char*>(executable.c_str()),
            const_cast<char*>(port.c_str()),
            const_cast<char*>(media_path.c_str()),
            const_cast<char*>(stream_name.c_str()),
            nullptr,
        };

        posix_spawn_file_actions_t actions;
        if (posix_spawn_file_actions_init(&actions) != 0) return false;
        const int open_stdout = posix_spawn_file_actions_addopen(&actions, STDOUT_FILENO, "/dev/null", O_WRONLY, 0);
        const int open_stderr = posix_spawn_file_actions_addopen(&actions, STDERR_FILENO, "/dev/null", O_WRONLY, 0);
        const int spawn_status = (open_stdout == 0 && open_stderr == 0)
            ? posix_spawn(&server_pid_, executable.c_str(), &actions, nullptr, arguments.data(), environ)
            : EINVAL;
        posix_spawn_file_actions_destroy(&actions);
        if (spawn_status != 0 || server_pid_ <= 0) {
            server_pid_ = -1;
            return false;
        }

        return wait_for_server_ready(std::chrono::seconds(5));
    }

    bool wait_for_server_ready(std::chrono::milliseconds timeout) {
        const auto deadline = std::chrono::steady_clock::now() + timeout;
        while (std::chrono::steady_clock::now() < deadline) {
            int status = 0;
            const pid_t result = waitpid(server_pid_, &status, WNOHANG);
            if (result == server_pid_) return false;
            if (result < 0 && errno != EINTR) return false;

            const int socket_fd = socket(AF_INET, SOCK_STREAM, 0);
            if (socket_fd >= 0) {
                sockaddr_in address{};
                address.sin_family = AF_INET;
                address.sin_port = htons(port_);
                address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
                const bool connected = connect(socket_fd, reinterpret_cast<const sockaddr*>(&address),
                                               sizeof(address)) == 0;
                close(socket_fd);
                if (connected) return true;
            }
            std::this_thread::sleep_for(std::chrono::milliseconds(25));
        }
        return false;
    }

    bool wait_for_server_exit(std::chrono::milliseconds timeout) const {
        const auto deadline = std::chrono::steady_clock::now() + timeout;
        int status = 0;
        while (std::chrono::steady_clock::now() < deadline) {
            const pid_t result = waitpid(server_pid_, &status, WNOHANG);
            if (result == server_pid_) return true;
            if (result < 0 && errno == ECHILD) return true;
            if (result < 0 && errno != EINTR) return false;
            std::this_thread::sleep_for(std::chrono::milliseconds(25));
        }
        return false;
    }

    void stop_server() {
        if (server_pid_ <= 0) return;
        kill(server_pid_, SIGTERM);
        if (!wait_for_server_exit(std::chrono::seconds(5))) {
            kill(server_pid_, SIGKILL);
            int status = 0;
            while (waitpid(server_pid_, &status, 0) < 0 && errno == EINTR) {
            }
        }
        server_pid_ = -1;
    }

    uint16_t port_ = 8556;
    pid_t server_pid_ = -1;
};

TEST_F(RealMediaIntegrationTest, DecodeH264StreamWithVideoToolbox) {
    const fs::path h264_file = fixture_dir() / "test_1080p_h264.mp4";
    ASSERT_TRUE(start_server(h264_file, "h264_test"));

    auto media_backend = aivision::media::create_zlm_backend();
    auto platform_adapter = std::make_shared<aivision::platform::MacosPlatformAdapter>();
    const std::string rtsp_url = "rtsp://127.0.0.1:" + std::to_string(port_) + "/live/h264_test";
    auto task = std::make_shared<aivision::core::CameraTask>(
        "cam_real_h264", rtsp_url, platform_adapter, media_backend);
    auto inst = std::make_shared<aivision::core::AlgorithmInstance>(
        "inst_h264", "cam_real_h264", "mock_algo", "1.0.0", 25, "{}", nullptr, nullptr);
    ASSERT_EQ(inst->init(aivision::core::FramePool::instance().get_frame_ops(),
                         platform_adapter->get_c_image_ops()), AV_OK);

    task->add_instance(inst);
    ASSERT_EQ(task->start(), AV_OK);

    for (int i = 0; i < 60; ++i) {
        if (task->get_decoded_frames() >= 10 && inst->get_processed_frames() >= 10) break;
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }

    EXPECT_GE(task->get_decoded_frames(), 10U);
    EXPECT_GE(inst->get_processed_frames(), 10U);
    task->stop();
}

TEST_F(RealMediaIntegrationTest, DecodeH265StreamWithVideoToolbox) {
    const fs::path h265_file = fixture_dir() / "test_1080p_h265.mp4";
    ASSERT_TRUE(start_server(h265_file, "h265_test"));

    auto media_backend = aivision::media::create_zlm_backend();
    auto platform_adapter = std::make_shared<aivision::platform::MacosPlatformAdapter>();
    const std::string rtsp_url = "rtsp://127.0.0.1:" + std::to_string(port_) + "/live/h265_test";
    auto task = std::make_shared<aivision::core::CameraTask>(
        "cam_real_h265", rtsp_url, platform_adapter, media_backend);
    auto inst = std::make_shared<aivision::core::AlgorithmInstance>(
        "inst_h265", "cam_real_h265", "mock_algo", "1.0.0", 25, "{}", nullptr, nullptr);
    ASSERT_EQ(inst->init(aivision::core::FramePool::instance().get_frame_ops(),
                         platform_adapter->get_c_image_ops()), AV_OK);

    task->add_instance(inst);
    ASSERT_EQ(task->start(), AV_OK);

    for (int i = 0; i < 60; ++i) {
        if (task->get_decoded_frames() >= 10 && inst->get_processed_frames() >= 10) break;
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }

    EXPECT_GE(task->get_decoded_frames(), 10U);
    EXPECT_GE(inst->get_processed_frames(), 10U);
    task->stop();
}

TEST_F(RealMediaIntegrationTest, StreamDisconnectAndAutoReconnect) {
    const fs::path h264_file = fixture_dir() / "test_1080p_h264.mp4";
    ASSERT_TRUE(start_server(h264_file, "reconnect_test"));

    auto media_backend = aivision::media::create_zlm_backend();
    auto platform_adapter = std::make_shared<aivision::platform::MacosPlatformAdapter>();
    const std::string rtsp_url = "rtsp://127.0.0.1:" + std::to_string(port_) + "/live/reconnect_test";
    auto task = std::make_shared<aivision::core::CameraTask>(
        "cam_reconnect", rtsp_url, platform_adapter, media_backend);
    auto inst = std::make_shared<aivision::core::AlgorithmInstance>(
        "inst_rec", "cam_reconnect", "mock_algo", "1.0.0", 25, "{}", nullptr, nullptr);
    ASSERT_EQ(inst->init(aivision::core::FramePool::instance().get_frame_ops(),
                         platform_adapter->get_c_image_ops()), AV_OK);

    task->add_instance(inst);
    ASSERT_EQ(task->start(), AV_OK);
    for (int i = 0; i < 40; ++i) {
        if (task->get_decoded_frames() >= 10) break;
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    EXPECT_GE(task->get_decoded_frames(), 5U);

    stop_server();
    std::this_thread::sleep_for(std::chrono::milliseconds(1500));
    const uint64_t frames_before_resume = task->get_decoded_frames();
    ASSERT_TRUE(start_server(h264_file, "reconnect_test"));

    for (int i = 0; i < 50; ++i) {
        if (task->get_decoded_frames() >= frames_before_resume + 10) break;
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }

    EXPECT_GT(task->get_decoded_frames(), frames_before_resume);
    task->stop();
}

TEST_F(RealMediaIntegrationTest, DynamicTrackReplacementResolutionChange) {
    const fs::path h264_1080p = fixture_dir() / "test_1080p_h264.mp4";
    const fs::path h264_720p = fixture_dir() / "test_720p_h264.mp4";
    ASSERT_TRUE(start_server(h264_1080p, "track_switch_test"));

    auto media_backend = aivision::media::create_zlm_backend();
    auto platform_adapter = std::make_shared<aivision::platform::MacosPlatformAdapter>();
    const std::string rtsp_url = "rtsp://127.0.0.1:" + std::to_string(port_) + "/live/track_switch_test";
    auto task = std::make_shared<aivision::core::CameraTask>(
        "cam_track_switch", rtsp_url, platform_adapter, media_backend);

    std::atomic<uint32_t> last_width{0};
    std::atomic<uint32_t> last_height{0};
    std::atomic<uint32_t> count_1080p{0};
    std::atomic<uint32_t> count_720p{0};

    auto inst = create_mock_instance("inst_switch", "cam_track_switch", 25);
    ASSERT_NE(inst, nullptr);
    ASSERT_EQ(inst->init(aivision::core::FramePool::instance().get_frame_ops(),
                         platform_adapter->get_c_image_ops()), AV_OK);

    inst->set_result_callback([&](const av_algo_result&, const av_frame_desc& frame) {
        last_width.store(frame.width);
        last_height.store(frame.height);
        if (frame.width == 1920 && frame.height == 1080) {
            count_1080p.fetch_add(1);
        } else if (frame.width == 1280 && frame.height == 720) {
            count_720p.fetch_add(1);
        }
    });

    task->add_instance(inst);
    ASSERT_EQ(task->start(), AV_OK);

    // Phase 1: Wait for 1080p frames
    for (int i = 0; i < 50; ++i) {
        if (count_1080p.load() >= 10) break;
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    EXPECT_GE(count_1080p.load(), 5U);

    // Switch stream to 720p by restarting RTSP server with 720p video
    stop_server();
    std::this_thread::sleep_for(std::chrono::milliseconds(1500));
    ASSERT_TRUE(start_server(h264_720p, "track_switch_test"));

    // Phase 2: Wait for 720p frames after reconnect and decoder re-init
    for (int i = 0; i < 60; ++i) {
        if (count_720p.load() >= 10) break;
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }

    EXPECT_GE(count_720p.load(), 5U);
    EXPECT_EQ(last_width.load(), 1280U);
    EXPECT_EQ(last_height.load(), 720U);
    task->stop();
}

TEST_F(RealMediaIntegrationTest, MultiInstanceSamplingAndSlowConsumerIsolation) {
    const fs::path h264_file = fixture_dir() / "test_1080p_h264.mp4";
    ASSERT_TRUE(start_server(h264_file, "multi_inst_test"));

    auto media_backend = aivision::media::create_zlm_backend();
    auto platform_adapter = std::make_shared<aivision::platform::MacosPlatformAdapter>();
    const std::string rtsp_url = "rtsp://127.0.0.1:" + std::to_string(port_) + "/live/multi_inst_test";
    auto task = std::make_shared<aivision::core::CameraTask>(
        "cam_multi_inst", rtsp_url, platform_adapter, media_backend);

    // Instance 1: High FPS (25 FPS), Fast consumer
    auto inst_fast = create_mock_instance("inst_fast", "cam_multi_inst", 25);
    ASSERT_NE(inst_fast, nullptr);
    ASSERT_EQ(inst_fast->init(aivision::core::FramePool::instance().get_frame_ops(),
                              platform_adapter->get_c_image_ops()), AV_OK);

    // Instance 2: Low FPS (5 FPS), Moderate consumer
    auto inst_low_fps = create_mock_instance("inst_low", "cam_multi_inst", 5);
    ASSERT_NE(inst_low_fps, nullptr);
    ASSERT_EQ(inst_low_fps->init(aivision::core::FramePool::instance().get_frame_ops(),
                                platform_adapter->get_c_image_ops()), AV_OK);

    // Instance 3: Slow consumer (simulating artificial delay in callback to cause queue backlog)
    auto inst_slow = create_mock_instance("inst_slow", "cam_multi_inst", 25);
    ASSERT_NE(inst_slow, nullptr);
    ASSERT_EQ(inst_slow->init(aivision::core::FramePool::instance().get_frame_ops(),
                              platform_adapter->get_c_image_ops()), AV_OK);

    inst_slow->set_result_callback([](const av_algo_result&, const av_frame_desc&) {
        // Artificially block/slow down this worker
        std::this_thread::sleep_for(std::chrono::milliseconds(150));
    });

    task->add_instance(inst_fast);
    task->add_instance(inst_low_fps);
    task->add_instance(inst_slow);

    ASSERT_EQ(task->start(), AV_OK);

    // Run for ~4 seconds
    for (int i = 0; i < 40; ++i) {
        if (inst_fast->get_processed_frames() >= 25 && inst_slow->get_dropped_frames() > 0) break;
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }

    const uint64_t decoded = task->get_decoded_frames();
    const uint64_t fast_processed = inst_fast->get_processed_frames();
    const uint64_t low_processed = inst_low_fps->get_processed_frames();
    const uint64_t slow_processed = inst_slow->get_processed_frames();
    const uint64_t slow_dropped = inst_slow->get_dropped_frames();

    // Fast instance should process significantly more frames than low FPS instance
    EXPECT_GE(decoded, 20U);
    EXPECT_GE(fast_processed, 15U);
    EXPECT_GE(low_processed, 3U);
    EXPECT_LT(low_processed, fast_processed);

    // Slow instance drops frames due to bounded queue overflow, but fast instance is NOT blocked
    EXPECT_GE(slow_dropped, 1U);
    EXPECT_GT(fast_processed, slow_processed);

    // Ensure frame pool has not leaked frames during drop & processing
    task->stop();
    std::this_thread::sleep_for(std::chrono::milliseconds(200));
    EXPECT_EQ(aivision::core::FramePool::instance().active_frame_count(), 0U);
}

TEST_F(RealMediaIntegrationTest, LongContinuousStreamingStability) {
    const fs::path h264_file = fixture_dir() / "test_1080p_h264.mp4";
    ASSERT_TRUE(start_server(h264_file, "long_stream_test"));

    auto media_backend = aivision::media::create_zlm_backend();
    auto platform_adapter = std::make_shared<aivision::platform::MacosPlatformAdapter>();
    const std::string rtsp_url = "rtsp://127.0.0.1:" + std::to_string(port_) + "/live/long_stream_test";
    auto task = std::make_shared<aivision::core::CameraTask>(
        "cam_long_stream", rtsp_url, platform_adapter, media_backend);
    auto inst = std::make_shared<aivision::core::AlgorithmInstance>(
        "inst_long", "cam_long_stream", "mock_algo", "1.0.0", 25, "{}", nullptr, nullptr);
    ASSERT_EQ(inst->init(aivision::core::FramePool::instance().get_frame_ops(),
                         platform_adapter->get_c_image_ops()), AV_OK);

    task->add_instance(inst);
    ASSERT_EQ(task->start(), AV_OK);

    // Allow environment variable to configure duration; default to 10 seconds for unit test run,
    // or 60+ seconds when AIVISION_EXTENDED_MEDIA_TESTS is set.
    const char* ext_env = std::getenv("AIVISION_EXTENDED_MEDIA_TESTS");
    const int duration_sec = (ext_env != nullptr && std::string(ext_env) == "1") ? 60 : 8;

    const auto start_time = std::chrono::steady_clock::now();
    uint64_t prev_decoded = 0;
    while (std::chrono::duration_cast<std::chrono::seconds>(
               std::chrono::steady_clock::now() - start_time).count() < duration_sec) {
        std::this_thread::sleep_for(std::chrono::seconds(1));
        const uint64_t current_decoded = task->get_decoded_frames();
        EXPECT_GE(current_decoded, prev_decoded);
        prev_decoded = current_decoded;
    }

    EXPECT_GT(task->get_decoded_frames(), 30U);
    EXPECT_GT(inst->get_processed_frames(), 30U);

    task->stop();
    std::this_thread::sleep_for(std::chrono::milliseconds(200));
    EXPECT_EQ(aivision::core::FramePool::instance().active_frame_count(), 0U);
}

#endif
