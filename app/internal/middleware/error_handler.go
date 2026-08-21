package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/response"
)

// ErrorHandler 统一错误处理中间件：所有失败响应统一由这里输出，handler 不各自拼接。
// 错误码与文案唯一来自 errno，文案按请求语言 i18n。
// 挂载在 recovery 之后、业务路由之前。
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}

		lastErr := c.Errors.Last().Err

		var e *errno.Error
		if errors.As(lastErr, &e) {
			// 业务失败默认 HTTP 200；认证、权限和参数错误使用对应 HTTP 状态码。
			status := http.StatusOK
			switch e.Code {
			case errno.CodeUnauthorized:
				status = http.StatusUnauthorized
			case errno.CodeForbidden:
				status = http.StatusForbidden
			case errno.CodeInvalidParam, errno.CodeFileTooLarge, errno.CodeFileTypeNotAllowed:
				status = http.StatusBadRequest
			}
			response.WriteFail(c, status, e.Code)
			return
		}

		// 非业务错误：对外统一为内部错误，不泄露内部细节；原始错误已在 c.Errors 供日志中间件读取。
		response.WriteFail(c, http.StatusInternalServerError, errno.CodeInternal)
	}
}
