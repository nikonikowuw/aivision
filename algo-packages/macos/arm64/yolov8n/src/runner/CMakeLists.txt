add_executable(run_local standalone_runner.mm)
set_target_properties(run_local PROPERTIES RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}")
target_compile_options(run_local PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
target_include_directories(run_local PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/aivision-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/aivision-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
target_link_libraries(run_local PRIVATE
    yolov8n
    yolov8n_preprocess
    yolov8n_inference
    yolov8n_postprocess
    aivision_sdk_headers
    "-framework Foundation"
    "-framework CoreVideo"
    "-framework CoreGraphics"
    "-framework ImageIO"
    "-framework Accelerate"
    "-framework CoreText"
)