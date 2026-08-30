add_library(yolov8n_postprocess STATIC
    postprocessor.cpp
)

target_include_directories(yolov8n_postprocess PUBLIC
    ${CMAKE_CURRENT_SOURCE_DIR}
)

target_link_libraries(yolov8n_postprocess PUBLIC
    argus_sdk_headers
)
