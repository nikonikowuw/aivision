add_library(yolov8n_preprocess STATIC preprocessor.mm)
set_target_properties(yolov8n_preprocess PROPERTIES POSITION_INDEPENDENT_CODE ON)
target_compile_options(yolov8n_preprocess PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
target_include_directories(yolov8n_preprocess PUBLIC
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
target_link_libraries(yolov8n_preprocess PUBLIC
    argus_sdk_headers
    "-framework Foundation"
    "-framework CoreVideo"
    "-framework Accelerate"
)
