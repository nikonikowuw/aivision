#include <cerrno>
#include <chrono>
#include <csignal>
#include <cstdlib>
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

#endif
