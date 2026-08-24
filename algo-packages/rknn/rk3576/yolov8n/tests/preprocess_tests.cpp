#include <gtest/gtest.h>
#include "preprocess/preprocessor.hpp"

TEST(PreprocessTest, LetterboxCalculation) {
    auto lb = aivision::cv::compute_letterbox(1920, 1080, 640, 640);
    EXPECT_GT(lb.scale, 0.0f);
    EXPECT_EQ(lb.net_w, 640u);
    EXPECT_EQ(lb.net_h, 640u);
}
