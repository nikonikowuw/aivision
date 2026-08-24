#include <gtest/gtest.h>
#include "postprocess/postprocessor.hpp"

TEST(PostprocessTest, LabelLookup) {
    yolov8n::Postprocessor::set_labels({"person", "bicycle", "car"});
    EXPECT_EQ(yolov8n::Postprocessor::get_label(0), "person");
    EXPECT_EQ(yolov8n::Postprocessor::get_label(1), "bicycle");
    EXPECT_EQ(yolov8n::Postprocessor::get_label(2), "car");
    EXPECT_EQ(yolov8n::Postprocessor::get_label(99), "unknown");
}
