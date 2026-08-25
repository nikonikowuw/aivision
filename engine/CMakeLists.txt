cmake_minimum_required(VERSION 3.24)
project(aivision_engine VERSION 1.0.0 LANGUAGES C CXX)

set(PLATFORM_TARGET "macos" CACHE STRING "Target platform (macos or mock)")
if(APPLE AND PLATFORM_TARGET STREQUAL "macos")
    enable_language(OBJCXX)
endif()

set(CMAKE_C_STANDARD 11)
set(CMAKE_C_STANDARD_REQUIRED ON)
set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

# Options
option(ENGINE_ENABLE_ASAN "Enable AddressSanitizer" OFF)
option(ENGINE_ENABLE_TSAN "Enable ThreadSanitizer" OFF)
option(ENGINE_BUILD_TESTS "Build test suite" ON)

function(aivision_enable_strict_warnings target)
    target_compile_options(${target} PRIVATE
        $<$<COMPILE_LANGUAGE:CXX>:-Wall>
        $<$<COMPILE_LANGUAGE:CXX>:-Wextra>
        $<$<COMPILE_LANGUAGE:CXX>:-Wpedantic>
        $<$<COMPILE_LANGUAGE:CXX>:-Werror>
        $<$<COMPILE_LANGUAGE:OBJCXX>:-Wall>
        $<$<COMPILE_LANGUAGE:OBJCXX>:-Wextra>
        $<$<COMPILE_LANGUAGE:OBJCXX>:-Wpedantic>
        $<$<COMPILE_LANGUAGE:OBJCXX>:-Werror>
    )
endfunction()

if(ENGINE_ENABLE_ASAN)
    add_compile_options(-fsanitize=address -fno-omit-frame-pointer)
    add_link_options(-fsanitize=address)
endif()

if(ENGINE_ENABLE_TSAN)
    add_compile_options(-fsanitize=thread)
    add_link_options(-fsanitize=thread)
endif()

# Dependencies
find_package(PkgConfig REQUIRED)
find_package(Threads REQUIRED)
find_package(Protobuf CONFIG REQUIRED)
find_package(gRPC CONFIG REQUIRED)
find_package(nlohmann_json CONFIG REQUIRED)

if(ENGINE_BUILD_TESTS)
    find_package(GTest CONFIG REQUIRED)
    include(GoogleTest)
    enable_testing()
endif()

# Platform detection

# ZLMediaKit configuration
set(AIVISION_USE_ZLM OFF)
if(APPLE AND PLATFORM_TARGET STREQUAL "macos")
    set(AIVISION_USE_ZLM ON)
    set(ENABLE_OPENSSL OFF CACHE BOOL "" FORCE)
    set(ENABLE_WEBRTC OFF CACHE BOOL "" FORCE)
    set(ENABLE_SRT OFF CACHE BOOL "" FORCE)
    set(ENABLE_FFMPEG OFF CACHE BOOL "" FORCE)
    set(ENABLE_MP4 ON CACHE BOOL "" FORCE)
    set(ENABLE_FAAC OFF CACHE BOOL "" FORCE)
    set(ENABLE_X264 OFF CACHE BOOL "" FORCE)
    set(ENABLE_API OFF CACHE BOOL "" FORCE)
    set(ENABLE_SERVER OFF CACHE BOOL "" FORCE)
    set(ENABLE_TESTS OFF CACHE BOOL "" FORCE)
    set(AIVISION_ZLM_OUTPUT_DIR "${CMAKE_CURRENT_BINARY_DIR}/zlm_release" CACHE PATH "ZLMediaKit output directory")

    include_directories(${CMAKE_CURRENT_SOURCE_DIR}/third_party/ZLMediaKit/3rdpart/media-server/libflv/include)
    add_subdirectory(third_party/ZLMediaKit zlm_build EXCLUDE_FROM_ALL)
endif()

# 1. sdk_abi_headers
add_library(sdk_abi_headers INTERFACE)
target_include_directories(sdk_abi_headers INTERFACE
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../sdk/include>
    $<INSTALL_INTERFACE:include>
)

# 2. platform_api
add_library(platform_api INTERFACE)
target_include_directories(platform_api INTERFACE
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
)
target_link_libraries(platform_api INTERFACE sdk_abi_headers)

