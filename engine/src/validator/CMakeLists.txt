# Package validator executable
add_executable(package_validator
    validator_main.cpp
    ../core/algo/algo_sandbox.cpp
)
set_target_properties(package_validator PROPERTIES
    RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}"
)
if(APPLE AND PLATFORM_TARGET STREQUAL "macos")
    target_sources(package_validator PRIVATE macos_test_frame.mm)
    target_compile_definitions(package_validator PRIVATE AIVISION_PLATFORM_MACOS=1)
endif()
target_include_directories(package_validator PRIVATE
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../include>
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../../sdk/include>
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
