add_executable(run_local standalone_runner.mm)
set_target_properties(run_local PROPERTIES RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}")
target_compile_options(run_local PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
target_include_directories(run_local PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
target_link_libraries(run_local PRIVATE
    yolo26n
    yolo26n_preprocess
    yolo26n_inference
    yolo26n_postprocess
    argus_sdk_headers
    "-framework Foundation"
    "-framework CoreVideo"
    "-framework CoreGraphics"
    "-framework ImageIO"
    "-framework Accelerate"
    "-framework CoreText"
)