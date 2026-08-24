#include <atomic>
#include <chrono>
#include <csignal>
#include <cstdlib>
#include <exception>
#include <iostream>
#include <memory>
#include <string>
#include <thread>

#include "Common/config.h"
#include "Network/TcpServer.h"
#include "Record/MP4Reader.h"
#include "Rtsp/RtspSession.h"
#include "Util/logger.h"

using namespace mediakit;
using namespace toolkit;

namespace {

std::atomic<bool> running{true};

void handle_signal(int) {
    running.store(false, std::memory_order_relaxed);
}

} // namespace

int main(int argc, char* argv[]) {
    if (argc != 4) {
        std::cerr << "usage: test_rtsp_server <port> <mp4-file> <stream>\n";
        return 2;
    }

    const auto port = static_cast<uint16_t>(std::strtoul(argv[1], nullptr, 10));
    const std::string file_path = argv[2];
    const std::string stream = argv[3];
    if (port == 0 || file_path.empty() || stream.empty()) return 2;

    std::signal(SIGINT, handle_signal);
    std::signal(SIGTERM, handle_signal);
    Logger::Instance().add(std::make_shared<ConsoleChannel>());

    mINI::Instance()[Protocol::kEnableRtsp] = 1;
    mINI::Instance()[Protocol::kEnableMP4] = 0;
    mINI::Instance()[Record::kFileRepeat] = true;

    std::shared_ptr<MP4Reader> reader;
    auto poller = EventPollerPool::Instance().getPoller();
    try {
        const MediaTuple tuple{"__defaultVhost__", "live", stream, ""};
        std::exception_ptr setup_error;
        poller->sync([&] {
            try {
                reader = std::make_shared<MP4Reader>(tuple, file_path, poller);
                reader->startReadMP4(40, true, true);
            } catch (...) {
                setup_error = std::current_exception();
            }
        });
        if (setup_error) std::rethrow_exception(setup_error);
        if (!reader) throw std::runtime_error("MP4 reader setup did not complete");
    } catch (const std::exception& error) {
        std::cerr << "failed to start MP4 reader: " << error.what() << '\n';
        return 1;
    }

    std::shared_ptr<TcpServer> rtsp_server;
    try {
        std::exception_ptr server_error;
        poller->sync([&] {
            try {
                rtsp_server = std::make_shared<TcpServer>();
                rtsp_server->start<RtspSession>(port);
            } catch (...) {
                server_error = std::current_exception();
            }
        });
        if (server_error) std::rethrow_exception(server_error);
        if (!rtsp_server) throw std::runtime_error("RTSP server setup did not complete");
    } catch (const std::exception& error) {
        poller->sync([&] {
            reader->stopReadMP4();
            reader.reset();
        });
        std::cerr << "failed to start RTSP server: " << error.what() << '\n';
        return 1;
    }

    while (running.load(std::memory_order_relaxed)) {
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }

    poller->sync([&] {
        reader->stopReadMP4();
        reader.reset();
        rtsp_server.reset();
    });
    return 0;
}
