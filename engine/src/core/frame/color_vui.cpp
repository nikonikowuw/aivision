#include "aivision/core/color_vui.hpp"

#include <optional>
#include <vector>

namespace aivision::core {
namespace {

class BitReader {
public:
    explicit BitReader(const std::vector<uint8_t>& bytes) : bytes_(bytes) {}

    bool read_bit(bool& value) {
        if (bit_offset_ >= bytes_.size() * 8) return false;
        value = (bytes_[bit_offset_ / 8] >> (7 - (bit_offset_ % 8))) & 1U;
        ++bit_offset_;
        return true;
    }

    bool read_bits(unsigned count, uint32_t& value) {
        if (count > 32 || bit_offset_ + count > bytes_.size() * 8) return false;
        value = 0;
        for (unsigned i = 0; i < count; ++i) {
            bool bit = false;
            if (!read_bit(bit)) return false;
            value = (value << 1) | static_cast<uint32_t>(bit);
        }
        return true;
    }

    bool read_ue(uint32_t& value) {
        unsigned leading_zero_bits = 0;
        bool bit = false;
        while (leading_zero_bits < 32) {
            if (!read_bit(bit)) return false;
            if (bit) break;
            ++leading_zero_bits;
        }
        if (leading_zero_bits == 32) return false;
        uint32_t suffix = 0;
        if (!read_bits(leading_zero_bits, suffix)) return false;
        if (leading_zero_bits == 32) return false;
        value = ((uint32_t{1} << leading_zero_bits) - 1U) + suffix;
        return true;
    }

