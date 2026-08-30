# Main engine application executable
add_executable(argus-engine
    main.cpp
)
set_target_properties(argus-engine PROPERTIES
    RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}"
)
target_include_directories(argus-engine PRIVATE
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../include>
)
target_link_libraries(argus-engine PRIVATE
    engine_core
)
argus_enable_strict_warnings(argus-engine)
if(ARGUS_USE_ZLM)
    target_link_libraries(argus-engine PRIVATE media_zlm)
else()
    target_link_libraries(argus-engine PRIVATE media_mock)
endif()
if(APPLE AND PLATFORM_TARGET STREQUAL "macos")
    target_compile_definitions(argus-engine PRIVATE ARGUS_PLATFORM_MACOS=1)
    target_link_libraries(argus-engine PRIVATE platform_macos)
else()
    target_link_libraries(argus-engine PRIVATE platform_mock)
endif()
