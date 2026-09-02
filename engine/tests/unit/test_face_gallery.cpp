/**
 * @file test_face_gallery.cpp
 * @brief FaceGallery 底库快照、校验、空库和 1:N 比对单元测试。
 */

#include <gtest/gtest.h>

#include <cmath>
#include <cstring>
#include <string>
#include <vector>

#include "argus/core/face_gallery.hpp"

namespace {

std::vector<float> unit_vector(std::size_t index, float sign = 1.0f) {
    std::vector<float> embedding(argus::core::kFaceEmbeddingDimensions, 0.0f);
    embedding[index] = sign;
    return embedding;
}

std::string encode_embedding(const std::vector<float>& embedding) {
    return std::string(reinterpret_cast<const char*>(embedding.data()),
                       embedding.size() * sizeof(float));
}

void fill_gallery_response(
    argus::v1::GetFaceGalleryResponse* response,
    uint64_t revision,
    std::initializer_list<std::pair<const char*, std::vector<float>>> entries) {
    response->set_changed(true);
    response->set_gallery_revision(revision);
    for (const auto& [face_id, embedding] : entries) {
        auto* entry = response->add_entries();
        entry->set_face_id(face_id);
        entry->set_person_id(std::string("person-") + face_id);
        entry->set_person_name(std::string("name-") + face_id);
        entry->set_embedding(encode_embedding(embedding));
    }
}

} // namespace

TEST(FaceGalleryTest, ReplacesSnapshotAndReturnsBestMatch) {
    argus::core::FaceGallery gallery;
    argus::v1::GetFaceGalleryResponse response;
    fill_gallery_response(&response, 2,
                          {{"face-a", unit_vector(0)}, {"face-b", unit_vector(1)}});

    std::string error;
    ASSERT_TRUE(gallery.load_from(response, &error)) << error;
    EXPECT_EQ(gallery.revision(), 2U);
    EXPECT_EQ(gallery.size(), 2U);
    ASSERT_TRUE(gallery.ready());

    const auto match = gallery.match(unit_vector(1).data());
    ASSERT_TRUE(match.has_value());
    EXPECT_EQ(match->entry.face_id, "face-b");
    EXPECT_EQ(match->entry.person_id, "person-face-b");
    EXPECT_FLOAT_EQ(match->similarity, 1.0f);
}

TEST(FaceGalleryTest, RejectsInvalidSnapshotAndKeepsPreviousVersion) {
    argus::core::FaceGallery gallery;
    ASSERT_TRUE(gallery.replace(3, unit_vector(0),
                                {argus::core::FaceGalleryEntry{"face-a", "person-a", "Alice"}}));

    argus::v1::GetFaceGalleryResponse invalid;
    fill_gallery_response(&invalid, 4, {{"face-b", unit_vector(0)}});
    invalid.mutable_entries(0)->set_embedding(std::string(4, '\0'));

    std::string error;
    EXPECT_FALSE(gallery.load_from(invalid, &error));
    EXPECT_FALSE(error.empty());
    EXPECT_EQ(gallery.revision(), 3U);
    EXPECT_EQ(gallery.size(), 1U);
    ASSERT_TRUE(gallery.match(unit_vector(0).data()).has_value());
    EXPECT_EQ(gallery.match(unit_vector(0).data())->entry.face_id, "face-a");
}

TEST(FaceGalleryTest, UnchangedResponseDoesNotClearCurrentSnapshot) {
    argus::core::FaceGallery gallery;
    ASSERT_TRUE(gallery.replace(5, unit_vector(0),
                                {argus::core::FaceGalleryEntry{"face-a", "person-a", "Alice"}}));

    argus::v1::GetFaceGalleryResponse response;
    response.set_changed(false);
    response.set_gallery_revision(5);
    std::string error;
    ASSERT_TRUE(gallery.load_from(response, &error)) << error;

    EXPECT_EQ(gallery.revision(), 5U);
    EXPECT_EQ(gallery.size(), 1U);
}

TEST(FaceGalleryTest, RejectsDuplicateFaceIDsInSnapshotAndKeepsPreviousVersion) {
    argus::core::FaceGallery gallery;
    ASSERT_TRUE(gallery.replace(3, unit_vector(0),
                                {argus::core::FaceGalleryEntry{"face-a", "person-a", "Alice"}}));

    argus::v1::GetFaceGalleryResponse response;
    fill_gallery_response(&response, 4,
                          {{"face-a", unit_vector(0)}, {"face-a", unit_vector(1)}});

    std::string error;
    EXPECT_FALSE(gallery.load_from(response, &error));
    EXPECT_EQ(error, "face gallery entry face_id is duplicated");
    EXPECT_EQ(gallery.revision(), 3U);
    EXPECT_EQ(gallery.size(), 1U);
    ASSERT_TRUE(gallery.match(unit_vector(0).data()).has_value());
    EXPECT_EQ(gallery.match(unit_vector(0).data())->entry.face_id, "face-a");
}

