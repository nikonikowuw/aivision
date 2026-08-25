cmake_minimum_required(VERSION 3.24)

set(MOCK_FIXTURE_DIR "${CMAKE_CURRENT_BINARY_DIR}/fixtures/packages/mock_pkg")
set(MOCK_FIXTURE_LIB_DIR "${MOCK_FIXTURE_DIR}/lib")
add_library(mock_algo_fixture SHARED
    fixtures/packages/mock_pkg/mock_algo.c
)
set_target_properties(mock_algo_fixture PROPERTIES
    PREFIX "lib"
    OUTPUT_NAME "mock-detector"
    SUFFIX ".dylib"
    LIBRARY_OUTPUT_DIRECTORY "${MOCK_FIXTURE_LIB_DIR}"
    RUNTIME_OUTPUT_DIRECTORY "${MOCK_FIXTURE_LIB_DIR}"
    ARCHIVE_OUTPUT_DIRECTORY "${MOCK_FIXTURE_LIB_DIR}"
    C_VISIBILITY_PRESET hidden
    VISIBILITY_INLINES_HIDDEN YES
)
add_custom_command(TARGET mock_algo_fixture POST_BUILD
    COMMAND ${CMAKE_COMMAND} -E make_directory "${MOCK_FIXTURE_DIR}"
    COMMAND ${CMAKE_COMMAND} -E copy "${CMAKE_CURRENT_SOURCE_DIR}/fixtures/packages/mock_pkg/config.schema.json" "${MOCK_FIXTURE_DIR}/config.schema.json"
    COMMAND ${CMAKE_COMMAND} -E copy "${CMAKE_CURRENT_SOURCE_DIR}/fixtures/packages/mock_pkg/testimage.jpg" "${MOCK_FIXTURE_DIR}/testimage.jpg"
    COMMAND ${CMAKE_COMMAND}
        -DPACKAGE_DIR=${MOCK_FIXTURE_DIR}
        -DSOURCE_MANIFEST=${CMAKE_CURRENT_SOURCE_DIR}/fixtures/packages/mock_pkg/manifest.json
        -P "${CMAKE_CURRENT_SOURCE_DIR}/fixtures/packages/mock_pkg/update_manifest.cmake"
)
add_custom_target(mock_algo_fixture_assets
    COMMAND ${CMAKE_COMMAND} -E make_directory "${MOCK_FIXTURE_DIR}"
    COMMAND ${CMAKE_COMMAND} -E copy "${CMAKE_CURRENT_SOURCE_DIR}/fixtures/packages/mock_pkg/config.schema.json" "${MOCK_FIXTURE_DIR}/config.schema.json"
    COMMAND ${CMAKE_COMMAND} -E copy "${CMAKE_CURRENT_SOURCE_DIR}/fixtures/packages/mock_pkg/testimage.jpg" "${MOCK_FIXTURE_DIR}/testimage.jpg"
    COMMAND ${CMAKE_COMMAND}
        -DPACKAGE_DIR=${MOCK_FIXTURE_DIR}
        -DSOURCE_MANIFEST=${CMAKE_CURRENT_SOURCE_DIR}/fixtures/packages/mock_pkg/manifest.json
        -P "${CMAKE_CURRENT_SOURCE_DIR}/fixtures/packages/mock_pkg/update_manifest.cmake"
    DEPENDS mock_algo_fixture
            "${CMAKE_CURRENT_SOURCE_DIR}/fixtures/packages/mock_pkg/config.schema.json"
            "${CMAKE_CURRENT_SOURCE_DIR}/fixtures/packages/mock_pkg/manifest.json"
            "${CMAKE_CURRENT_SOURCE_DIR}/fixtures/packages/mock_pkg/testimage.jpg"
            "${CMAKE_CURRENT_SOURCE_DIR}/fixtures/packages/mock_pkg/update_manifest.cmake"
)
target_include_directories(mock_algo_fixture PRIVATE
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../sdk/include>
)

