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

int main() {
    @autoreleasepool {
        test_similarity_transform_alignment();
    }
    std::cout << "All preprocess tests passed!" << std::endl;
    return 0;
}
