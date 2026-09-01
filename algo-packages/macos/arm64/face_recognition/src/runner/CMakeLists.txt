add_executable(face_recognition_runner
    standalone_runner.mm
    $<TARGET_OBJECTS:face_recognition_preprocess>
)
target_compile_options(face_recognition_runner PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
target_include_directories(face_recognition_runner PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
    "${CMAKE_CURRENT_SOURCE_DIR}/third_party"
)
target_link_libraries(face_recognition_runner PRIVATE
    ${CMAKE_DL_LIBS}
    "-framework Foundation"
    "-framework CoreGraphics"
    "-framework ImageIO"
    "-framework CoreVideo"
    "-framework Accelerate"
)
