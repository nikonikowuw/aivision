add_library(yolo26n_preprocess STATIC preprocessor.mm)
set_target_properties(yolo26n_preprocess PROPERTIES POSITION_INDEPENDENT_CODE ON)
target_compile_options(yolo26n_preprocess PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
target_include_directories(yolo26n_preprocess PUBLIC
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
target_link_libraries(yolo26n_preprocess PUBLIC
    argus_sdk_headers
    "-framework Foundation"
    "-framework CoreVideo"
    "-framework Accelerate"
)
