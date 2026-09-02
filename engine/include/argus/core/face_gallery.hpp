#pragma once

/**
 * @file face_gallery.hpp
 * @brief Engine 内存人脸底库及归一化 1:N 比对索引。
 *
 * 底库由 Go 控制面按 gallery_revision 提供全量快照。模块先在锁外校验并构建
 * 连续 embedding 缓冲区，再在独占锁内整体替换；任何坏快照都不会破坏当前可用底库。
 * match() 使用共享锁，返回最高归一化相似度及其 face/person 快照，不暴露原始向量。
 */

#include <cstddef>
#include <cstdint>
#include <optional>
#include <shared_mutex>
#include <string>
#include <vector>

#include "argus/v1/app.pb.h"

namespace argus::core {

/// 人脸 embedding 的固定维度。
inline constexpr std::size_t kFaceEmbeddingDimensions = 512;
/// 人脸 embedding 的固定字节长度（512 个 little-endian float32）。
inline constexpr std::size_t kFaceEmbeddingBytes = kFaceEmbeddingDimensions * sizeof(float);
/// MVP 人脸底库的全局条目上限。
inline constexpr std::size_t kMaxFaceGalleryEntries = 5000;

/**
 * @brief 底库中的单个 face-level 条目元数据。
 */
struct FaceGalleryEntry {
    std::string face_id;
    std::string person_id;
    std::string person_name;
};

/**
 * @brief 1:N 比对结果。
 */
struct FaceMatch {
    FaceGalleryEntry entry;
    float similarity = 0.0f; ///< 归一化到 [0, 1] 的相似度。
};

/**
 * @brief 线程安全的人脸底库索引。
 *
 * @note Engine 当前使用进程级单例，以便结果回调和控制面线程共享同一份底库。
 *       load_from() 失败时保持旧版本；replace() 成功后才改变 revision 与 ready 状态。
 */
class FaceGallery {
public:
    /// 构造独立底库实例，便于测试和需要独立生命周期的调用方使用。
    FaceGallery() = default;

    /// 返回进程级底库实例。
    static FaceGallery& instance();

    /**
     * @brief 从控制面响应校验并全量替换底库。
     * @param response GetFaceGallery 响应；changed=false 时必须不含 entries。
     * @param error 可选的受控诊断原因，不包含 embedding 内容。
     * @return true 表示响应有效且已处理；false 表示整批拒绝并保留旧库。
     */
    bool load_from(const argus::v1::GetFaceGalleryResponse& response, std::string* error = nullptr);

    /**
     * @brief 用已经解码的连续向量缓冲区整体替换底库。
     * @param revision 新底库版本，必须大于 0。
     * @param flat_embeddings N * 512 个 L2 归一化 float32。
     * @param metadata 与 N 个向量一一对应的元数据。
     * @param error 可选的受控诊断原因。
     * @return true 表示替换成功；false 表示输入非法且旧库不变。
     */
    bool replace(uint64_t revision, std::vector<float> flat_embeddings,
                 std::vector<FaceGalleryEntry> metadata, std::string* error = nullptr);

    /// 返回当前已加载 revision；读操作使用共享锁。
    [[nodiscard]] uint64_t revision() const;
    /// 底库非空且 revision 已初始化时返回 true；空库是有效的已同步状态但不可匹配。
    [[nodiscard]] bool ready() const;
    /// 返回当前底库条目数。
    [[nodiscard]] std::size_t size() const;

    /**
     * @brief 对一个 512 维 L2 归一化 query 执行顺序 1:N 比对。
     * @param query_embedding 指向至少 512 个 float 的只读缓冲区。
     * @return 最高归一化相似度；query 非法或底库为空时返回 nullopt。
     */
    [[nodiscard]] std::optional<FaceMatch> match(const float* query_embedding) const;

    /**
     * @brief 对一个 512 维 L2 归一化 query 执行 1:N 比对，返回 Top-K 候选（降序排列）。
     * @param query_embedding 指向至少 512 个 float 的只读缓冲区。
     * @param k 最大返回候选数量，默认 5。
     * @return 降序排列的 Top-K 候选结果；query 非法或底库为空时返回空 vector。
     */
    [[nodiscard]] std::vector<FaceMatch> match_topk(const float* query_embedding, std::size_t k = 5) const;

private:
    mutable std::shared_mutex mutex_;
    std::vector<float> embeddings_; ///< N * 512 的连续向量，便于顺序点积。
    std::vector<FaceGalleryEntry> metadata_;
    uint64_t revision_ = 0;
};

} // namespace argus::core
