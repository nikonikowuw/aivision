add_library(yolo26n_postprocess STATIC postprocessor.cpp)
set_target_properties(yolo26n_postprocess PROPERTIES POSITION_INDEPENDENT_CODE ON)
target_compile_options(yolo26n_postprocess PRIVATE ${ALGO_WARNING_FLAGS})
target_include_directories(yolo26n_postprocess PUBLIC
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
target_link_libraries(yolo26n_postprocess PUBLIC argus_sdk_headers)
