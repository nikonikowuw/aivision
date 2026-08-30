# Platform mock target
add_library(platform_mock STATIC
    mock/mock_platform.cpp
)
target_include_directories(platform_mock PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../include>
)
target_link_libraries(platform_mock PUBLIC platform_api)
argus_enable_strict_warnings(platform_mock)

# Platform macos target
if(APPLE AND PLATFORM_TARGET STREQUAL "macos")
    add_library(platform_macos STATIC
        macos/macos_platform.mm
        macos/macos_decoder.mm
        macos/macos_image_ops.mm
        macos/macos_telemetry.cpp
    )
    target_include_directories(platform_macos PUBLIC
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../include>
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
    argus_enable_strict_warnings(platform_macos)
endif()
