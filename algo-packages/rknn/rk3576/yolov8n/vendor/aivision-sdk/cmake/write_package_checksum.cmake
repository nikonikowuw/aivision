if(NOT DEFINED INPUT_FILE OR NOT DEFINED OUTPUT_FILE)
    message(FATAL_ERROR "Package checksum requires INPUT_FILE and OUTPUT_FILE")
endif()

if(NOT EXISTS "${INPUT_FILE}" OR IS_DIRECTORY "${INPUT_FILE}")
    message(FATAL_ERROR "Package archive does not exist: ${INPUT_FILE}")
endif()

file(SHA256 "${INPUT_FILE}" digest)
if(digest STREQUAL "")
    message(FATAL_ERROR "Could not compute package SHA-256: ${INPUT_FILE}")
endif()
get_filename_component(input_name "${INPUT_FILE}" NAME)
file(WRITE "${OUTPUT_FILE}" "${digest}  ${input_name}\n")
