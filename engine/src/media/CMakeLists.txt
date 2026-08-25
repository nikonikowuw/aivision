# Media backend module
if(AIVISION_USE_ZLM)
    add_library(media_zlm STATIC
        zlm/zlm_source.cpp
    )
    target_include_directories(media_zlm PUBLIC
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../include>
    )
    target_include_directories(media_zlm SYSTEM PUBLIC
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../third_party/ZLMediaKit/src>
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../third_party/ZLMediaKit/3rdpart/ZLToolKit/src>
    )
    target_link_libraries(media_zlm PUBLIC
        media_api
        zlmediakit
        ZLToolKit
        ext-codec
    )
    aivision_enable_strict_warnings(media_zlm)
else()
    add_library(media_mock STATIC
        mock/mock_source.cpp
    )
    target_include_directories(media_mock PUBLIC
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/../../include>
    )
    target_link_libraries(media_mock PUBLIC media_api)
    aivision_enable_strict_warnings(media_mock)
endif()
