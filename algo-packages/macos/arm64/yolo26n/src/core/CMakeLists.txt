add_library(yolo26n_core OBJECT algo_entry.cpp manifest_loader.mm)
set_target_properties(yolo26n_core PROPERTIES POSITION_INDEPENDENT_CODE ON)
target_compile_options(yolo26n_core PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
target_include_directories(yolo26n_core PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
target_link_libraries(yolo26n_core PRIVATE
    argus_sdk_headers
    yolo26n_preprocess
    yolo26n_inference
    yolo26n_postprocess
)
