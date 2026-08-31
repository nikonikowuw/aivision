add_executable(license_plate_recognition_runner
    standalone_runner.mm
)
target_compile_options(license_plate_recognition_runner PRIVATE ${ALGO_WARNING_FLAGS} -fobjc-arc)
target_include_directories(license_plate_recognition_runner PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/../../vendor/argus-sdk/toolkit/include"
    "${CMAKE_CURRENT_SOURCE_DIR}/.."
    "${CMAKE_CURRENT_SOURCE_DIR}/third_party"
)
target_link_libraries(license_plate_recognition_runner PRIVATE
    ${CMAKE_DL_LIBS}
    "-framework Foundation"
    "-framework CoreVideo"
    "-framework Accelerate"
)
