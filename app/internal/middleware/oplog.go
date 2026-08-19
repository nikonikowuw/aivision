package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/mask"
	"niko-vue-admin/app/internal/service"
)

const (
	// maxRequestCaptureBytes 限制日志中间件预读的请求体大小；超出后仍完整回放给 handler，日志不记录原文。
	maxRequestCaptureBytes = 1 << 20

	maxOperationLogUsernameLen  = 64
	maxOperationLogModuleLen    = 64
	maxOperationLogActionLen    = 64
	maxOperationLogMethodLen    = 16
	maxOperationLogPathLen      = 255
	maxOperationLogIPLen        = 64
	maxOperationLogUserAgentLen = 255
)

// OplogMiddleware 操作日志全自动采集中间件。
// 拦截 POST/PUT/DELETE 且以 /api 开头的写操作请求（含登录/登出）。
type OplogMiddleware struct {
	srv service.OperationLogService
	log *zap.Logger
}

// NewOplogMiddleware 创建操作日志中间件。
func NewOplogMiddleware(srv service.OperationLogService, log *zap.Logger) *OplogMiddleware {
	if log == nil {
		log = zap.NewNop()
	}
	return &OplogMiddleware{
		srv: srv,
		log: log,
	}
}

// Handler 处理写请求的日志采集与异步落库。
func (m *OplogMiddleware) Handler(c *gin.Context) {
	method := c.Request.Method
	path := c.Request.URL.Path

	// 仅拦截 POST / PUT / DELETE 且路径以 /api 开头的写操作。
	if !isWriteMethod(method) || !isAPIPath(path) {
		c.Next()
		return
	}

	start := time.Now()

	// 只预读有限前缀并在 handler 前回放，避免日志中间件因超大请求体无界分配内存。
	bodyBytes, bodyComplete, bodyErr := captureRequestBody(c)
	if bodyErr != nil {
		m.log.Warn("failed to capture request body", zap.Error(bodyErr), zap.String("path", path))
	}

	// 执行 recovery、统一错误处理和后续业务；它们都位于 Oplog 内层，返回时状态码已经最终确定。
	c.Next()

	duration := time.Since(start).Milliseconds()
	statusCode := c.Writer.Status()

	var userID uint64
	var username string

	if identity, ok := IdentityFromContext(c); ok {
		userID = identity.UserID
		username = identity.Username
	} else if bodyComplete && len(bodyBytes) > 0 {
		// 登录失败等场景下未生成 Identity，尝试从完整 JSON 请求体提取 username。
		username = extractUsernameFromBody(bodyBytes)
	}

	maskedBody := ""
	if len(bodyBytes) > 0 {
		if bodyComplete {
			maskedBody = mask.MaskJSONBody(bodyBytes, mask.DefaultMaxBodyLen)
		} else {
			// 不对不完整的前缀做原文截断，避免密码出现在无法解析的片段中。
			maskedBody = mask.OmittedBody
		}
	}

	module := mask.Truncate(inferModule(c), maxOperationLogModuleLen)
	storedPath := mask.Truncate(path, maxOperationLogPathLen)
	action := mask.Truncate(inferAction(c, method, storedPath), maxOperationLogActionLen)

	logItem := &model.OperationLog{
		CreatedAt:  start,
		UserID:     userID,
		Username:   mask.Truncate(username, maxOperationLogUsernameLen),
		Module:     module,
		Action:     action,
		Method:     mask.Truncate(method, maxOperationLogMethodLen),
		Path:       storedPath,
		Query:      mask.MaskQuery(c.Request.URL.RawQuery, mask.DefaultMaxBodyLen),
		Body:       maskedBody,
		StatusCode: statusCode,
		DurationMs: duration,
		IP:         mask.Truncate(c.ClientIP(), maxOperationLogIPLen),
		UserAgent:  mask.Truncate(c.Request.UserAgent(), maxOperationLogUserAgentLen),
	}

	// 异步落库（带 recover，写失败仅 zap warn，不影响客户端响应）。
	go func(item *model.OperationLog) {
		defer func() {
			if r := recover(); r != nil {
				m.log.Warn("panic in operation log recording", zap.Any("recover", r))
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := m.srv.Record(ctx, item); err != nil {
			m.log.Warn("failed to record operation log", zap.Error(err), zap.String("path", item.Path))
		}
	}(logItem)
}

func captureRequestBody(c *gin.Context) ([]byte, bool, error) {
	body := c.Request.Body
	if body == nil {
		return nil, true, nil
	}

	captured, err := io.ReadAll(io.LimitReader(body, int64(maxRequestCaptureBytes)+1))
	complete := err == nil && len(captured) <= maxRequestCaptureBytes
	logBytes := captured
	if len(logBytes) > maxRequestCaptureBytes {
		logBytes = logBytes[:maxRequestCaptureBytes]
	}

	c.Request.Body = &replayReadCloser{
		Reader: io.MultiReader(bytes.NewReader(captured), body),
		closer: body,
	}
	return logBytes, complete, err
}

type replayReadCloser struct {
	io.Reader
	closer io.Closer
}

func (r *replayReadCloser) Close() error {
	return r.closer.Close()
}

func isWriteMethod(method string) bool {
	return method == "POST" || method == "PUT" || method == "DELETE"
}

func isAPIPath(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/")
}

// actionI18nMap 路由与动作 i18n key 的映射表。
var actionI18nMap = map[string]string{
	// Auth
	"POST /api/auth/login":   "system.log.actionLogin",
	"POST /api/auth/logout":  "system.log.actionLogout",
	"POST /api/auth/refresh": "system.log.actionRefreshToken",

	// User
	"POST /api/user":                   "system.user.addUser",
	"PUT /api/user/:id":                "system.user.editUser",
	"DELETE /api/user/:id":             "system.user.deleteUser",
	"DELETE /api/user/batch":           "system.common.batchDelete",
	"PUT /api/user/batch-status":       "system.user.batchStatus",
	"PUT /api/user/:id/reset-password": "system.user.resetPassword",
	"PUT /api/user/:id/roles":          "system.user.assignRole",
	"PUT /api/user/:id/status":         "system.user.status",

	// Menu
	"POST /api/menu":       "system.menu.addMenu",
	"PUT /api/menu/:id":    "system.menu.editMenu",
	"DELETE /api/menu/:id": "system.menu.deleteMenu",

	// Role
	"POST /api/role":          "system.role.addRole",
	"PUT /api/role/:id":       "system.role.editRole",
	"DELETE /api/role/:id":    "system.role.deleteRole",
	"DELETE /api/role/batch":  "system.common.batchDelete",
	"PUT /api/role/:id/menus": "system.role.assignMenu",

	// Department
	"POST /api/dept":       "system.dept.addDept",
	"PUT /api/dept/:id":    "system.dept.editDept",
	"DELETE /api/dept/:id": "system.dept.deleteDept",
}

// inferAction 根据 Gin FullPath 和 Method 推断语义化的 i18n action key，未匹配则 fallback 到 "Method Path"。
func inferAction(c *gin.Context, method, fallbackPath string) string {
	fullPath := c.FullPath()
	if fullPath != "" {
		key := strings.ToUpper(method) + " " + fullPath
		if actionKey, ok := actionI18nMap[key]; ok {
			return actionKey
		}
	}
	return method + " " + fallbackPath
}

// inferModule 推断模块名称，优先使用 FullPath，退化使用 URL Path。
func inferModule(c *gin.Context) string {
	target := c.FullPath()
	if target == "" {
		target = c.Request.URL.Path
	}

	if target == "/api" || target == "/api/" {
		return "system"
	}

	target = strings.TrimPrefix(target, "/api/")
	parts := strings.Split(target, "/")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return "system"
}

// extractUsernameFromBody 从 JSON 请求体提取 username 字段。
func extractUsernameFromBody(body []byte) string {
	var payload struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Username != "" {
		return payload.Username
	}
	return ""
}
