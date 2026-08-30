find_library(RGA_LIB NAMES rga librga PATHS /usr/lib /usr/lib64 /lib /usr/lib/aarch64-linux-gnu /lib/aarch64-linux-gnu)

add_library(yolov8n_preprocess STATIC
    preprocessor.cpp
)

target_include_directories(yolov8n_preprocess PUBLIC
    ${CMAKE_CURRENT_SOURCE_DIR}
)

target_link_libraries(yolov8n_preprocess PUBLIC
    argus_sdk_headers
)

if(RGA_LIB)
    target_link_libraries(yolov8n_preprocess PUBLIC ${RGA_LIB})
    target_compile_definitions(yolov8n_preprocess PRIVATE HAVE_RGA=1)
endif()