# 3. media_api
add_library(media_api INTERFACE)
target_include_directories(media_api INTERFACE
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
)
target_link_libraries(media_api INTERFACE sdk_abi_headers)

# 4. Protobuf & gRPC codegen
set(PROTO_SRC_DIR ${CMAKE_CURRENT_SOURCE_DIR}/proto)
set(PROTO_FILES
    ${PROTO_SRC_DIR}/aivision/v1/common.proto
    ${PROTO_SRC_DIR}/aivision/v1/engine.proto
    ${PROTO_SRC_DIR}/aivision/v1/app.proto
    ${PROTO_SRC_DIR}/aivision/v1/person.proto
)
set(PROTO_GEN_DIR ${CMAKE_CURRENT_BINARY_DIR}/generated)
file(MAKE_DIRECTORY ${PROTO_GEN_DIR})

set(PROTO_SRCS
    ${PROTO_GEN_DIR}/aivision/v1/common.pb.cc
    ${PROTO_GEN_DIR}/aivision/v1/common.pb.h
    ${PROTO_GEN_DIR}/aivision/v1/engine.pb.cc
    ${PROTO_GEN_DIR}/aivision/v1/engine.pb.h
    ${PROTO_GEN_DIR}/aivision/v1/engine.grpc.pb.cc
    ${PROTO_GEN_DIR}/aivision/v1/engine.grpc.pb.h
    ${PROTO_GEN_DIR}/aivision/v1/app.pb.cc
    ${PROTO_GEN_DIR}/aivision/v1/app.pb.h
    ${PROTO_GEN_DIR}/aivision/v1/app.grpc.pb.cc
    ${PROTO_GEN_DIR}/aivision/v1/app.grpc.pb.h
    ${PROTO_GEN_DIR}/aivision/v1/person.pb.cc
    ${PROTO_GEN_DIR}/aivision/v1/person.pb.h
    ${PROTO_GEN_DIR}/aivision/v1/person.grpc.pb.cc
    ${PROTO_GEN_DIR}/aivision/v1/person.grpc.pb.h
)

find_program(GRPC_CPP_PLUGIN_EXECUTABLE grpc_cpp_plugin REQUIRED)

add_custom_command(
    OUTPUT ${PROTO_SRCS}
    COMMAND protobuf::protoc
        --proto_path=${PROTO_SRC_DIR}
        --cpp_out=${PROTO_GEN_DIR}
        --grpc_out=${PROTO_GEN_DIR}
        --plugin=protoc-gen-grpc=${GRPC_CPP_PLUGIN_EXECUTABLE}
        ${PROTO_FILES}
    DEPENDS ${PROTO_FILES}
    COMMENT "Generating gRPC/Protobuf C++ files"
)

add_library(proto_gen STATIC ${PROTO_SRCS})
target_include_directories(proto_gen PUBLIC
    $<BUILD_INTERFACE:${PROTO_GEN_DIR}>
)
target_link_libraries(proto_gen PUBLIC
    protobuf::libprotobuf
    gRPC::grpc++
)

# 5. engine_logging
add_library(engine_logging STATIC
    src/core/logging/log_record.cpp
    src/core/logging/log_sanitizer.cpp
    src/core/logging/log_sink.cpp
    src/core/logging/log_writer.cpp
    src/core/logging/log_context.cpp
    src/core/logging/logger.cpp
)
target_include_directories(engine_logging PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
)
target_link_libraries(engine_logging PUBLIC Threads::Threads)
aivision_enable_strict_warnings(engine_logging)

# 6. engine_core
add_library(engine_core STATIC
    src/core/frame/frame_pool.cpp
    src/core/frame/color_vui.cpp
    src/core/task/camera_task.cpp
    src/core/task/task_scheduler.cpp
    src/core/algo/algo_manager.cpp
    src/core/algo/algo_instance.cpp
    src/core/algo/algo_sandbox.cpp
    src/core/image/image_manager.cpp
    src/core/resource/resource_ledger.cpp
    src/core/telemetry/telemetry_collector.cpp
    src/core/ipc/uds_client.cpp
    src/core/ipc/uds_server.cpp
    src/platform/platform_registry.cpp
)
target_include_directories(engine_core PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<BUILD_INTERFACE:${PROTO_GEN_DIR}>
)
target_link_libraries(engine_core PUBLIC
    engine_logging
    platform_api
    media_api
    proto_gen
    nlohmann_json::nlohmann_json
    ${CMAKE_DL_LIBS}
)
aivision_enable_strict_warnings(engine_core)

