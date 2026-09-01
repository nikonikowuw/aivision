cmake_minimum_required(VERSION 3.24)
project(argus_algo_rk3576_yolov8n LANGUAGES C CXX)

set(CMAKE_C_STANDARD 11)
set(CMAKE_C_STANDARD_REQUIRED ON)
set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

set(CMAKE_RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}")
set(CMAKE_LIBRARY_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}")
set(CMAKE_ARCHIVE_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}")

list(APPEND CMAKE_MODULE_PATH "${CMAKE_CURRENT_SOURCE_DIR}/vendor/argus-sdk/cmake")
include(ArgusAlgoSDKConfig)
include(ArgusAlgoPackage)

find_library(RKNN_RT_LIB NAMES rknnrt librknnrt PATHS /usr/lib /usr/lib64 /lib /usr/lib/aarch64-linux-gnu /lib/aarch64-linux-gnu)

if(RKNN_RT_LIB)
    add_library(rknnrt UNKNOWN IMPORTED GLOBAL)
    set_target_properties(rknnrt PROPERTIES
        IMPORTED_LOCATION "${RKNN_RT_LIB}"
    )
endif()

add_subdirectory(src)

argus_add_algo_package(yolov8n
    ALGORITHM_ID general_detection
    VERSION 1.0.0
    PLATFORM_ID rk3576-rknn
    SRCS $<TARGET_OBJECTS:yolov8n_core>
    TARGET_DEPS yolov8n_preprocess yolov8n_inference yolov8n_postprocess
)

if(RKNN_RT_LIB)
    target_link_libraries(yolov8n PRIVATE ${RKNN_RT_LIB})
endif()

if(CMAKE_SYSTEM_NAME STREQUAL "Linux")
    file(GENERATE OUTPUT "${CMAKE_CURRENT_BINARY_DIR}/yolov8n.map"
         CONTENT "{ global: av_algo_get_abi; local: *; };\n")
    target_link_options(yolov8n PRIVATE
        "-Wl,--version-script=${CMAKE_CURRENT_BINARY_DIR}/yolov8n.map")
endif()

include(CTest)
option(BUILD_TESTING "Build unit tests" ON)
if(BUILD_TESTING)
    add_subdirectory(tests)
endif()
