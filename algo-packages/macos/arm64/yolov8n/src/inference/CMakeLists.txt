add_library(yolov8n_inference STATIC coreml_runner.mm)
set_target_properties(yolov8n_inference PROPERTIES POSITION_INDEPENDENT_CODE ON)
target_compile_options(yolov8n_inference PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
target_include_directories(yolov8n_inference PUBLIC
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
target_link_libraries(yolov8n_inference PUBLIC
    argus_sdk_headers
    "-framework Foundation"
    "-framework CoreML"
    "-framework CoreVideo"
    "-framework Accelerate"
)
