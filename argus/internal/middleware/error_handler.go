// Package middleware 提供统一错误处理、认证鉴权、操作日志与开放接口中间件。
package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/response"
)

// NewErrorHandler 统一错误处理中间件：所有失败响应统一由这里输出，handler 不各自拼接。
// 错误码与文案唯一来自 errno，文案按请求语言 i18n。
// 挂载在 recovery 之后、业务路由之前。
// 对非业务错误（内部错误/系统异常）输出 zap 结构化错误日志。
func NewErrorHandler(log *zap.Logger) gin.HandlerFunc {
	if log == nil {
		log = zap.NewNop()
	}
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
			case errno.CodeInvalidParam, errno.CodeFileTooLarge, errno.CodeFileTypeNotAllowed, errno.CodeNetworkInvalidConfig, errno.CodeNetworkGatewayPoolInvalid, errno.CodeStorageInvalidConfig:
				status = http.StatusBadRequest
			case errno.CodeNotFound, errno.CodeNetworkTransactionNotFound, errno.CodeTaskNotFound, errno.CodeInstanceNotFound:
				status = http.StatusNotFound
			case errno.CodeNetworkTransactionPending, errno.CodeNetworkTransactionExpired, errno.CodeNetworkInterfaceNotManaged, errno.CodeNetworkOwnershipConflict, errno.CodeNetworkExternalDrift, errno.CodeNetworkBondSlaveInvalid, errno.CodeNetworkBondModeConflict, errno.CodeNetworkDhcpServerConflict,
				errno.CodeCameraInUse, errno.CodeTaskAlreadyExists:
				status = http.StatusConflict
			case errno.CodeNetworkUnsupported, errno.CodeNetworkApplyFailed, errno.CodeNetworkRecoveryFailed, errno.CodeNetworkStateCorrupt, errno.CodeNetworkNotReady, errno.CodeNetworkLacpNegotiationFailed:
				status = http.StatusServiceUnavailable
			case errno.CodeResourceExceeded, errno.CodeFPSTierExceeded, errno.CodeRuleOutOfBounds, errno.CodeRuleTooFewPoints, errno.CodeRuleSelfIntersect:
				status = http.StatusBadRequest
			case errno.CodeInternal:
				status = http.StatusInternalServerError
			}

			if status >= http.StatusInternalServerError || e.Code == errno.CodeInternal {
				log.Error("business internal error",
					zap.String("method", c.Request.Method),
					zap.String("path", c.Request.URL.Path),
					zap.String("query", c.Request.URL.RawQuery),
					zap.String("client_ip", c.ClientIP()),
					zap.Int("code", e.Code),
					zap.Error(lastErr),
				)
			} else {
				log.Debug("business error handled",
					zap.String("method", c.Request.Method),
					zap.String("path", c.Request.URL.Path),
					zap.Int("status", status),
					zap.Int("code", e.Code),
					zap.Error(lastErr),
				)
			}

			// 携带文案插值参数的错误（如配额拒绝的三个数字）随响应输出。
			response.WriteFail(c, status, e.Code, e.Args()...)
			return
		}

		// 非业务错误：对外统一为内部错误，不泄露内部细节；控制台记录完整错误信息供排查
		log.Error("unhandled internal error",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("query", c.Request.URL.RawQuery),
			zap.String("client_ip", c.ClientIP()),
			zap.Error(lastErr),
		)
		response.WriteFail(c, http.StatusInternalServerError, errno.CodeInternal)
	}
}

// ErrorHandler 供单元测试或无 logger 注入场景调用。
func ErrorHandler() gin.HandlerFunc {
	return NewErrorHandler(nil)
}
