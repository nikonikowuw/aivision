/**
 * @file face_gallery.cpp
 * @brief Engine 内存人脸底库校验、原子换库与 1:N 点积比对实现。
 */

#include "argus/core/face_gallery.hpp"

#include <algorithm>
#include <cmath>
#include <cstring>
#include <limits>
#include <mutex>
#include <numeric>
#include <unordered_set>
#include <utility>

namespace argus::core {
namespace {

bool set_error(std::string* error, const char* message) {
    if (error) *error = message;
    return false;
}

bool valid_embedding(const float* embedding) {
    if (!embedding) return false;

    double norm_squared = 0.0;
    for (std::size_t i = 0; i < kFaceEmbeddingDimensions; ++i) {
        const float value = embedding[i];
        if (!std::isfinite(value)) return false;
        const double value_as_double = static_cast<double>(value);
        norm_squared += value_as_double * value_as_double;
    }
    if (!std::isfinite(norm_squared)) return false;

    const double norm = std::sqrt(norm_squared);
    return std::isfinite(norm) && norm >= 0.98 && norm <= 1.02;
}

bool valid_entry_count(std::size_t count, std::string* error) {
    if (count > kMaxFaceGalleryEntries) {
        return set_error(error, "face gallery entry count exceeds limit");
    }
    return true;
}

} // namespace

FaceGallery& FaceGallery::instance() {
    static FaceGallery gallery;
    return gallery;
}

bool FaceGallery::load_from(const argus::v1::GetFaceGalleryResponse& response, std::string* error) {
    const uint64_t current_revision = revision();
    if (!response.changed()) {
        if (response.gallery_revision() != current_revision) {
            return set_error(error, "unchanged face gallery response has unexpected revision");
        }
        if (response.entries_size() != 0) {
            return set_error(error, "unchanged face gallery response contains entries");
        }
        return true;
    }
    if (response.gallery_revision() == 0 || response.gallery_revision() <= current_revision) {
        return set_error(error, "changed face gallery response has stale revision");
    }
    if (!valid_entry_count(static_cast<std::size_t>(response.entries_size()), error)) {
        return false;
    }

    std::vector<float> flat_embeddings;
    std::vector<FaceGalleryEntry> metadata;
    flat_embeddings.reserve(static_cast<std::size_t>(response.entries_size()) * kFaceEmbeddingDimensions);
    metadata.reserve(static_cast<std::size_t>(response.entries_size()));

    std::unordered_set<std::string> face_ids;
    face_ids.reserve(static_cast<std::size_t>(response.entries_size()));
    for (const auto& source : response.entries()) {
        if (source.face_id().empty() || source.person_id().empty()) {
            return set_error(error, "face gallery entry identity is empty");
        }
        if (!face_ids.insert(source.face_id()).second) {
            return set_error(error, "face gallery entry face_id is duplicated");
        }
        if (source.embedding().size() != static_cast<int>(kFaceEmbeddingBytes)) {
            return set_error(error, "face gallery embedding length is invalid");
        }

        std::vector<float> decoded(kFaceEmbeddingDimensions);
        // 部署平台为 little-endian；memcpy 避免未对齐 protobuf string 访问和 strict-aliasing 问题。
        std::memcpy(decoded.data(), source.embedding().data(), kFaceEmbeddingBytes);
        if (!valid_embedding(decoded.data())) {
            return set_error(error, "face gallery embedding is not finite or normalized");
        }

        flat_embeddings.insert(flat_embeddings.end(), decoded.begin(), decoded.end());
        metadata.push_back(FaceGalleryEntry{
            source.face_id(),
            source.person_id(),
            source.person_name(),
        });
    }

    return replace(response.gallery_revision(), std::move(flat_embeddings), std::move(metadata), error);
}

bool FaceGallery::replace(uint64_t revision, std::vector<float> flat_embeddings,
                          std::vector<FaceGalleryEntry> metadata, std::string* error) {
    if (revision == 0) {
        return set_error(error, "face gallery revision is zero");
    }
    if (!valid_entry_count(metadata.size(), error)) return false;
    if (flat_embeddings.size() != metadata.size() * kFaceEmbeddingDimensions) {
        return set_error(error, "face gallery embedding count does not match metadata");
    }

    std::unordered_set<std::string> face_ids;
    face_ids.reserve(metadata.size());
    for (std::size_t i = 0; i < metadata.size(); ++i) {
        if (metadata[i].face_id.empty() || metadata[i].person_id.empty()) {
            return set_error(error, "face gallery entry identity is empty");
        }
        if (!face_ids.insert(metadata[i].face_id).second) {
            return set_error(error, "face gallery entry face_id is duplicated");
        }
        if (!valid_embedding(flat_embeddings.data() + i * kFaceEmbeddingDimensions)) {
            return set_error(error, "face gallery embedding is not finite or normalized");
        }
    }

    // 所有分配与校验已完成；独占锁内只执行 move，避免结果线程观察到半套数据。
    std::unique_lock<std::shared_mutex> lock(mutex_);
    if (revision <= revision_) {
        return set_error(error, "face gallery revision is stale");
    }
    embeddings_ = std::move(flat_embeddings);
    metadata_ = std::move(metadata);
    revision_ = revision;
    return true;
}

uint64_t FaceGallery::revision() const {
    std::shared_lock<std::shared_mutex> lock(mutex_);
    return revision_;
}

bool FaceGallery::ready() const {
    std::shared_lock<std::shared_mutex> lock(mutex_);
    return revision_ > 0 && !metadata_.empty();
}

std::size_t FaceGallery::size() const {
    std::shared_lock<std::shared_mutex> lock(mutex_);
    return metadata_.size();
}

std::optional<FaceMatch> FaceGallery::match(const float* query_embedding) const {
    if (!valid_embedding(query_embedding)) return std::nullopt;

    std::shared_lock<std::shared_mutex> lock(mutex_);
    if (metadata_.empty() || embeddings_.size() != metadata_.size() * kFaceEmbeddingDimensions) {
        return std::nullopt;
    }

    std::size_t best_index = 0;
    float best_similarity = -std::numeric_limits<float>::infinity();
    for (std::size_t i = 0; i < metadata_.size(); ++i) {
        const float* base = embeddings_.data() + i * kFaceEmbeddingDimensions;
        float dot = 0.0f;
        for (std::size_t j = 0; j < kFaceEmbeddingDimensions; ++j) {
            dot += query_embedding[j] * base[j];
        }
        if (!std::isfinite(dot)) continue;
        const float normalized = std::clamp((dot + 1.0f) * 0.5f, 0.0f, 1.0f);
        if (normalized > best_similarity) {
            best_similarity = normalized;
            best_index = i;
        }
    }

    if (!std::isfinite(best_similarity)) return std::nullopt;
    return FaceMatch{metadata_[best_index], best_similarity};
}

} // namespace argus::core