# 6. media backend
if(AIVISION_USE_ZLM)
    add_library(media_zlm STATIC
        src/media/zlm/zlm_source.cpp
    )
    target_include_directories(media_zlm PUBLIC
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/third_party/ZLMediaKit/src>
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/third_party/ZLMediaKit/3rdpart/ZLToolKit/src>
    )
    target_link_libraries(media_zlm PUBLIC
        media_api
        zlmediakit
        ZLToolKit
        ext-codec
    )
    aivision_enable_strict_warnings(media_zlm)
else()
    add_library(media_mock STATIC
        src/media/mock/mock_source.cpp
    )
    target_include_directories(media_mock PUBLIC
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    )
    target_link_libraries(media_mock PUBLIC media_api)
    aivision_enable_strict_warnings(media_mock)
endif()

# 7. platform_mock
add_library(platform_mock STATIC
    src/platform/mock/mock_platform.cpp
)
target_include_directories(platform_mock PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
)
target_link_libraries(platform_mock PUBLIC platform_api)
aivision_enable_strict_warnings(platform_mock)

# 8. platform_macos
if(APPLE AND PLATFORM_TARGET STREQUAL "macos")
    add_library(platform_macos STATIC
        src/platform/macos/macos_platform.mm
        src/platform/macos/macos_decoder.mm
        src/platform/macos/macos_image_ops.mm
        src/platform/macos/macos_telemetry.cpp
    )
    target_include_directories(platform_macos PUBLIC
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    )
    target_link_libraries(platform_macos PUBLIC
        platform_api
        "-framework VideoToolbox"
        "-framework CoreVideo"
        "-framework CoreMedia"
        "-framework Accelerate"
        "-framework ImageIO"
        "-framework CoreGraphics"
        "-framework CoreImage"
        "-framework Foundation"
        "-framework IOKit"
    )
    aivision_enable_strict_warnings(platform_macos)
endif()

# 9. package_validator
add_executable(package_validator
    src/validator/validator_main.cpp
    src/core/algo/algo_sandbox.cpp
)
if(APPLE AND PLATFORM_TARGET STREQUAL "macos")
    target_sources(package_validator PRIVATE src/validator/macos_test_frame.mm)
    target_compile_definitions(package_validator PRIVATE AIVISION_PLATFORM_MACOS=1)
endif()
target_include_directories(package_validator PRIVATE
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../sdk/include>
)
target_link_libraries(package_validator PRIVATE
    engine_logging
    nlohmann_json::nlohmann_json
    ${CMAKE_DL_LIBS}
)
aivision_enable_strict_warnings(package_validator)
if(APPLE AND PLATFORM_TARGET STREQUAL "macos")
    target_link_libraries(package_validator PRIVATE
        "-framework CoreVideo"
        "-framework CoreGraphics"
        "-framework ImageIO"
        "-framework Foundation"
    )
endif()

# 10. engine_app
add_executable(aivision-engine
    src/app/main.cpp
)
target_include_directories(aivision-engine PRIVATE
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
)
target_link_libraries(aivision-engine PRIVATE
    engine_core
)
aivision_enable_strict_warnings(aivision-engine)
if(AIVISION_USE_ZLM)
    target_link_libraries(aivision-engine PRIVATE media_zlm)
else()
    target_link_libraries(aivision-engine PRIVATE media_mock)
endif()
if(APPLE AND PLATFORM_TARGET STREQUAL "macos")
    target_compile_definitions(aivision-engine PRIVATE AIVISION_PLATFORM_MACOS=1)
    target_link_libraries(aivision-engine PRIVATE platform_macos)
else()
    target_link_libraries(aivision-engine PRIVATE platform_mock)
endif()

# Tests
if(ENGINE_BUILD_TESTS)
    add_subdirectory(tests)
endif()
