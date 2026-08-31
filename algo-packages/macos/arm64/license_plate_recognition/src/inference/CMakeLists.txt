add_library(lpr_inference OBJECT
    model_inference.mm
)
target_compile_options(lpr_inference PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
target_include_directories(lpr_inference PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
target_link_libraries(lpr_inference PRIVATE
    "-framework Foundation"
    "-framework CoreML"
)
