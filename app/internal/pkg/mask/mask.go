// Package mask 提供敏感字段脱敏与长文本截断工具。
package mask

import (
	"encoding/json"
	"net/url"
	"strings"
)

// DefaultMaxBodyLen 默认请求体最大记录长度（4KB = 4096 字符）。
const DefaultMaxBodyLen = 4096

// OmittedBody 表示请求体无法安全脱敏时不记录原文。
const OmittedBody = "[request body omitted]"

// OmittedQuery 表示查询参数无法安全解析时不记录原文。
const OmittedQuery = "[query omitted]"

// sensitiveKeyKeywords 敏感字段名关键字（小写，不带下划线或连字符以便统一匹配）。
var sensitiveKeyKeywords = []string{
	"password",
	"token",
	"secret",
	"authorization",
	"pwd",
}

// isSensitiveKey 判断给定的 key 是否属于敏感字段。
func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(key)
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")

	for _, kw := range sensitiveKeyKeywords {
		if strings.Contains(normalized, kw) {
			return true
		}
	}
	return false
}

// MaskData 递归对 JSON 树状数据进行脱敏。
func MaskData(val any) any {
	switch v := val.(type) {
	case map[string]any:
		res := make(map[string]any, len(v))
		for k, item := range v {
			if isSensitiveKey(k) {
				res[k] = "***"
			} else {
				res[k] = MaskData(item)
			}
		}
		return res
	case []any:
		res := make([]any, len(v))
		for i, item := range v {
			res[i] = MaskData(item)
		}
		return res
	default:
		return v
	}
}

// Truncate 截断字符串并在超长时追加省略号。
func Truncate(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// MaskJSONBody 对请求体进行 JSON 敏感字段脱敏，并限制最大输出长度。
func MaskJSONBody(body []byte, maxLen int) string {
	if len(body) == 0 {
		return ""
	}
	if maxLen <= 0 {
		maxLen = DefaultMaxBodyLen
	}

	var data any
	if err := json.Unmarshal(body, &data); err != nil {
		// 解析失败时不能证明内容安全，固定记录 omission 标记，避免密码或 token 泄露。
		return OmittedBody
	}

	masked := MaskData(data)
	out, err := json.Marshal(masked)
	if err != nil {
		return OmittedBody
	}

	return Truncate(string(out), maxLen)
}

// MaskQuery 对 URL 查询参数进行敏感字段脱敏，并限制最大记录长度。
func MaskQuery(rawQuery string, maxLen int) string {
	if rawQuery == "" {
		return ""
	}
	if maxLen <= 0 {
		maxLen = DefaultMaxBodyLen
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		// 解析失败时不能安全识别敏感参数，固定记录 omission 标记。
		return OmittedQuery
	}
	for key := range values {
		if isSensitiveKey(key) {
			values[key] = []string{"***"}
		}
	}
	return Truncate(values.Encode(), maxLen)
}