TEST(FaceGalleryTest, RejectsDuplicateFaceIDsInDirectReplacement) {
    argus::core::FaceGallery gallery;
    auto embeddings = unit_vector(0);
    const auto second_embedding = unit_vector(1);
    embeddings.insert(embeddings.end(), second_embedding.begin(), second_embedding.end());

    std::string error;
    EXPECT_FALSE(gallery.replace(1, std::move(embeddings),
                                 {argus::core::FaceGalleryEntry{"face-a", "person-a", "Alice"},
                                  argus::core::FaceGalleryEntry{"face-a", "person-b", "Bob"}},
                                 &error));
    EXPECT_EQ(error, "face gallery entry face_id is duplicated");
    EXPECT_EQ(gallery.revision(), 0U);
}

TEST(FaceGalleryTest, RejectsStaleChangedResponseAndMismatchedUnchangedResponse) {
    argus::core::FaceGallery gallery;
    ASSERT_TRUE(gallery.replace(5, unit_vector(0),
                                {argus::core::FaceGalleryEntry{"face-a", "person-a", "Alice"}}));

    argus::v1::GetFaceGalleryResponse stale;
    fill_gallery_response(&stale, 5, {{"face-b", unit_vector(1)}});
    std::string error;
    EXPECT_FALSE(gallery.load_from(stale, &error));
    EXPECT_EQ(error, "changed face gallery response has stale revision");

    argus::v1::GetFaceGalleryResponse mismatched;
    mismatched.set_changed(false);
    mismatched.set_gallery_revision(4);
    error.clear();
    EXPECT_FALSE(gallery.load_from(mismatched, &error));
    EXPECT_EQ(error, "unchanged face gallery response has unexpected revision");
    EXPECT_EQ(gallery.revision(), 5U);
    EXPECT_EQ(gallery.size(), 1U);
}

TEST(FaceGalleryTest, RejectsStaleDirectReplacement) {
    argus::core::FaceGallery gallery;
    ASSERT_TRUE(gallery.replace(5, unit_vector(0),
                                {argus::core::FaceGalleryEntry{"face-a", "person-a", "Alice"}}));

    std::string error;
    EXPECT_FALSE(gallery.replace(5, unit_vector(1),
                                 {argus::core::FaceGalleryEntry{"face-b", "person-b", "Bob"}},
                                 &error));
    EXPECT_EQ(error, "face gallery revision is stale");
    EXPECT_EQ(gallery.revision(), 5U);
    EXPECT_EQ(gallery.size(), 1U);
}

TEST(FaceGalleryTest, LoadsEmptyChangedSnapshotAsValidUnreadyGallery) {
    argus::core::FaceGallery gallery;
    argus::v1::GetFaceGalleryResponse response;
    response.set_changed(true);
    response.set_gallery_revision(1);

    std::string error;
    ASSERT_TRUE(gallery.load_from(response, &error)) << error;
    EXPECT_EQ(gallery.revision(), 1U);
    EXPECT_EQ(gallery.size(), 0U);
    EXPECT_FALSE(gallery.ready());
}

TEST(FaceGalleryTest, UnchangedResponseWithEntriesIsRejected) {
    argus::core::FaceGallery gallery;
    ASSERT_TRUE(gallery.replace(1, {}, {}));

    argus::v1::GetFaceGalleryResponse response;
    response.set_changed(false);
    response.set_gallery_revision(1);
    response.add_entries()->set_face_id("unexpected");

    std::string error;
    EXPECT_FALSE(gallery.load_from(response, &error));
    EXPECT_EQ(error, "unchanged face gallery response contains entries");
}

TEST(FaceGalleryTest, EmptySnapshotIsValidButNotReadyAndDoesNotMatch) {
    argus::core::FaceGallery gallery;
    std::string error;
    ASSERT_TRUE(gallery.replace(9, {}, {}, &error)) << error;

    EXPECT_EQ(gallery.revision(), 9U);
    EXPECT_EQ(gallery.size(), 0U);
    EXPECT_FALSE(gallery.ready());
    EXPECT_FALSE(gallery.match(unit_vector(0).data()).has_value());
}

TEST(FaceGalleryTest, RejectsNonNormalizedQuery) {
    argus::core::FaceGallery gallery;
    ASSERT_TRUE(gallery.replace(1, unit_vector(0),
                                {argus::core::FaceGalleryEntry{"face-a", "person-a", "Alice"}}));

    auto invalid_query = unit_vector(0);
    invalid_query[0] = 2.0f;
    EXPECT_FALSE(gallery.match(invalid_query.data()).has_value());
}

TEST(FaceGalleryTest, EnforcesMaximumEntryCount) {
    argus::core::FaceGallery gallery;
    std::vector<float> embeddings((argus::core::kMaxFaceGalleryEntries + 1) *
                                   argus::core::kFaceEmbeddingDimensions, 0.0f);
    std::vector<argus::core::FaceGalleryEntry> metadata;
    metadata.reserve(argus::core::kMaxFaceGalleryEntries + 1);
    for (std::size_t i = 0; i <= argus::core::kMaxFaceGalleryEntries; ++i) {
        embeddings[i * argus::core::kFaceEmbeddingDimensions] = 1.0f;
        metadata.push_back({std::to_string(i), "person", "name"});
    }

    EXPECT_FALSE(gallery.replace(1, std::move(embeddings), std::move(metadata)));
    EXPECT_EQ(gallery.revision(), 0U);
    EXPECT_EQ(gallery.size(), 0U);
}
