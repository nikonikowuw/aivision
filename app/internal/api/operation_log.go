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
// @Summary 分页获取操作日志列表
// @Description 分页查询用户操作日志，支持用户名、模块名称、状态码以及时间范围筛选
// @Tags 操作日志模块
// @Security BearerAuth
// @Produce json
// @Param page query int false "页码" default(1)
// @Param pageSize query int false "每页条数" default(10)
// @Param username query string false "操作用户名"
// @Param module query string false "功能模块"
// @Param statusCode query int false "HTTP响应状态码"
// @Param startTime query string false "开始时间 (RFC3339 格式: 2006-01-02T15:04:05Z07:00)"
// @Param endTime query string false "结束时间 (RFC3339 格式: 2006-01-02T15:04:05Z07:00)"
// @Success 200 {object} LogPageResponse "操作日志分页数据"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/oplog/page [get]
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
// @Summary 获取操作日志详情
// @Description 根据 ID 查询单条操作日志详情
// @Tags 操作日志模块
// @Security BearerAuth
// @Produce json
// @Param id path int true "日志ID"
// @Success 200 {object} LogResponse "操作日志详情"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/oplog/{id} [get]
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
