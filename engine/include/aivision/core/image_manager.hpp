#pragma once

#include <string>
#include <vector>
#include <mutex>
#include <filesystem>
#include <cstdint>
#include <unordered_map>
#include "aivision/types.h"
#include "aivision/platform/platform_api.hpp"

namespace aivision::core {

enum class ImageDeleteStatus {
    DELETED,
    ALREADY_ABSENT,
    FAILED,
};

struct ImageRecord {
    std::string image_id;
    std::string event_id;
    std::string rel_path;
    int64_t created_at_ns = 0;
    std::string report_status = "unreported";
};

class ImageManager {
public:
    static ImageManager& instance();

    ImageManager();
    ~ImageManager();

    void init(const std::string& base_dir, std::shared_ptr<platform::IImageProcessor> processor);

    av_status save_detection_image(
        const av_frame_desc* frame,
        const av_rect* crop_roi,
        const std::string& event_id,
        ImageRecord* out_record
    );

    av_status mark_reported(const std::string& image_id);
    av_status delete_image(const std::string& image_id);
    ImageDeleteStatus delete_image_with_status(const std::string& image_id);
    std::vector<std::pair<std::string, bool>> batch_delete_images(const std::vector<std::string>& image_ids);
    std::vector<std::pair<std::string, ImageDeleteStatus>> reconcile_images(const std::vector<std::string>& retain_image_ids);
    std::vector<std::string> scan_orphan_images(const std::vector<std::string>& active_db_image_ids);

private:
    bool load_catalog_locked();
    bool persist_catalog_locked();
    bool is_safe_image_id(const std::string& image_id) const;
    bool is_path_within_base(const std::filesystem::path& path) const;
    std::string make_image_id(const std::string& event_id, int64_t created_at_ns) const;
    std::string date_directory(int64_t created_at_ns) const;

    std::string base_dir_ = "var/images";
    std::shared_ptr<platform::IImageProcessor> processor_;
    std::unordered_map<std::string, ImageRecord> catalog_;
    std::mutex mutex_;
};

} // namespace aivision::core
