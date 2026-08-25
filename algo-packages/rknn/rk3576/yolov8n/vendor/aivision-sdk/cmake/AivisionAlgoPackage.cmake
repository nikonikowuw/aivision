# AivisionAlgoPackage.cmake
function(aivision_add_algo_package TARGET_NAME)
    set(options)
    set(oneValueArgs ALGORITHM_ID VERSION PLATFORM_ID)
    set(multiValueArgs SRCS DEPS TARGET_DEPS)
    cmake_parse_arguments(ALGO_PKG "${options}" "${oneValueArgs}" "${multiValueArgs}" ${ARGN})

    add_library(${TARGET_NAME} SHARED ${ALGO_PKG_SRCS})
    target_link_libraries(${TARGET_NAME} PRIVATE
        aivision_sdk_headers
        ${ALGO_PKG_TARGET_DEPS}
        ${ALGO_PKG_DEPS}
    )
    set_target_properties(${TARGET_NAME} PROPERTIES
        PREFIX "lib"
        OUTPUT_NAME "${ALGO_PKG_ALGORITHM_ID}"
        C_VISIBILITY_PRESET hidden
        CXX_VISIBILITY_PRESET hidden
        VISIBILITY_INLINES_HIDDEN YES
    )

    set(PACKAGE_OUT_DIR "${CMAKE_CURRENT_BINARY_DIR}/package_out")
    set(PACKAGE_ARCHIVE "${CMAKE_CURRENT_BINARY_DIR}/${ALGO_PKG_ALGORITHM_ID}-${ALGO_PKG_VERSION}-${ALGO_PKG_PLATFORM_ID}.zip")
    set(PACKAGE_DIGEST "${PACKAGE_ARCHIVE}.sha256")
    set(PACKAGE_ENTRY_LIBRARY "lib/lib${ALGO_PKG_ALGORITHM_ID}${CMAKE_SHARED_LIBRARY_SUFFIX}")
    set(PACKAGE_OPTIONAL_COPY_COMMANDS)
    if(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/config.schema.json" AND
       NOT IS_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/config.schema.json")
        list(APPEND PACKAGE_OPTIONAL_COPY_COMMANDS
            COMMAND ${CMAKE_COMMAND} -E copy
                "${CMAKE_CURRENT_SOURCE_DIR}/config.schema.json"
                "${PACKAGE_OUT_DIR}/config.schema.json")
    endif()
    if(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/.env" AND
       NOT IS_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/.env")
        list(APPEND PACKAGE_OPTIONAL_COPY_COMMANDS
            COMMAND ${CMAKE_COMMAND} -E copy
                "${CMAKE_CURRENT_SOURCE_DIR}/.env"
                "${PACKAGE_OUT_DIR}/.env")
    endif()
    if(IS_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/model")
        list(APPEND PACKAGE_OPTIONAL_COPY_COMMANDS
            COMMAND ${CMAKE_COMMAND} -E copy_directory
                "${CMAKE_CURRENT_SOURCE_DIR}/model"
                "${PACKAGE_OUT_DIR}/model")
    endif()
    add_custom_target(package_${TARGET_NAME}
        COMMAND ${CMAKE_COMMAND} -E rm -rf "${PACKAGE_OUT_DIR}"
        COMMAND ${CMAKE_COMMAND} -E make_directory "${PACKAGE_OUT_DIR}/lib"
        COMMAND ${CMAKE_COMMAND} -E copy "$<TARGET_FILE:${TARGET_NAME}>" "${PACKAGE_OUT_DIR}/${PACKAGE_ENTRY_LIBRARY}"
        COMMAND ${CMAKE_COMMAND} -E copy "${CMAKE_CURRENT_SOURCE_DIR}/testimage.jpg" "${PACKAGE_OUT_DIR}/testimage.jpg"
        ${PACKAGE_OPTIONAL_COPY_COMMANDS}
        COMMAND ${CMAKE_COMMAND}
            -DSTAGING_DIR=${PACKAGE_OUT_DIR}
            -DSOURCE_MANIFEST=${CMAKE_CURRENT_SOURCE_DIR}/manifest.json
            -DENTRY_LIBRARY=${PACKAGE_ENTRY_LIBRARY}
            -P "${CMAKE_CURRENT_FUNCTION_LIST_DIR}/validate_package.cmake"
        COMMAND ${CMAKE_COMMAND} -E chdir "${PACKAGE_OUT_DIR}"
            ${CMAKE_COMMAND} -E tar "cfv" "${PACKAGE_ARCHIVE}" --format=zip .
        COMMAND ${CMAKE_COMMAND}
            -DINPUT_FILE=${PACKAGE_ARCHIVE}
            -DOUTPUT_FILE=${PACKAGE_DIGEST}
            -P "${CMAKE_CURRENT_FUNCTION_LIST_DIR}/write_package_checksum.cmake"
        DEPENDS ${TARGET_NAME}
        BYPRODUCTS "${PACKAGE_ARCHIVE}" "${PACKAGE_DIGEST}"
        COMMENT "Packaging algorithm bundle for ${TARGET_NAME}"
        VERBATIM
    )
endfunction()
