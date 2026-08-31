add_library(lpr_preprocess OBJECT
    preprocessor.mm
)
target_compile_options(lpr_preprocess PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
target_include_directories(lpr_preprocess PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
target_link_libraries(lpr_preprocess PRIVATE
    "-framework Foundation"
    "-framework CoreVideo"
    "-framework Accelerate"
)
