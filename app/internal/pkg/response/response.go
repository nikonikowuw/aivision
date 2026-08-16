// Package response 定义统一响应体 {code,data,message}。
package response

import "niko-vue-admin/app/internal/pkg/errno"

// Result 统一响应体，对齐 vben defaultResponseInterceptor（codeField/dataField/successCode=0）。
type Result struct {
	Code    int    `json:"code"`
	Data    any    `json:"data"`
	Message string `json:"message"`
}

// OK 成功响应：code=0，文案来自 errno（唯一文案源）。
// 成功文案不随请求语言变化（各语言下均为 "ok"），取默认语言。
func OK(data any) Result {
	return Result{Code: errno.CodeOK, Data: data, Message: errno.Message(errno.DefaultLang, errno.CodeOK)}
}

// Fail 业务失败响应：code=<errno>，data 为 null。
func Fail(code int, message string) Result {
	return Result{Code: code, Data: nil, Message: message}
}
