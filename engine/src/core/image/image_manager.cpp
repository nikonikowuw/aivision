/**
 * @file image_manager.cpp
 * @brief 告警抓拍图片 Catalog 记录管理与原子持久化实现
 *
 * 包含：
 * 1. write_atomic_file：临时文件写入 + fsync + 原子 rename 落盘；
 * 2. load_catalog_locked / persist_catalog_locked：catalog.json 解析与同步；
 * 3. 对账与安全路径检查（防止目录穿越）。
 */

#include "argus/core/image_manager.hpp"

#include <chrono>
#include <cctype>
#include <cerrno>
#include <ctime>
#include <fcntl.h>
#include <fstream>
#include <iomanip>
#include <nlohmann/json.hpp>
#include <sstream>
#include <sys/stat.h>
#include <unistd.h>
#include <unordered_set>


namespace fs = std::filesystem;

namespace argus::core {
namespace {

bool write_fd(int fd, const uint8_t* data, size_t size) {
    size_t offset = 0;
    while (offset < size) {
        const ssize_t written = ::write(fd, data + offset, size - offset);
        if (written <= 0) return false;
        offset += static_cast<size_t>(written);
    }
    return true;
}

bool fsync_directory(const fs::path& directory) {
    const int fd = ::open(directory.c_str(), O_RDONLY);
    if (fd < 0) return false;
    const bool ok = ::fsync(fd) == 0;
    ::close(fd);
    return ok;
}

bool write_atomic_file(const fs::path& temporary, const fs::path& final_path,
                       const std::string& bytes, bool exclusive) {
    const int flags = O_WRONLY | O_CREAT | (exclusive ? O_EXCL : O_TRUNC);
    const int fd = ::open(temporary.c_str(), flags, 0644);
    if (fd < 0) return false;

    const bool ok = write_fd(fd, reinterpret_cast<const uint8_t*>(bytes.data()), bytes.size()) &&
                    ::fsync(fd) == 0;
    const int saved_errno = errno;
    ::close(fd);
    if (!ok) {
        std::error_code ec;
        fs::remove(temporary, ec);
        errno = saved_errno;
        return false;
    }

    std::error_code ec;
    if (exclusive && fs::exists(final_path, ec)) {
        fs::remove(temporary, ec);
        return false;
    }
    if (ec) {
        fs::remove(temporary, ec);
        return false;
    }
    fs::rename(temporary, final_path, ec);
    if (ec) {
        fs::remove(temporary, ec);
        return false;
    }
    if (!fsync_directory(final_path.parent_path())) {
        fs::remove(final_path, ec);
        return false;
    }
    return true;
}

} // namespace

ImageManager& ImageManager::instance() {
    static ImageManager inst;
    return inst;
}

ImageManager::ImageManager() = default;
ImageManager::~ImageManager() = default;

void ImageManager::init(const std::string& base_dir, std::shared_ptr<platform::IImageProcessor> processor) {
    std::lock_guard<std::mutex> lock(mutex_);
    base_dir_ = base_dir;
    processor_ = std::move(processor);
    catalog_.clear();
    fs::create_directories(fs::path(base_dir_) / ".tmp");
    fs::create_directories(fs::path(base_dir_) / "catalog");

    std::error_code ec;
    const fs::path temp_dir = fs::path(base_dir_) / ".tmp";
    if (fs::exists(temp_dir, ec)) {
        for (const auto& entry : fs::directory_iterator(temp_dir, ec)) {
            if (!ec && entry.is_regular_file() && entry.path().extension() == ".part") {
                fs::remove(entry.path(), ec);
            }
        }
    }
    load_catalog_locked();
}

bool ImageManager::load_catalog_locked() {
    const fs::path catalog_path = fs::path(base_dir_) / "catalog" / "catalog.json";
    if (!fs::exists(catalog_path)) return true;

    try {
        std::ifstream input(catalog_path);
        const auto json = nlohmann::json::parse(input);
        if (!json.is_object()) return false;
        for (const auto& [image_id, value] : json.items()) {
            if (!is_safe_image_id(image_id) || !value.is_object()) continue;
            ImageRecord record;
            record.image_id = image_id;
            record.event_id = value.value("event_id", "");
            record.rel_path = value.value("relative_path", "");
            record.created_at_ns = value.value("created_at_ns", int64_t{0});
            record.report_status = value.value("report_status", "unreported");
            if (record.rel_path.empty() || !is_path_within_base(fs::path(base_dir_) / record.rel_path)) continue;
            catalog_.emplace(image_id, std::move(record));
        }
        return true;
    } catch (const std::exception&) {
        return false;
    }
}

bool ImageManager::persist_catalog_locked() {
    const fs::path catalog_dir = fs::path(base_dir_) / "catalog";
    std::error_code ec;
    fs::create_directories(catalog_dir, ec);
    if (ec) return false;

    nlohmann::json json = nlohmann::json::object();
    for (const auto& [image_id, record] : catalog_) {
        json[image_id] = {
            {"event_id", record.event_id},
            {"relative_path", record.rel_path},
            {"created_at_ns", record.created_at_ns},
            {"report_status", record.report_status}
        };
    }
    const fs::path temporary = catalog_dir / "catalog.json.part";
    return write_atomic_file(temporary, catalog_dir / "catalog.json", json.dump(2), false);
}

bool ImageManager::is_safe_image_id(const std::string& image_id) const {
    if (image_id.empty() || image_id.size() > 256) return false;
    for (const unsigned char ch : image_id) {
        if (!(std::isalnum(ch) || ch == '-' || ch == '_')) return false;
    }
    return image_id != "." && image_id != "..";
}

bool ImageManager::is_path_within_base(const fs::path& path) const {
    std::error_code ec;
    const fs::path root = fs::weakly_canonical(fs::path(base_dir_), ec);
    if (ec) return false;
    const fs::path candidate = fs::weakly_canonical(path, ec);
    if (ec) return false;

    auto root_it = root.begin();
    auto candidate_it = candidate.begin();
    for (; root_it != root.end() && candidate_it != candidate.end(); ++root_it, ++candidate_it) {
        if (*root_it != *candidate_it) return false;
    }
    return root_it == root.end();
}

std::string ImageManager::make_image_id(const std::string& event_id, int64_t created_at_ns) const {
    std::string safe;
    safe.reserve(event_id.size());
    for (const unsigned char ch : event_id) {
        safe.push_back((std::isalnum(ch) || ch == '-' || ch == '_') ? static_cast<char>(ch) : '_');
    }
    if (safe.empty()) safe = "event";
    if (safe.size() > 96) safe.resize(96);
    std::ostringstream id;
    id << "img-" << safe << "-" << std::hex << static_cast<uint64_t>(created_at_ns);
    return id.str();
}

std::string ImageManager::date_directory(int64_t created_at_ns) const {
    const std::time_t seconds = static_cast<std::time_t>(created_at_ns / 1000000000);
    std::tm utc{};
    gmtime_r(&seconds, &utc);
    char date[11]{};
    std::strftime(date, sizeof(date), "%Y-%m-%d", &utc);
    return date;
}

av_status ImageManager::save_detection_image(
    const av_frame_desc* frame,
    const av_rect* crop_roi,
    const std::string& event_id,
    ImageRecord* out_record
) {
    if (!frame || event_id.empty() || !out_record) return AV_ERR_INVALID_ARG;

    std::lock_guard<std::mutex> lock(mutex_);
    if (!processor_) return AV_ERR_INVALID_ARG;

    // 1. 调用平台图像处理器完成 ROI 裁剪与 JPEG 硬件/硬件加速压缩编码
    std::vector<uint8_t> jpeg_data;
    const av_status encode_status = processor_->encode_jpeg(frame, crop_roi, 80, jpeg_data);
    if (encode_status != AV_OK) return encode_status;
    if (jpeg_data.empty()) return AV_ERR_INTERNAL;

    // 1.1 并行利用硬件图像处理器编码生成低带宽轻量缩略图（宽度 360px，Q=70）
    std::vector<uint8_t> thumb_jpeg_data;
    processor_->encode_thumbnail_jpeg(frame, 360, 70, thumb_jpeg_data);

    // 2. 生成安全 image_id 并按 UTC 日期构建存储子目录（如 2025-05-18/img-xxx.jpg）
    const auto now_ns = std::chrono::duration_cast<std::chrono::nanoseconds>(
        std::chrono::system_clock::now().time_since_epoch()).count();
    const std::string image_id = make_image_id(event_id, now_ns);
    const std::string relative_path = date_directory(now_ns) + "/" + image_id + ".jpg";
    const std::string thumb_rel_path = date_directory(now_ns) + "/" + image_id + "_thumb.jpg";
    const fs::path final_path = fs::path(base_dir_) / relative_path;
    const fs::path thumb_final_path = fs::path(base_dir_) / thumb_rel_path;
    const fs::path temporary = fs::path(base_dir_) / ".tmp" / (image_id + ".jpg.part");
    const fs::path thumb_temporary = fs::path(base_dir_) / ".tmp" / (image_id + "_thumb.jpg.part");

    // 3. 校验路径安全性，防止目录穿越
    std::error_code ec;
    fs::create_directories(final_path.parent_path(), ec);
    if (ec || !is_path_within_base(final_path)) return AV_ERR_INVALID_ARG;
    fs::create_directories(temporary.parent_path(), ec);
    if (ec) return AV_ERR_INTERNAL;

    // 4. 原子持久化 JPEG 原图与缩略图文件（临时文件写入 + fsync + rename）
    if (!write_atomic_file(temporary, final_path,
                           std::string(reinterpret_cast<const char*>(jpeg_data.data()), jpeg_data.size()), true)) {
        return AV_ERR_INTERNAL;
    }
    if (!thumb_jpeg_data.empty()) {
        write_atomic_file(thumb_temporary, thumb_final_path,
                          std::string(reinterpret_cast<const char*>(thumb_jpeg_data.data()), thumb_jpeg_data.size()), true);
    }

    // 5. 更新本地内存 Catalog 记录并原子刷盘 catalog.json
    ImageRecord record{
        .image_id = image_id,
        .event_id = event_id,
        .rel_path = relative_path,
        .created_at_ns = now_ns,
        .report_status = "unreported"
    };
    catalog_[image_id] = record;
    if (!persist_catalog_locked()) {
        catalog_.erase(image_id);
        fs::remove(final_path, ec);
        return AV_ERR_INTERNAL;
    }

    *out_record = std::move(record);
    return AV_OK;
}

av_status ImageManager::mark_reported(const std::string& image_id) {
    if (!is_safe_image_id(image_id)) return AV_ERR_INVALID_ARG;
    std::lock_guard<std::mutex> lock(mutex_);
    const auto it = catalog_.find(image_id);
    if (it == catalog_.end()) return AV_ERR_INVALID_ARG;
    const std::string previous = it->second.report_status;
    it->second.report_status = "reported";
    if (!persist_catalog_locked()) {
        it->second.report_status = previous;
        return AV_ERR_INTERNAL;
    }
    return AV_OK;
}

av_status ImageManager::delete_image(const std::string& image_id) {
    if (!is_safe_image_id(image_id)) return AV_ERR_INVALID_ARG;
    const auto status = delete_image_with_status(image_id);
    return status == ImageDeleteStatus::FAILED ? AV_ERR_INTERNAL : AV_OK;
}

ImageDeleteStatus ImageManager::delete_image_with_status(const std::string& image_id) {
    if (!is_safe_image_id(image_id)) return ImageDeleteStatus::FAILED;
    std::lock_guard<std::mutex> lock(mutex_);
    const auto it = catalog_.find(image_id);
    if (it == catalog_.end()) return ImageDeleteStatus::ALREADY_ABSENT;

    const fs::path path = fs::path(base_dir_) / it->second.rel_path;
    if (!is_path_within_base(path)) return ImageDeleteStatus::FAILED;
    std::error_code ec;
    const bool existed = fs::exists(path, ec);
    if (ec) return ImageDeleteStatus::FAILED;
    if (existed && !fs::remove(path, ec)) return ImageDeleteStatus::FAILED;
    if (ec) return ImageDeleteStatus::FAILED;

    // 级联删除对应的缩略图文件（若存在）
    std::string thumb_rel = it->second.rel_path;
    const auto dot_pos = thumb_rel.rfind('.');
    if (dot_pos != std::string::npos) {
        thumb_rel = thumb_rel.substr(0, dot_pos) + "_thumb" + thumb_rel.substr(dot_pos);
        const fs::path thumb_path = fs::path(base_dir_) / thumb_rel;
        if (is_path_within_base(thumb_path)) {
            fs::remove(thumb_path, ec);
        }
    }

    const ImageRecord record = it->second;
    catalog_.erase(it);
    if (!persist_catalog_locked()) {
        catalog_.emplace(record.image_id, record);
        return ImageDeleteStatus::FAILED;
    }
    return existed ? ImageDeleteStatus::DELETED : ImageDeleteStatus::ALREADY_ABSENT;
}

std::vector<std::pair<std::string, bool>> ImageManager::batch_delete_images(const std::vector<std::string>& image_ids) {
    std::vector<std::pair<std::string, bool>> results;
    results.reserve(image_ids.size());
    for (const auto& id : image_ids) {
        results.emplace_back(id, delete_image(id) == AV_OK);
    }
    return results;
}

std::vector<std::pair<std::string, ImageDeleteStatus>> ImageManager::reconcile_images(
    const std::vector<std::string>& retain_image_ids) {
    std::unordered_set<std::string> retained(retain_image_ids.begin(), retain_image_ids.end());
    std::vector<std::string> candidates;
    {
        std::lock_guard<std::mutex> lock(mutex_);
        for (const auto& [image_id, record] : catalog_) {
            if (retained.find(image_id) == retained.end() && record.report_status != "unreported") {
                candidates.push_back(image_id);
            }
        }
    }

    std::vector<std::pair<std::string, ImageDeleteStatus>> results;
    results.reserve(candidates.size());
    for (const auto& image_id : candidates) {
        results.emplace_back(image_id, delete_image_with_status(image_id));
    }
    return results;
}

std::vector<std::string> ImageManager::scan_orphan_images(const std::vector<std::string>& active_db_image_ids) {
    std::lock_guard<std::mutex> lock(mutex_);
    std::unordered_set<std::string> active(active_db_image_ids.begin(), active_db_image_ids.end());
    std::unordered_set<std::string> found;

    for (const auto& [image_id, record] : catalog_) {
        if (active.find(image_id) == active.end()) found.insert(image_id);
    }

    std::error_code ec;
    if (fs::exists(base_dir_, ec)) {
        for (fs::recursive_directory_iterator it(base_dir_, ec), end; it != end && !ec; it.increment(ec)) {
            const auto status = it->symlink_status();
            if (fs::is_symlink(status) || !fs::is_regular_file(status) || it->path().extension() != ".jpg") continue;
            if (it->path().parent_path().filename() == ".tmp" || it->path().parent_path().filename() == "catalog") continue;
            if (!is_path_within_base(it->path())) continue;
            const std::string image_id = it->path().stem().string();
            if (is_safe_image_id(image_id) && active.find(image_id) == active.end()) found.insert(image_id);
        }
    }
    return {found.begin(), found.end()};
}

} // namespace argus::core
