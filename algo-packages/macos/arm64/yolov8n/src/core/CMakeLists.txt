add_library(yolov8n_core OBJECT algo_entry.cpp manifest_loader.mm)
set_target_properties(yolov8n_core PROPERTIES POSITION_INDEPENDENT_CODE ON)
target_compile_options(yolov8n_core PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
target_include_directories(yolov8n_core PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
target_link_libraries(yolov8n_core PRIVATE
    argus_sdk_headers
    yolov8n_preprocess
    yolov8n_inference
    yolov8n_postprocess
)
