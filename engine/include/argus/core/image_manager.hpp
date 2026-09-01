#pragma once

/**
 * @file image_manager.hpp
 * @brief 抓拍图片 Catalog 管理、原子存储与对账清理
 * 
 * 引擎作为告警抓拍图片的唯一所有者：
 * 1. 负责依据 ROI 裁剪并编码为 JPEG 写入磁盘；
 * 2. 维护本地元数据 catalog（catalog.json），跟踪 unreported / reported 状态；
 * 3. 提供幂等删除、批量删除、孤儿图片扫描及双向对账清理。
 */

#include <string>
#include <vector>
#include <mutex>
#include <filesystem>
#include <functional>
#include <cstdint>
#include <unordered_map>
#include "argus/types.h"
#include "argus/platform/platform_api.hpp"

namespace argus::core {

/**
 * @brief 图片删除操作的状态结果
 */
enum class ImageDeleteStatus {
    DELETED,        ///< 成功删除文件与 catalog 记录
    ALREADY_ABSENT, ///< 目标文件本身已不存在（幂等成功）
    FAILED,         ///< 删除失败（如权限错误或非法路径）
};

/**
 * @brief 抓拍图片本地记录元数据
 */
struct ImageRecord {
    std::string image_id;                   ///< 图片唯一标识
    std::string event_id;                   ///< 关联的批次/抓拍拥有者 ID；同一图片可被多个目标事件引用
    std::string rel_path;                   ///< 相对于 images 存储根目录的相对路径
    int64_t created_at_ns = 0;              ///< 生成时间戳（纳秒）
    std::string report_status = "unreported"; ///< 图片级上报状态："unreported" 或 "reported"
};

/**
 * @brief 抓拍图片管理器（单例）
 */
class ImageManager {
public:
    static ImageManager& instance();

    explicit ImageManager(std::function<int64_t()> now_provider = {});
    ~ImageManager();

    /**
     * @brief 初始化存储目录与图像处理器
     * @param base_dir 图片存储根目录（如 "var/images"）
     * @param processor 平台图像处理器指针
     */
    void init(const std::string& base_dir, std::shared_ptr<platform::IImageProcessor> processor);

    /**
     * @brief 裁剪并编码保存一个检测批次的共享抓拍图片
     * @param frame 原始视频帧
     * @param crop_roi 裁剪区域（为空表示全帧抓拍）
     * @param capture_id 批次级抓拍拥有者 ID，不是单个目标事件 ID
     * @param out_record 输出生成的元数据记录；调用方可将 image_id 复制到多个目标事件
     */
    av_status save_detection_image(
        const av_frame_desc* frame,
        const av_rect* crop_roi,
        const std::string& capture_id,
        ImageRecord* out_record
    );

    /**
     * @brief 标记图片已成功上报至控制面
     */
    av_status mark_reported(const std::string& image_id);

    /**
     * @brief 单张删除图片
     */
    av_status delete_image(const std::string& image_id);

    /**
     * @brief 单张删除图片并返回具体状态
     */
    ImageDeleteStatus delete_image_with_status(const std::string& image_id);

    /**
     * @brief 批量删除图片
     */
    std::vector<std::pair<std::string, bool>> batch_delete_images(const std::vector<std::string>& image_ids);

    /**
     * @brief 控制面对账：传入需要保留的图片 ID 集合，删除不在集合中的已上报图片
     */
    std::vector<std::pair<std::string, ImageDeleteStatus>> reconcile_images(const std::vector<std::string>& retain_image_ids);

    /**
     * @brief 返回需要继续向控制面对账的未上报 Catalog 记录
     */
    std::vector<ImageRecord> list_unreported_images();

    /**
     * @brief 扫描并获取未在数据库中登记的孤儿图片列表
     */
    std::vector<std::string> scan_orphan_images(const std::vector<std::string>& active_db_image_ids);

private:
    bool load_catalog_locked();
    bool persist_catalog_locked();
    bool is_safe_image_id(const std::string& image_id) const;
    bool is_path_within_base(const std::filesystem::path& path) const;
    std::string make_image_id(const std::string& capture_id, int64_t created_at_ns) const;
    std::string date_directory(int64_t created_at_ns) const;

    std::string base_dir_ = "var/images";
    std::shared_ptr<platform::IImageProcessor> processor_;
    std::function<int64_t()> now_provider_;
    std::unordered_map<std::string, ImageRecord> catalog_;
    std::mutex mutex_;
};

} // namespace argus::core

