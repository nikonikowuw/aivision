add_library(lpr_core OBJECT
    algo_entry.cpp
)
target_compile_options(lpr_core PRIVATE ${ALGO_WARNING_FLAGS})
target_include_directories(lpr_core PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
