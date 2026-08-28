// Package response 定义统一响应体 {code,data,message}。
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/pkg/errno"
)

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

// Success 往 gin Context 写入成功 HTTP 200 响应。
// 失败响应统一由错误处理中间件输出，这里只负责成功。
func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, OK(data))
}

// WriteFail 按请求语言输出统一失败响应，供错误中间件 / NoRoute / NoMethod / Recovery 复用。
// args 可选：插值到 errno 文案模板占位符的参数（见 errno.MessageWithArgs，
// 如 CodeResourceExceeded 的已用/申请/上限三个数字）。
func WriteFail(c *gin.Context, status, code int, args ...any) {
	lang := errno.LangFromHeader(c.GetHeader("Accept-Language"))
	c.JSON(status, Fail(code, errno.MessageWithArgs(lang, code, args...)))
}
