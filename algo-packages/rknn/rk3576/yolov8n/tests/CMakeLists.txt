enable_testing()

add_executable(abi_tests abi_tests.cpp)
target_link_libraries(abi_tests PRIVATE yolov8n argus_sdk_headers)
target_include_directories(abi_tests PRIVATE "${CMAKE_CURRENT_SOURCE_DIR}/../src")
add_test(NAME abi_tests COMMAND abi_tests)

add_executable(package_tests package_tests.cpp)
target_link_libraries(package_tests PRIVATE yolov8n argus_sdk_headers)
target_include_directories(package_tests PRIVATE "${CMAKE_CURRENT_SOURCE_DIR}/../src")
target_compile_definitions(package_tests PRIVATE PACKAGE_ROOT_DIR="${CMAKE_CURRENT_SOURCE_DIR}/..")
add_test(NAME package_tests COMMAND package_tests)

add_executable(preprocess_tests preprocess_tests.cpp)
target_link_libraries(preprocess_tests PRIVATE yolov8n_preprocess argus_sdk_headers)
target_include_directories(preprocess_tests PRIVATE "${CMAKE_CURRENT_SOURCE_DIR}/../src")
add_test(NAME preprocess_tests COMMAND preprocess_tests)

add_executable(postprocess_tests postprocess_tests.cpp)
target_link_libraries(postprocess_tests PRIVATE yolov8n_postprocess argus_sdk_headers)
target_include_directories(postprocess_tests PRIVATE "${CMAKE_CURRENT_SOURCE_DIR}/../src")
add_test(NAME postprocess_tests COMMAND postprocess_tests)
