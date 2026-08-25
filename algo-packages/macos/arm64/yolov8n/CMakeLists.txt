cmake_minimum_required(VERSION 3.24)

# This package is a macOS Apple Silicon artifact. Keep the build contract
# independent from the host architecture and SDK selected by the developer.
if(DEFINED CMAKE_OSX_ARCHITECTURES AND NOT "${CMAKE_OSX_ARCHITECTURES}" STREQUAL "" AND
   NOT "${CMAKE_OSX_ARCHITECTURES}" STREQUAL "arm64")
    message(FATAL_ERROR "yolov8n only supports CMAKE_OSX_ARCHITECTURES=arm64")
endif()
if(DEFINED CMAKE_OSX_DEPLOYMENT_TARGET AND NOT "${CMAKE_OSX_DEPLOYMENT_TARGET}" STREQUAL "" AND
   NOT "${CMAKE_OSX_DEPLOYMENT_TARGET}" STREQUAL "14.0")
    message(FATAL_ERROR "yolov8n requires CMAKE_OSX_DEPLOYMENT_TARGET=14.0")
endif()
set(CMAKE_OSX_ARCHITECTURES "arm64" CACHE STRING "yolov8n target architecture" FORCE)
set(CMAKE_OSX_DEPLOYMENT_TARGET "14.0" CACHE STRING "yolov8n minimum macOS version" FORCE)

project(yolov8n_algo VERSION 1.0.0 LANGUAGES C CXX OBJCXX)

if(NOT APPLE OR NOT CMAKE_OSX_ARCHITECTURES STREQUAL "arm64")
    message(FATAL_ERROR "yolov8n requires an Apple Silicon macOS build (arm64)")
endif()

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_OBJCXX_STANDARD 20)
set(CMAKE_OBJCXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_VISIBILITY_PRESET hidden)
set(CMAKE_C_VISIBILITY_PRESET hidden)
set(CMAKE_OBJCXX_VISIBILITY_PRESET hidden)
set(CMAKE_VISIBILITY_INLINES_HIDDEN ON)

set(ALGO_WARNING_FLAGS -Wall -Wextra -Wpedantic -Werror)

list(APPEND CMAKE_PREFIX_PATH "${CMAKE_CURRENT_SOURCE_DIR}/vendor/aivision-sdk/cmake")
find_package(AivisionAlgoSDK REQUIRED)

add_subdirectory(src)

# The production dylib contains only the C ABI object; implementation modules
# remain separate targets so they can be rebuilt independently.
aivision_add_algo_package(yolov8n
    ALGORITHM_ID yolov8n
    VERSION 1.0.0
    PLATFORM_ID macos-arm64-coreml
    SRCS "$<TARGET_OBJECTS:yolov8n_core>"
    TARGET_DEPS
        yolov8n_preprocess
        yolov8n_inference
        yolov8n_postprocess
    DEPS
        "-framework Foundation"
        "-framework CoreVideo"
        "-framework CoreML"
        "-framework Accelerate"
)
target_compile_options(yolov8n PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
target_include_directories(yolov8n PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/vendor/aivision-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/vendor/aivision-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/src"
)
if(APPLE)
    file(GENERATE OUTPUT "${CMAKE_CURRENT_BINARY_DIR}/yolov8n.exports"
         CONTENT "_av_algo_get_abi\n")
    target_link_options(yolov8n PRIVATE
        "-Wl,-exported_symbols_list,${CMAKE_CURRENT_BINARY_DIR}/yolov8n.exports")
endif()

add_subdirectory(src/runner)

include(CTest)
if(BUILD_TESTING)
    add_executable(yolov8n_tests tests/package_tests.cpp)
    target_compile_options(yolov8n_tests PRIVATE ${ALGO_WARNING_FLAGS})
    target_include_directories(yolov8n_tests PRIVATE
        "${CMAKE_CURRENT_SOURCE_DIR}/vendor/aivision-sdk/include"
        "${CMAKE_CURRENT_SOURCE_DIR}/vendor/aivision-sdk/toolkit/include"
        "${CMAKE_CURRENT_SOURCE_DIR}/src"
    )
    target_link_libraries(yolov8n_tests PRIVATE
        yolov8n_postprocess
        aivision_sdk_headers
    )
    add_test(NAME yolov8n_tests COMMAND yolov8n_tests)

    add_executable(yolov8n_preprocess_tests tests/preprocess_tests.mm)
    target_compile_options(yolov8n_preprocess_tests PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
    target_include_directories(yolov8n_preprocess_tests PRIVATE
        "${CMAKE_CURRENT_SOURCE_DIR}/vendor/aivision-sdk/include"
        "${CMAKE_CURRENT_SOURCE_DIR}/vendor/aivision-sdk/toolkit/include"
        "${CMAKE_CURRENT_SOURCE_DIR}/src"
    )
    target_link_libraries(yolov8n_preprocess_tests PRIVATE
        yolov8n_preprocess
        aivision_sdk_headers
        "-framework Foundation"
        "-framework CoreVideo"
        "-framework Accelerate"
    )
    add_test(NAME yolov8n_preprocess_tests COMMAND yolov8n_preprocess_tests)

    add_executable(yolov8n_manifest_loader_tests
        tests/manifest_loader_tests.mm
        src/core/manifest_loader.mm)
    target_compile_options(yolov8n_manifest_loader_tests PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
    target_include_directories(yolov8n_manifest_loader_tests PRIVATE
        "${CMAKE_CURRENT_SOURCE_DIR}/vendor/aivision-sdk/include"
        "${CMAKE_CURRENT_SOURCE_DIR}/vendor/aivision-sdk/toolkit/include"
        "${CMAKE_CURRENT_SOURCE_DIR}/src")
    target_link_libraries(yolov8n_manifest_loader_tests PRIVATE "-framework Foundation")
    add_test(NAME yolov8n_manifest_loader_tests COMMAND yolov8n_manifest_loader_tests)

    add_executable(yolov8n_abi_tests tests/abi_tests.cpp)
    target_compile_options(yolov8n_abi_tests PRIVATE ${ALGO_WARNING_FLAGS})
    target_compile_definitions(yolov8n_abi_tests PRIVATE
        YOLOV8N_PACKAGE_ROOT="${CMAKE_CURRENT_SOURCE_DIR}"
    )
    target_include_directories(yolov8n_abi_tests PRIVATE
        "${CMAKE_CURRENT_SOURCE_DIR}/vendor/aivision-sdk/include"
    )
    target_link_libraries(yolov8n_abi_tests PRIVATE
        yolov8n
        aivision_sdk_headers
        "-framework Foundation"
        "-framework CoreML"
        "-framework CoreVideo"
        "-framework Accelerate"
    )
    add_test(NAME yolov8n_abi_tests COMMAND yolov8n_abi_tests)
endif()
