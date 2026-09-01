/**
 * @file face_extract_symbol.c
 * @brief 符号审计正向 fixture：导出白名单内的可选扩展符号 av_algo_extract_face
 */

#include "argus/algo.h"

AV_EXPORT int av_algo_extract_face(av_algo_library lib, const av_face_extract_input* in,
                                   av_face_extract_output* out) {
    (void)lib;
    (void)in;
    (void)out;
    return AV_ERR_NOT_IMPLEMENTED;
}
