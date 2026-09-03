#import <Foundation/Foundation.h>
#include "preprocess/preprocessor.hpp"
#include <cassert>
#include <iostream>

using namespace face_recognition;

void test_similarity_transform_alignment() {
    ImageBuffer img;
    img.width = 640;
    img.height = 480;
    img.channels = 3;
    img.data.assign(640 * 480 * 3, 200);

    float test_landmarks[10] = {
        132.48f, 436.64f,
        149.15f, 437.39f,
        144.36f, 446.31f,
        134.71f, 455.58f,
        147.61f, 456.25f
    };

    ImageBuffer out_face;
    std::string err;
    bool ok = Preprocessor::align_face_112x112(img, test_landmarks, out_face, err);
    assert(ok);
    assert(out_face.width == 112);
    assert(out_face.height == 112);
    assert(out_face.data.size() == 112 * 112 * 3);
    assert(err.empty());
    (void)ok;

    std::cout << "[PASS] test_similarity_transform_alignment" << std::endl;
}

void test_nv12_preprocess_and_alignment() {
    constexpr uint32_t width = 1920;
    constexpr uint32_t height = 1080;
    std::vector<uint8_t> nv12_buf(width * height * 3 / 2, 128);

    av_frame_desc frame{};
    frame.width = width;
    frame.height = height;
    frame.pixel_format = AV_PIX_NV12;
    frame.stride[0] = width;
    frame.stride[1] = width;
    frame.opaque = nv12_buf.data();
    frame.opaque_kind = AV_OPAQUE_NONE;
    frame.memory_type = AV_MEM_HOST;

    PreprocessResult prep_res;
    std::string err;
    bool ok = Preprocessor::process_frame(&frame, prep_res, err);
    assert(ok);
    assert(err.empty());
    assert(prep_res.orig_width == 1920);
    assert(prep_res.orig_height == 1080);
    assert(prep_res.letterbox_rgb.width == 640);
    assert(prep_res.letterbox_rgb.height == 384);
    assert(prep_res.letterbox_rgb.data.size() == 640 * 384 * 3);

    float test_landmarks[10] = {
        900.0f, 500.0f,
        1020.0f, 502.0f,
        960.0f, 550.0f,
        910.0f, 600.0f,
        1010.0f, 602.0f
    };

    ImageBuffer out_face;
    ok = Preprocessor::align_face_112x112(prep_res, test_landmarks, out_face, err);
    assert(ok);
    assert(err.empty());
    assert(out_face.width == 112);
    assert(out_face.height == 112);
    assert(out_face.data.size() == 112 * 112 * 3);

    std::cout << "[PASS] test_nv12_preprocess_and_alignment" << std::endl;
}

int main() {
    @autoreleasepool {
        test_similarity_transform_alignment();
        test_nv12_preprocess_and_alignment();
    }
    std::cout << "All preprocess tests passed!" << std::endl;
    return 0;
}
