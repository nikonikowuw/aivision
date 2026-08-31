# ArgusAlgoSDKConfig.cmake
cmake_minimum_required(VERSION 3.24)

get_filename_component(ARGUS_SDK_ROOT "${CMAKE_CURRENT_LIST_DIR}/.." ABSOLUTE)

# 1. Pure C ABI headers target
if(NOT TARGET argus_sdk_abi)
    add_library(argus_sdk_abi INTERFACE)
    target_include_directories(argus_sdk_abi INTERFACE
        $<BUILD_INTERFACE:${ARGUS_SDK_ROOT}/include>
    )
endif()

# 2. C++ Toolkit headers target
if(NOT TARGET argus_sdk_toolkit)
    add_library(argus_sdk_toolkit INTERFACE)
    target_include_directories(argus_sdk_toolkit INTERFACE
        $<BUILD_INTERFACE:${ARGUS_SDK_ROOT}/toolkit/include>
    )
    target_link_libraries(argus_sdk_toolkit INTERFACE argus_sdk_abi)
endif()

# Backwards compatibility alias
if(NOT TARGET argus_sdk_headers)
    add_library(argus_sdk_headers INTERFACE)
    target_link_libraries(argus_sdk_headers INTERFACE argus_sdk_toolkit)
endif()

include("${CMAKE_CURRENT_LIST_DIR}/ArgusAlgoPackage.cmake")
