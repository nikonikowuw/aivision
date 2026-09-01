#import <Foundation/Foundation.h>
#import <CoreVideo/CoreVideo.h>
#include "preprocess/preprocessor.hpp"
#include <cassert>
#include <iostream>

using namespace lpr;

void test_warp_plate_geometry() {
    ImageBuffer src;
    src.width = 640;
    src.height = 480;
    src.channels = 3;
    src.data.assign(640 * 480 * 3, 200);

    // Quad points in src
    float landmarks[8] = {
        100.0f, 100.0f, // tl
        420.0f, 100.0f, // tr
        420.0f, 148.0f, // br
        100.0f, 148.0f  // bl
    };

    ImageBuffer out_plate;
    std::string err;
    bool ok = Preprocessor::warp_plate_320x48(src, landmarks, false, out_plate, err);
    assert(ok);
    assert(out_plate.width == 320);
    assert(out_plate.height == 48);
    assert(out_plate.data.size() == 320 * 48 * 3);
    assert(out_plate.data[0] == 200);

    std::cout << "[PASS] test_warp_plate_geometry" << std::endl;
}

int main() {
    @autoreleasepool {
        test_warp_plate_geometry();
        std::cout << "All preprocess tests passed!" << std::endl;
    }
    return 0;
}
