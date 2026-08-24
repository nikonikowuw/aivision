if(NOT DEFINED PACKAGE_DIR OR NOT DEFINED SOURCE_MANIFEST)
    message(FATAL_ERROR "PACKAGE_DIR and SOURCE_MANIFEST are required")
endif()

file(SHA256 "${PACKAGE_DIR}/lib/libmock_algo.dylib" library_sha)
file(READ "${SOURCE_MANIFEST}" manifest)
string(REPLACE "@LIBRARY_SHA256@" "${library_sha}" manifest "${manifest}")
file(WRITE "${PACKAGE_DIR}/manifest.json" "${manifest}")
