cmake_minimum_required(VERSION 3.24)

if(DEFINED CMAKE_OSX_ARCHITECTURES AND NOT "${CMAKE_OSX_ARCHITECTURES}" STREQUAL "" AND
   NOT "${CMAKE_OSX_ARCHITECTURES}" STREQUAL "arm64")
    message(FATAL_ERROR "face_recognition only supports CMAKE_OSX_ARCHITECTURES=arm64")
endif()
if(DEFINED CMAKE_OSX_DEPLOYMENT_TARGET AND NOT "${CMAKE_OSX_DEPLOYMENT_TARGET}" STREQUAL "" AND
   NOT "${CMAKE_OSX_DEPLOYMENT_TARGET}" STREQUAL "14.0")
    message(FATAL_ERROR "face_recognition requires CMAKE_OSX_DEPLOYMENT_TARGET=14.0")
endif()
set(CMAKE_OSX_ARCHITECTURES "arm64" CACHE STRING "face_recognition target architecture" FORCE)
set(CMAKE_OSX_DEPLOYMENT_TARGET "14.0" CACHE STRING "face_recognition minimum macOS version" FORCE)

project(face_recognition_algo VERSION 1.0.0 LANGUAGES C CXX OBJCXX)

if(NOT APPLE OR NOT CMAKE_OSX_ARCHITECTURES STREQUAL "arm64")
    message(FATAL_ERROR "face_recognition requires an Apple Silicon macOS build (arm64)")
endif()

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_OBJCXX_STANDARD 20)
set(CMAKE_OBJCXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_VISIBILITY_PRESET hidden)
set(CMAKE_C_VISIBILITY_PRESET hidden)
set(CMAKE_OBJCXX_VISIBILITY_PRESET hidden)
set(CMAKE_VISIBILITY_INLINES_HIDDEN ON)

if(ENABLE_ASAN)
    set(ALGO_WARNING_FLAGS ${ALGO_WARNING_FLAGS} -fsanitize=address,undefined -fno-omit-frame-pointer)
    add_link_options(-fsanitize=address,undefined)
endif()

list(APPEND CMAKE_PREFIX_PATH "${CMAKE_CURRENT_SOURCE_DIR}/vendor/argus-sdk/cmake")
find_package(ArgusAlgoSDK REQUIRED)

add_subdirectory(src)

argus_add_algo_package(face_recognition
    ALGORITHM_ID face_recognition
    VERSION 1.0.0
    PLATFORM_ID macos-arm64-coreml
    SRCS "$<TARGET_OBJECTS:face_recognition_core>"
    TARGET_DEPS
        face_recognition_preprocess
        face_recognition_inference
        face_recognition_postprocess
    DEPS
        "-framework Foundation"
        "-framework CoreVideo"
        "-framework CoreML"
        "-framework Accelerate"
)
target_compile_options(face_recognition PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
target_include_directories(face_recognition PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/src"
)
if(APPLE)
    file(GENERATE OUTPUT "${CMAKE_CURRENT_BINARY_DIR}/face_recognition.exports"
         CONTENT "_av_algo_get_abi\n")
    target_link_options(face_recognition PRIVATE
        "-Wl,-exported_symbols_list,${CMAKE_CURRENT_BINARY_DIR}/face_recognition.exports")
endif()

add_subdirectory(src/runner)

include(CTest)
if(BUILD_TESTING)
    add_executable(face_recognition_tests tests/package_tests.cpp)
    target_compile_options(face_recognition_tests PRIVATE ${ALGO_WARNING_FLAGS})
    target_include_directories(face_recognition_tests PRIVATE
        "${CMAKE_CURRENT_SOURCE_DIR}/vendor/argus-sdk/include"
        "${CMAKE_CURRENT_SOURCE_DIR}/vendor/argus-sdk/toolkit/include"
        "${CMAKE_CURRENT_SOURCE_DIR}/src"
    )
    target_link_libraries(face_recognition_tests PRIVATE
        face_recognition_postprocess
        argus_sdk_headers
    )
    add_test(NAME face_recognition_tests COMMAND face_recognition_tests)

    add_executable(face_recognition_preprocess_tests tests/preprocess_tests.mm)
    target_compile_options(face_recognition_preprocess_tests PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
    target_include_directories(face_recognition_preprocess_tests PRIVATE
        "${CMAKE_CURRENT_SOURCE_DIR}/vendor/argus-sdk/include"
        "${CMAKE_CURRENT_SOURCE_DIR}/vendor/argus-sdk/toolkit/include"
        "${CMAKE_CURRENT_SOURCE_DIR}/src"
    )
    target_link_libraries(face_recognition_preprocess_tests PRIVATE
        face_recognition_preprocess
        argus_sdk_headers
        "-framework Foundation"
        "-framework CoreVideo"
        "-framework Accelerate"
    )
    add_test(NAME face_recognition_preprocess_tests COMMAND face_recognition_preprocess_tests)

    add_executable(face_recognition_abi_tests tests/abi_tests.cpp)
    target_compile_options(face_recognition_abi_tests PRIVATE ${ALGO_WARNING_FLAGS})
    target_compile_definitions(face_recognition_abi_tests PRIVATE
        FACE_RECOGNITION_PACKAGE_ROOT="${CMAKE_CURRENT_SOURCE_DIR}"
        FACE_RECOGNITION_DYLIB_TARGET="$<TARGET_FILE:face_recognition>"
    )
    target_include_directories(face_recognition_abi_tests PRIVATE
        "${CMAKE_CURRENT_SOURCE_DIR}/vendor/argus-sdk/include"
    )
    target_link_libraries(face_recognition_abi_tests PRIVATE
        ${CMAKE_DL_LIBS}
    )
    add_test(NAME face_recognition_abi_tests COMMAND face_recognition_abi_tests)
endif()