# Unit tests
add_executable(unit_tests
    unit/test_main.cpp
    unit/test_frame_pool.cpp
    unit/test_color_vui.cpp
    unit/test_resource_ledger.cpp
    unit/test_toolkit.cpp
    unit/test_algo_sandbox.cpp
    unit/test_camera_task.cpp
    unit/test_image_manager.cpp
    unit/test_real_media.cpp
)
target_include_directories(unit_tests PRIVATE
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../include>
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../sdk/include>
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../sdk/toolkit/include>
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../src>
)
target_compile_definitions(unit_tests PRIVATE
    AIVISION_FIXTURE_PACKAGE_DIR="${MOCK_FIXTURE_DIR}"
    AIVISION_PACKAGE_VALIDATOR_PATH="${CMAKE_BINARY_DIR}/package_validator"
    AIVISION_SOURCE_DIR="${CMAKE_CURRENT_SOURCE_DIR}/.."
    AIVISION_BINARY_DIR="${CMAKE_BINARY_DIR}"
)
target_link_libraries(unit_tests PRIVATE
    engine_core
    platform_mock
    GTest::gtest
    GTest::gtest_main
)
if(APPLE AND PLATFORM_TARGET STREQUAL "macos")
    target_compile_definitions(unit_tests PRIVATE AIVISION_PLATFORM_MACOS=1)
    target_link_libraries(unit_tests PRIVATE platform_macos)
endif()
if(AIVISION_USE_ZLM)
    target_compile_definitions(unit_tests PRIVATE AIVISION_USE_ZLM=1)
    target_link_libraries(unit_tests PRIVATE media_zlm)
endif()

if(ENGINE_ENABLE_ASAN OR ENGINE_ENABLE_TSAN)
    target_compile_definitions(unit_tests PRIVATE AIVISION_SKIP_IPC_TESTS=1)
elseif(PLATFORM_TARGET STREQUAL "mock")
    target_compile_definitions(unit_tests PRIVATE
        AIVISION_RUN_SANDBOX_CHILD_TEST=1
        AIVISION_RUN_PACKAGE_RPC_TEST=1
    )
endif()
add_dependencies(unit_tests mock_algo_fixture mock_algo_fixture_assets)
add_test(NAME unit_tests COMMAND unit_tests WORKING_DIRECTORY ${CMAKE_CURRENT_BINARY_DIR})
if(ENGINE_ENABLE_TSAN)
    # Keep real-media coverage; suppress only known ZLToolKit initialization races.
    set_tests_properties(unit_tests PROPERTIES
        ENVIRONMENT "TSAN_OPTIONS=suppressions=${CMAKE_CURRENT_SOURCE_DIR}/tsan.supp:halt_on_error=1"
    )
endif()

# Contract tests
add_executable(contract_tests
    contract/test_contract_mock.cpp
)
target_include_directories(contract_tests PRIVATE
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../include>
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../src>
)
target_link_libraries(contract_tests PRIVATE
    engine_core
    platform_mock
    GTest::gtest
    GTest::gtest_main
)
add_dependencies(contract_tests mock_algo_fixture mock_algo_fixture_assets)
add_test(NAME contract_tests COMMAND contract_tests WORKING_DIRECTORY ${CMAKE_CURRENT_BINARY_DIR})

if(AIVISION_USE_ZLM)
    add_executable(test_rtsp_server media/rtsp_server.cpp)
    target_include_directories(test_rtsp_server PRIVATE
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../include>
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../third_party/ZLMediaKit/src>
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../third_party/ZLMediaKit/3rdpart/ZLToolKit/src>
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../third_party/ZLMediaKit/3rdpart/media-server/libmov/include>
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../third_party/ZLMediaKit/3rdpart/media-server/libmpeg/include>
    )
    target_compile_definitions(test_rtsp_server PRIVATE ENABLE_MP4)
    target_link_libraries(test_rtsp_server PRIVATE
        ext-codec
    )
    add_dependencies(unit_tests test_rtsp_server)
endif()
