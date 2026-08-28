add_library(face_recognition_postprocess OBJECT
    postprocessor.cpp
)
target_compile_options(face_recognition_postprocess PRIVATE ${ALGO_WARNING_FLAGS})
target_include_directories(face_recognition_postprocess PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/aivision-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/aivision-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
