# Protobuf & gRPC codegen module
set(PROTO_SRC_DIR ${CMAKE_CURRENT_SOURCE_DIR})
set(PROTO_FILES
    ${PROTO_SRC_DIR}/argus/v1/common.proto
    ${PROTO_SRC_DIR}/argus/v1/engine.proto
    ${PROTO_SRC_DIR}/argus/v1/app.proto
    ${PROTO_SRC_DIR}/argus/v1/person.proto
)
set(PROTO_GEN_DIR ${CMAKE_BINARY_DIR}/generated)
file(MAKE_DIRECTORY ${PROTO_GEN_DIR})

set(PROTO_SRCS
    ${PROTO_GEN_DIR}/argus/v1/common.pb.cc
    ${PROTO_GEN_DIR}/argus/v1/common.pb.h
    ${PROTO_GEN_DIR}/argus/v1/engine.pb.cc
    ${PROTO_GEN_DIR}/argus/v1/engine.pb.h
    ${PROTO_GEN_DIR}/argus/v1/engine.grpc.pb.cc
    ${PROTO_GEN_DIR}/argus/v1/engine.grpc.pb.h
    ${PROTO_GEN_DIR}/argus/v1/app.pb.cc
    ${PROTO_GEN_DIR}/argus/v1/app.pb.h
    ${PROTO_GEN_DIR}/argus/v1/app.grpc.pb.cc
    ${PROTO_GEN_DIR}/argus/v1/app.grpc.pb.h
    ${PROTO_GEN_DIR}/argus/v1/person.pb.cc
    ${PROTO_GEN_DIR}/argus/v1/person.pb.h
    ${PROTO_GEN_DIR}/argus/v1/person.grpc.pb.cc
    ${PROTO_GEN_DIR}/argus/v1/person.grpc.pb.h
)

find_program(GRPC_CPP_PLUGIN_EXECUTABLE grpc_cpp_plugin REQUIRED)

add_custom_command(
    OUTPUT ${PROTO_SRCS}
    COMMAND protobuf::protoc
        --proto_path=${PROTO_SRC_DIR}
        --cpp_out=${PROTO_GEN_DIR}
        --grpc_out=${PROTO_GEN_DIR}
        --plugin=protoc-gen-grpc=${GRPC_CPP_PLUGIN_EXECUTABLE}
        ${PROTO_FILES}
    DEPENDS ${PROTO_FILES}
    COMMENT "Generating gRPC/Protobuf C++ files"
)

add_library(proto_gen STATIC ${PROTO_SRCS})
target_include_directories(proto_gen PUBLIC
    $<BUILD_INTERFACE:${PROTO_GEN_DIR}>
)
target_link_libraries(proto_gen PUBLIC
    protobuf::libprotobuf
    gRPC::grpc++
)
