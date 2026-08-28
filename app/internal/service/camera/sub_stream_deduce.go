package camera

import (
	"net/url"
	"regexp"
	"strings"
)

// SubStreamCandidate 子码流推导候选
type SubStreamCandidate struct {
	Brand       string `json:"brand"`
	SubURL      string `json:"subUrl"`
	Description string `json:"description"`
}

// DeduceSubStream 根据主流 RTSP URL 规则自动推导子码流候选地址
func DeduceSubStream(mainURL string) []SubStreamCandidate {
	if mainURL == "" {
		return nil
	}

	u, err := url.Parse(mainURL)
	if err != nil {
		return nil
	}

	path := u.Path
	rawQuery := u.RawQuery

	var candidates []SubStreamCandidate

	// 1. 海康威视 / 天地伟业 (Hikvision / Tiandy / ISAPI)
	// 主码流通常为 /Streaming/Channels/101 或 /h264/ch1/main/av_stream 或 /ch1/main/av_stream
	if strings.Contains(path, "/Streaming/Channels/") {
		re := regexp.MustCompile(`/Streaming/Channels/(\d+)01`)
		if re.MatchString(path) {
			subPath := re.ReplaceAllString(path, `/Streaming/Channels/${1}02`)
			subU := *u
			subU.Path = subPath
			candidates = append(candidates, SubStreamCandidate{
				Brand:       "Hikvision",
				SubURL:      subU.String(),
				Description: "海康威视标准子码流 (Channel 102)",
			})
		}
	} else if strings.Contains(path, "/main/av_stream") {
		subPath := strings.Replace(path, "/main/av_stream", "/sub/av_stream", 1)
		subU := *u
		subU.Path = subPath
		brand := "Hikvision"
		desc := "海康威视/天地伟业子码流 (/sub/av_stream)"
		if strings.Contains(path, "/ch1/main/av_stream") && !strings.Contains(path, "/h264/") {
			brand = "Tiandy/Hikvision"
		}
		candidates = append(candidates, SubStreamCandidate{
			Brand:       brand,
			SubURL:      subU.String(),
			Description: desc,
		})
	}

	// 2. 大华 (Dahua)
	// 主码流通常为 /cam/realmonitor?channel=1&subtype=0
	if strings.Contains(path, "/cam/realmonitor") || strings.Contains(rawQuery, "subtype=0") {
		subQuery := strings.Replace(rawQuery, "subtype=0", "subtype=1", 1)
		if !strings.Contains(rawQuery, "subtype=") {
			if subQuery != "" {
				subQuery += "&subtype=1"
			} else {
				subQuery = "subtype=1"
			}
		}
		subU := *u
		subU.RawQuery = subQuery
		candidates = append(candidates, SubStreamCandidate{
			Brand:       "Dahua",
			SubURL:      subU.String(),
			Description: "大华标准子码流 (subtype=1)",
		})
	}

	// 3. 宇视 (Uniview)
	// 主码流通常为 /video1 或 /unicast/c1/s0/live
	if strings.Contains(path, "/video1") {
		subPath := strings.Replace(path, "/video1", "/video2", 1)
		subU := *u
		subU.Path = subPath
		candidates = append(candidates, SubStreamCandidate{
			Brand:       "Uniview",
			SubURL:      subU.String(),
			Description: "宇视标准子码流 (/video2)",
		})
	} else if strings.Contains(path, "/s0/live") {
		subPath := strings.Replace(path, "/s0/live", "/s1/live", 1)
		subU := *u
		subU.Path = subPath
		candidates = append(candidates, SubStreamCandidate{
			Brand:       "Uniview",
			SubURL:      subU.String(),
			Description: "宇视标准子码流 (s1/live)",
		})
	}

	// 4. TP-Link / 水星 (Mercury)
	// 主码流通常为 /stream1
	if strings.Contains(path, "/stream1") {
		subPath := strings.Replace(path, "/stream1", "/stream2", 1)
		subU := *u
		subU.Path = subPath
		candidates = append(candidates, SubStreamCandidate{
			Brand:       "TP-Link",
			SubURL:      subU.String(),
			Description: "TP-Link/水星标准子码流 (/stream2)",
		})
	}

	// 5. 通用后缀替换 (live/ch0 -> live/ch1, /main -> /sub, /0 -> /1)
	if len(candidates) == 0 {
		if strings.Contains(path, "/main") {
			subPath := strings.Replace(path, "/main", "/sub", 1)
			subU := *u
			subU.Path = subPath
			candidates = append(candidates, SubStreamCandidate{
				Brand:       "Generic",
				SubURL:      subU.String(),
				Description: "通用子码流 (/sub 替换)",
			})
		} else if strings.HasSuffix(path, "0") {
			subPath := path[:len(path)-1] + "1"
			subU := *u
			subU.Path = subPath
			candidates = append(candidates, SubStreamCandidate{
				Brand:       "Generic",
				SubURL:      subU.String(),
				Description: "通用子码流 (通道 0 -> 1)",
			})
		}
	}

	return candidates
}

// DeducePrimarySubStream 获取第一顺位推荐的子码流地址（若无可推导则返回空）
func DeducePrimarySubStream(mainURL string) string {
	candidates := DeduceSubStream(mainURL)
	if len(candidates) > 0 {
		return candidates[0].SubURL
	}
	return ""
}
