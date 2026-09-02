# Engine logging module
add_library(engine_logging STATIC
    logging/log_record.cpp
    logging/log_sanitizer.cpp
    logging/log_sink.cpp
    logging/log_writer.cpp
    logging/log_context.cpp
    logging/logger.cpp
)
target_include_directories(engine_logging PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../include>
)
target_link_libraries(engine_logging PUBLIC Threads::Threads)
argus_enable_strict_warnings(engine_logging)

# Engine core module
add_library(engine_core STATIC
    frame/frame_pool.cpp
    frame/color_vui.cpp
    task/camera_task.cpp
    task/task_scheduler.cpp
    camera/probe_rtsp.cpp
    camera/live_stream_manager.cpp
    algo/algo_manager.cpp
    algo/algo_instance.cpp
    algo/algo_sandbox.cpp
    algo/motion_gate.cpp
    algo/face_gallery.cpp
    image/image_manager.cpp
    resource/resource_ledger.cpp
    telemetry/telemetry_collector.cpp
    ipc/uds_client.cpp
    ipc/uds_server.cpp
    ../platform/platform_registry.cpp
)
target_include_directories(engine_core PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../include>
    $<BUILD_INTERFACE:${CMAKE_BINARY_DIR}/generated>
)
if(ARGUS_USE_ZLM)
    target_include_directories(engine_core SYSTEM PUBLIC
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../third_party/ZLMediaKit/src>
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../third_party/ZLMediaKit/3rdpart/ZLToolKit/src>
    )
endif()
target_link_libraries(engine_core PUBLIC
    engine_logging
    platform_api
    media_api
    proto_gen
    nlohmann_json::nlohmann_json
    ${CMAKE_DL_LIBS}
)
argus_enable_strict_warnings(engine_core)
