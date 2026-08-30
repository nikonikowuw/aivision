add_library(face_recognition_core OBJECT
    algo_entry.cpp
)
target_compile_options(face_recognition_core PRIVATE ${ALGO_WARNING_FLAGS})
target_include_directories(face_recognition_core PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
