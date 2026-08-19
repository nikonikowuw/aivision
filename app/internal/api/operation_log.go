package api

import (
	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/response"
	"niko-vue-admin/app/internal/service"
)

// OperationLogHandler 操作日志处理器。
type OperationLogHandler struct {
	srv service.OperationLogService
}

// NewOperationLogHandler 创建 OperationLogHandler 实例。
func NewOperationLogHandler(srv service.OperationLogService) *OperationLogHandler {
	return &OperationLogHandler{srv: srv}
}

// GetPage 获取操作日志分页列表 (GET /api/oplog/page)
func (h *OperationLogHandler) GetPage(c *gin.Context) {
	var query service.LogPageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}

	res, err := h.srv.GetPage(c.Request.Context(), &query)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, res)
}

// GetByID 获取操作日志详情 (GET /api/oplog/:id)
func (h *OperationLogHandler) GetByID(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}

	logItem, err := h.srv.GetByID(c.Request.Context(), id)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, logItem)
}
