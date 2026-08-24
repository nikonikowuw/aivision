add_library(yolov8n_core OBJECT
    algo_entry.cpp
)

target_include_directories(yolov8n_core PUBLIC
    ${CMAKE_CURRENT_SOURCE_DIR}
)

target_link_libraries(yolov8n_core PUBLIC
    aivision_sdk_headers
    yolov8n_preprocess
    yolov8n_inference
    yolov8n_postprocess
)