    bool read_se(int32_t& value) {
        uint32_t code = 0;
        if (!read_ue(code)) return false;
        value = (code & 1U) ? static_cast<int32_t>((code + 1) / 2) : -static_cast<int32_t>(code / 2);
        return true;
    }

private:
    const std::vector<uint8_t>& bytes_;
    size_t bit_offset_ = 0;
};

std::vector<uint8_t> to_rbsp(const uint8_t* data, size_t size, size_t nal_header_bytes) {
    std::vector<uint8_t> rbsp;
    if (!data || size == 0) return rbsp;
    size_t offset = 0;
    if (offset + 4 <= size && data[0] == 0 && data[1] == 0 && data[2] == 0 && data[3] == 1) offset = 4;
    else if (offset + 3 <= size && data[0] == 0 && data[1] == 0 && data[2] == 1) offset = 3;
    if (offset + nal_header_bytes > size) return {};
    offset += nal_header_bytes;
    rbsp.reserve(size - offset);
    unsigned zero_count = 0;
    for (; offset < size; ++offset) {
        const uint8_t byte = data[offset];
        if (zero_count >= 2 && byte == 3) {
            zero_count = 0;
            continue;
        }
        rbsp.push_back(byte);
        zero_count = byte == 0 ? zero_count + 1 : 0;
    }
    return rbsp;
}

void apply_colour_description(ColorVUIInfo& info, uint32_t primaries, uint32_t transfer,
                              uint32_t matrix, bool full_range) {
    if (primaries == 9) info.primaries = AV_COLOR_PRIM_BT2020;
    else if (primaries == 1) info.primaries = AV_COLOR_PRIM_BT709;
    if (transfer == 13) info.transfer = AV_COLOR_TRC_IEC61966_2_1;
    else if (transfer == 1) info.transfer = AV_COLOR_TRC_BT709;
    if (matrix == 9) info.matrix = AV_COLOR_MAT_BT2020_NCL;
    else if (matrix == 1) info.matrix = AV_COLOR_MAT_BT709;
    info.range = full_range ? AV_COLOR_RANGE_FULL : AV_COLOR_RANGE_LIMITED;
}

bool skip_scaling_list(BitReader& reader, unsigned count) {
    int32_t delta = 0;
    int32_t last_scale = 8;
    int32_t next_scale = 8;
    for (unsigned i = 0; i < count; ++i) {
        if (next_scale != 0) {
            if (!reader.read_se(delta)) return false;
            next_scale = (last_scale + delta + 256) % 256;
        }
        last_scale = next_scale == 0 ? last_scale : next_scale;
    }
    return true;
}

bool skip_h264_vui(BitReader& reader, ColorVUIInfo& info) {
    bool flag = false;
    uint32_t value = 0;
    if (!reader.read_bit(flag)) return false;
    if (flag) {
        if (!reader.read_bits(8, value)) return false;
        if (value == 255 && (!reader.read_bits(16, value) || !reader.read_bits(16, value))) return false;
    }
    if (!reader.read_bit(flag)) return false;
    if (flag && !reader.read_bit(flag)) return false;
    if (!reader.read_bit(flag)) return false;
    if (flag) {
        if (!reader.read_bits(3, value)) return false;
        bool full_range = false;
        if (!reader.read_bit(full_range)) return false;
        bool colour_description = false;
        if (!reader.read_bit(colour_description)) return false;
        if (colour_description) {
            uint32_t primaries = 0, transfer = 0, matrix = 0;
            if (!reader.read_bits(8, primaries) || !reader.read_bits(8, transfer) || !reader.read_bits(8, matrix)) return false;
            apply_colour_description(info, primaries, transfer, matrix, full_range);
        } else {
            info.range = full_range ? AV_COLOR_RANGE_FULL : AV_COLOR_RANGE_LIMITED;
        }
    }
    return true;
}

bool parse_h264(const std::vector<uint8_t>& rbsp, ColorVUIInfo& info) {
    BitReader reader(rbsp);
    uint32_t profile = 0, ignored = 0;
    bool flag = false;
    if (!reader.read_bits(8, profile) || !reader.read_bits(8, ignored) || !reader.read_bits(8, ignored)) return false;
    if (!reader.read_ue(ignored)) return false;
    const bool high_profile = profile == 100 || profile == 110 || profile == 122 || profile == 244 ||
                              profile == 44 || profile == 83 || profile == 86 || profile == 118 ||
                              profile == 128 || profile == 138 || profile == 139 || profile == 134;
    if (high_profile) {
        uint32_t chroma_format = 0;
        if (!reader.read_ue(chroma_format)) return false;
        if (chroma_format == 3 && !reader.read_bit(flag)) return false;
        if (!reader.read_ue(ignored) || !reader.read_ue(ignored)) return false;
        if (!reader.read_bit(flag) || !reader.read_bit(flag)) return false;
        if (flag) {
            const unsigned count = chroma_format != 3 ? 8 : 12;
            for (unsigned i = 0; i < count; ++i) {
                if (!reader.read_bit(flag)) return false;
                if (flag && !skip_scaling_list(reader, i < 6 ? 16 : 64)) return false;
            }
        }
    }
    if (!reader.read_ue(ignored)) return false;
    uint32_t poc_type = 0;
    if (!reader.read_ue(poc_type)) return false;
    if (poc_type == 0) {
        if (!reader.read_ue(ignored)) return false;
    } else if (poc_type == 1) {
        bool poc_flag = false;
        int32_t signed_value = 0;
        if (!reader.read_bit(poc_flag) || !reader.read_se(signed_value) || !reader.read_se(signed_value) || !reader.read_ue(ignored)) return false;
        for (uint32_t i = 0; i < ignored; ++i) if (!reader.read_se(signed_value)) return false;
    }
    if (!reader.read_ue(ignored) || !reader.read_bit(flag) || !reader.read_ue(ignored) || !reader.read_ue(ignored)) return false;
    bool frame_mbs_only = false;
    if (!reader.read_bit(frame_mbs_only)) return false;
    if (!frame_mbs_only && !reader.read_bit(flag)) return false;
    if (!reader.read_bit(flag)) return false;
    if (!reader.read_bit(flag)) return false;
    if (flag) {
        if (!reader.read_ue(ignored) || !reader.read_ue(ignored) || !reader.read_ue(ignored) || !reader.read_ue(ignored)) return false;
    }
    if (!reader.read_bit(flag)) return false;
    if (!flag) return true;
    info.vui_present = true;
    return skip_h264_vui(reader, info);
}

bool skip_profile_tier_level(BitReader& reader, uint32_t max_sub_layers_minus1) {
    uint32_t ignored = 0;
    bool flag = false;
    if (!reader.read_bits(2, ignored) || !reader.read_bit(flag) || !reader.read_bits(5, ignored) ||
        !reader.read_bits(32, ignored)) return false;
    for (int i = 0; i < 6; ++i) if (!reader.read_bits(8, ignored)) return false;
    if (!reader.read_bits(8, ignored)) return false;
    std::vector<uint8_t> profile_present(max_sub_layers_minus1), level_present(max_sub_layers_minus1);
    for (uint32_t i = 0; i < max_sub_layers_minus1; ++i) {
        bool profile = false;
        bool level = false;
        if (!reader.read_bit(profile) || !reader.read_bit(level)) return false;
        profile_present[i] = profile;
        level_present[i] = level;
    }
    for (uint32_t i = max_sub_layers_minus1; i < 8; ++i) if (!reader.read_bits(2, ignored)) return false;
    for (uint32_t i = 0; i < max_sub_layers_minus1; ++i) {
        if (profile_present[i]) {
            if (!reader.read_bits(2, ignored) || !reader.read_bit(flag) || !reader.read_bits(5, ignored) ||
                !reader.read_bits(32, ignored)) return false;
            for (int j = 0; j < 6; ++j) if (!reader.read_bits(8, ignored)) return false;
        }
        if (level_present[i] && !reader.read_bits(8, ignored)) return false;
    }
    return true;
}

bool skip_h265_st_ref_pic_sets(BitReader& reader, uint32_t count) {
    std::vector<uint32_t> delta_poc_counts(count, 0);
    for (uint32_t i = 0; i < count; ++i) {
        bool predicted = false;
        if (i != 0 && !reader.read_bit(predicted)) return false;
        if (i != 0 && predicted) {
            uint32_t delta_idx_minus1 = 0;
            if (!reader.read_ue(delta_idx_minus1)) return false;
            const uint32_t reference_index = i - 1 - std::min(delta_idx_minus1, i - 1);
            bool delta_rps_sign = false;
            uint32_t abs_delta_rps_minus1 = 0;
            if (!reader.read_bit(delta_rps_sign) || !reader.read_ue(abs_delta_rps_minus1)) return false;
            uint32_t current_count = 0;
            for (uint32_t j = 0; j <= delta_poc_counts[reference_index]; ++j) {
                bool used_by_current = false;
                if (!reader.read_bit(used_by_current)) return false;
                bool use_delta = true;
                if (!used_by_current && !reader.read_bit(use_delta)) return false;
                if (used_by_current || use_delta) ++current_count;
            }
            delta_poc_counts[i] = current_count;
        } else {
            uint32_t negative = 0;
            uint32_t positive = 0;
            if (!reader.read_ue(negative) || !reader.read_ue(positive)) return false;
            delta_poc_counts[i] = negative + positive;
            for (uint32_t j = 0; j < negative; ++j) {
                uint32_t value = 0;
                if (!reader.read_ue(value) || !reader.read_bit(predicted)) return false;
            }
            for (uint32_t j = 0; j < positive; ++j) {
                uint32_t value = 0;
                if (!reader.read_ue(value) || !reader.read_bit(predicted)) return false;
            }
        }
    }
    return true;
}

bool parse_h265(const std::vector<uint8_t>& rbsp, ColorVUIInfo& info) {
    BitReader reader(rbsp);
    uint32_t value = 0;
    bool flag = false;
    if (!reader.read_bits(4, value) || !reader.read_bits(3, value)) return false;
    const uint32_t max_sub_layers_minus1 = value;
    if (!reader.read_bit(flag) || !skip_profile_tier_level(reader, max_sub_layers_minus1) || !reader.read_ue(value) ||
        !reader.read_ue(value)) return false;
    const uint32_t chroma_format = value;
    if (chroma_format == 3 && !reader.read_bit(flag)) return false;
    if (!reader.read_ue(value) || !reader.read_ue(value)) return false;
    if (!reader.read_bit(flag)) return false;
    if (flag && (!reader.read_ue(value) || !reader.read_ue(value) || !reader.read_ue(value) || !reader.read_ue(value))) return false;
    uint32_t bit_depth_luma_minus8 = 0;
    uint32_t bit_depth_chroma_minus8 = 0;
    uint32_t log2_max_pic_order_cnt_lsb_minus4 = 0;
    if (!reader.read_ue(bit_depth_luma_minus8) || !reader.read_ue(bit_depth_chroma_minus8) ||
        !reader.read_ue(log2_max_pic_order_cnt_lsb_minus4)) return false;
    bool ordering_present = false;
    if (!reader.read_bit(ordering_present)) return false;
    const uint32_t first_layer = ordering_present ? 0 : max_sub_layers_minus1;
    for (uint32_t i = first_layer; i <= max_sub_layers_minus1; ++i) {
        if (!reader.read_ue(value) || !reader.read_ue(value) || !reader.read_ue(value)) return false;
    }
    for (int i = 0; i < 6; ++i) if (!reader.read_ue(value)) return false;
    bool scaling_list_enabled = false;
    bool amp_enabled = false;
    bool sao_enabled = false;
    if (!reader.read_bit(scaling_list_enabled) || !reader.read_bit(amp_enabled) || !reader.read_bit(sao_enabled)) return false;
    if (scaling_list_enabled) {
        bool scaling_data_present = false;
        if (!reader.read_bit(scaling_data_present)) return false;
        if (scaling_data_present) {
        // Scaling-list data is not needed for VUI; parsing it fully is required to reach VUI.
        for (uint32_t size_id = 0; size_id < 4; ++size_id) {
            const uint32_t matrix_count = size_id == 3 ? 2 : 6;
            const uint32_t coefficient_count = size_id == 0 ? 16 : size_id == 1 ? 64 : 256;
            for (uint32_t matrix = 0; matrix < matrix_count; ++matrix) {
                bool pred = false;
                if (!reader.read_bit(pred)) return false;
                if (!pred) {
                    if (!reader.read_ue(value)) return false;
                } else {
                    if (size_id > 1) {
                        int32_t delta = 0;
                        if (!reader.read_se(delta)) return false;
                    }
                    int32_t delta = 0;
                    for (uint32_t coefficient = 0; coefficient < coefficient_count; ++coefficient) {
                        if (!reader.read_se(delta)) return false;
                    }
                }
            }
        }
        }
    }
    bool pcm_enabled = false;
    if (!reader.read_bit(pcm_enabled)) return false;
    if (pcm_enabled && (!reader.read_bits(4, value) || !reader.read_bits(4, value) ||
        !reader.read_ue(value) || !reader.read_ue(value) || !reader.read_bit(flag))) return false;
    if (!reader.read_ue(value)) return false;
    const uint32_t short_term_ref_pic_sets = value;
    if (!skip_h265_st_ref_pic_sets(reader, short_term_ref_pic_sets)) return false;
    if (!reader.read_bit(flag)) return false;
    if (flag) {
        if (!reader.read_ue(value)) return false;
        const uint32_t long_term_ref_pics = value;
        for (uint32_t i = 0; i < long_term_ref_pics; ++i) {
            if (!reader.read_bits(log2_max_pic_order_cnt_lsb_minus4 + 4, value) || !reader.read_bit(flag)) return false;
        }
    }
    if (!reader.read_bit(flag) || !reader.read_bit(flag) || !reader.read_bit(flag)) return false;
    if (!flag) return true;
    info.vui_present = true;
    return skip_h264_vui(reader, info);
}

} // namespace

ColorVUIInfo ColorVUIParser::parse_h264_sps(const uint8_t* sps_data, size_t size) {
    ColorVUIInfo info;
    const auto rbsp = to_rbsp(sps_data, size, 1);
    if (rbsp.empty() || !parse_h264(rbsp, info)) {
        info.vui_present = false;
    }
    return info;
}

ColorVUIInfo ColorVUIParser::parse_h265_sps(const uint8_t* sps_data, size_t size) {
    ColorVUIInfo info;
    const auto rbsp = to_rbsp(sps_data, size, 2);
    if (rbsp.empty() || !parse_h265(rbsp, info)) {
        info.vui_present = false;
    }
    return info;
}

} // namespace aivision::core
