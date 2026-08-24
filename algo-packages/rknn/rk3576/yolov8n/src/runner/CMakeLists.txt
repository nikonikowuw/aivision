add_executable(run_local
    standalone_runner.cpp
)

target_include_directories(run_local PRIVATE
    third_party
    ${CMAKE_CURRENT_SOURCE_DIR}/..
)

target_link_libraries(run_local PRIVATE
    yolov8n
    aivision_sdk_headers
    yolov8n_preprocess
    yolov8n_inference
    yolov8n_postprocess
)

if(TARGET rknnrt)
    target_link_libraries(run_local PRIVATE rknnrt)
endif()
