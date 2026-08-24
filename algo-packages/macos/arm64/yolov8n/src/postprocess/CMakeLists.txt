add_library(yolov8n_postprocess STATIC postprocessor.cpp)
set_target_properties(yolov8n_postprocess PROPERTIES POSITION_INDEPENDENT_CODE ON)
target_compile_options(yolov8n_postprocess PRIVATE ${ALGO_WARNING_FLAGS})
target_include_directories(yolov8n_postprocess PUBLIC
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/aivision-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/aivision-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
target_link_libraries(yolov8n_postprocess PUBLIC aivision_sdk_headers)
