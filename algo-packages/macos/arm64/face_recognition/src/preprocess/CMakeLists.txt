add_library(face_recognition_preprocess OBJECT
    preprocessor.mm
)
target_compile_options(face_recognition_preprocess PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
target_include_directories(face_recognition_preprocess PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
)
target_link_libraries(face_recognition_preprocess PRIVATE
    "-framework Foundation"
    "-framework CoreVideo"
    "-framework VideoToolbox"
    "-framework Accelerate"
)
