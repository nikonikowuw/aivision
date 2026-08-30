add_library(yolov8n_inference STATIC
    rknn_runner.cpp
)

target_include_directories(yolov8n_inference PUBLIC
    ${CMAKE_CURRENT_SOURCE_DIR}
)

target_link_libraries(yolov8n_inference PUBLIC
    argus_sdk_headers
    yolov8n_postprocess
)

if(TARGET rknnrt)
    target_link_libraries(yolov8n_inference PUBLIC rknnrt)
    target_compile_definitions(yolov8n_inference PUBLIC HAVE_RKNNRT=1)
endif()
