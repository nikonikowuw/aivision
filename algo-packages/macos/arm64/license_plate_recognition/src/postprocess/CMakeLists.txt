add_library(lpr_postprocess OBJECT
    postprocessor.cpp
)
target_compile_options(lpr_postprocess PRIVATE ${ALGO_WARNING_FLAGS})
target_include_directories(lpr_postprocess PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
