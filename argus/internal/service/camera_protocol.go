package service

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"argus/app/internal/model"
)

// CameraProtocol 摄像头协议适配器：协议专属的 URL 校验与后续测活/运行能力入口。
// 核心 CRUD/测活通过协议注册表选择实现，后续新增协议只增加适配器。
type CameraProtocol interface {
	// ValidateURL 校验协议专属 URL；返回 nil 表示合法。
	ValidateURL(raw string) error
}

// ProtocolRegistry 摄像头协议注册表。
type ProtocolRegistry struct {
	protocols map[string]CameraProtocol
}

// NewProtocolRegistry 创建协议注册表并注册内置协议。
func NewProtocolRegistry() *ProtocolRegistry {
	return &ProtocolRegistry{
		protocols: map[string]CameraProtocol{
			model.CameraProtocolRTSP: rtspProtocol{},
		},
	}
}

// Lookup 返回指定协议的适配器；未注册返回 (nil, false)。
func (r *ProtocolRegistry) Lookup(protocol string) (CameraProtocol, bool) {
	p, ok := r.protocols[strings.ToLower(strings.TrimSpace(protocol))]
	return p, ok
}

// rtspProtocol 实现 RTSP 协议适配器（MVP 唯一协议）。
type rtspProtocol struct{}

// ValidateURL 校验 RTSP URL：
//   - 去除首尾空白；总长度 ≤ 2048（与表列一致）；
//   - 拒绝 fragment（#）与控制字符；
//   - 百分号编码必须是合法的两位十六进制数字；
//   - scheme 必须是 rtsp（大小写不敏感）、host 非空。
func (rtspProtocol) ValidateURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("url is empty")
	}
	if len(raw) > 2048 {
		return fmt.Errorf("url length %d exceeds 2048", len(raw))
	}
	if strings.Contains(raw, "#") {
		return fmt.Errorf("url must not contain fragment")
	}
	if err := validatePercentEncoding(raw); err != nil {
		return err
	}
	for _, ch := range raw {
		if unicode.IsControl(ch) {
			return fmt.Errorf("url must not contain control characters")
		}
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "rtsp") {
		return fmt.Errorf("unsupported scheme %q, want rtsp", parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("url host is empty")
	}
	return nil
}

// validatePercentEncoding 校验 URL 中所有百分号转义都是合法的两位十六进制。
func validatePercentEncoding(raw string) error {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '%' {
			continue
		}
		if i+2 >= len(raw) {
			return fmt.Errorf("incomplete percent escape at offset %d", i)
		}
		if !isHexDigit(raw[i+1]) || !isHexDigit(raw[i+2]) {
			return fmt.Errorf("invalid percent escape at offset %d", i)
		}
	}
	return nil
}

func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}
