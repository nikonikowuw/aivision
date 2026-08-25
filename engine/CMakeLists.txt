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

# Compiler launcher cache (ccache) support
find_program(CCACHE_PROGRAM ccache)
if(CCACHE_PROGRAM)
    message(STATUS "Found ccache: ${CCACHE_PROGRAM}")
    set(CMAKE_C_COMPILER_LAUNCHER ${CCACHE_PROGRAM})
    set(CMAKE_CXX_COMPILER_LAUNCHER ${CCACHE_PROGRAM})
    set(CMAKE_OBJCXX_COMPILER_LAUNCHER ${CCACHE_PROGRAM})
endif()

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

    include_directories(SYSTEM ${CMAKE_CURRENT_SOURCE_DIR}/third_party/ZLMediaKit/3rdpart/media-server/libflv/include)
    add_subdirectory(third_party/ZLMediaKit zlm_build EXCLUDE_FROM_ALL)

    # ZLMediaKit vendored headers are compiled under our strict -Werror flags on
    # consuming targets, so re-export their include dirs as SYSTEM to silence
    # warnings from third-party code.
    foreach(_zlm_target IN ITEMS zlmediakit ZLToolKit ext-codec jsoncpp flv mov mpeg)
        get_target_property(_zlm_incs ${_zlm_target} INTERFACE_INCLUDE_DIRECTORIES)
        if(_zlm_incs)
            set_target_properties(${_zlm_target} PROPERTIES
                INTERFACE_SYSTEM_INCLUDE_DIRECTORIES "${_zlm_incs}")
        endif()
    endforeach()
endif()

# Public API interfaces
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

# Subdirectories
add_subdirectory(proto)
add_subdirectory(src)

# Tests
if(ENGINE_BUILD_TESTS)
    add_subdirectory(tests)
endif()
