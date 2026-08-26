package api

import (
	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/response"
	"niko-vue-admin/app/internal/service"
)

// CameraHandler 摄像头 API。
type CameraHandler struct {
	svc service.CameraService
}

// NewCameraHandler 创建 CameraHandler 实例。
func NewCameraHandler(svc service.CameraService) *CameraHandler {
	return &CameraHandler{svc: svc}
}

// BatchDeleteCameraInput 批量删除摄像头入参。
type BatchDeleteCameraInput struct {
	IDs []uint64 `json:"ids" binding:"required,min=1,dive,gt=0"`
}

// ProbeCameraInput 测活入参。
type ProbeCameraInput struct {
	ID       *uint64 `json:"id"`       // 可选：已保存摄像头的数值 id；省略表示未保存表单
	Protocol string  `json:"protocol"` // 当前固定 rtsp；省略默认 rtsp
	RtspURL  string  `json:"rtspUrl" binding:"required"`
}

// GetPage 获取摄像头分页列表 (GET /api/camera/page)。
// @Summary 分页获取摄像头列表
// @Description 分页查询摄像头数据，支持名称模糊筛选
// @Tags 摄像头管理
// @Security BearerAuth
// @Produce json
// @Param page query int false "页码" default(1)
// @Param pageSize query int false "每页条数" default(10)
// @Param name query string false "名称模糊查询"
// @Success 200 {object} CameraPageResponse "摄像头分页数据"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/camera/page [get]
func (h *CameraHandler) GetPage(c *gin.Context) {
	var query service.CameraPageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	res, err := h.svc.GetPage(c.Request.Context(), &query)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, res)
}

// CreateCamera 创建摄像头 (POST /api/camera)。
// @Summary 新增摄像头
// @Description 新增 RTSP 视频源；保存只做字段与 URL 校验，不强制测活成功
// @Tags 摄像头管理
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body service.SaveCameraInput true "创建摄像头参数"
// @Success 200 {object} CameraResponse "创建成功的摄像头"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/camera [post]
func (h *CameraHandler) CreateCamera(c *gin.Context) {
	var input service.SaveCameraInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	cam, err := h.svc.CreateCamera(c.Request.Context(), &input)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, cam)
}

// UpdateCamera 更新摄像头 (PUT /api/camera/:id)。
// @Summary 更新摄像头
// @Description 根据 ID 修改摄像头名称、RTSP URL 与备注；配置变更后旧测活结果视为不适用于当前配置
// @Tags 摄像头管理
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "摄像头ID"
// @Param request body service.SaveCameraInput true "更新摄像头参数"
// @Success 200 {object} CameraResponse "更新后的摄像头"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/camera/{id} [put]
func (h *CameraHandler) UpdateCamera(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	var input service.SaveCameraInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	cam, err := h.svc.UpdateCamera(c.Request.Context(), id, &input)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, cam)
}

// DeleteCamera 删除摄像头 (DELETE /api/camera/:id)。
// @Summary 删除摄像头
// @Description 软删除指定 ID 的摄像头；camera_id 永不复用
// @Tags 摄像头管理
// @Security BearerAuth
// @Produce json
// @Param id path int true "摄像头ID"
// @Success 200 {object} NilResponse "删除成功"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/camera/{id} [delete]
func (h *CameraHandler) DeleteCamera(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	if err := h.svc.DeleteCamera(c.Request.Context(), id); err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, nil)
}

// BatchDeleteCamera 批量删除摄像头 (DELETE /api/camera/batch)。
// @Summary 批量删除摄像头
// @Description 批量软删除指定 ID 列表的摄像头
// @Tags 摄像头管理
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body BatchDeleteCameraInput true "批量删除参数"
// @Success 200 {object} NilResponse "删除成功"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/camera/batch [delete]
func (h *CameraHandler) BatchDeleteCamera(c *gin.Context) {
	var input BatchDeleteCameraInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	if err := h.svc.BatchDeleteCamera(c.Request.Context(), input.IDs); err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, nil)
}

// ProbeCamera 摄像头测活 (POST /api/camera/probe)。
// @Summary 摄像头测活
// @Description 对 RTSP 配置执行测活（TCP 优先，失败回退 UDP，首帧即成功）。测活失败也返回 code=0，结果在 data.status 中
// @Tags 摄像头管理
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body ProbeCameraInput true "测活参数"
// @Success 200 {object} ProbeResultResponse "测活结构化结果"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 403 {object} response.Result "无权限"
// @Router /api/camera/probe [post]
func (h *CameraHandler) ProbeCamera(c *gin.Context) {
	var input ProbeCameraInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	req := service.ProbeCameraRequest{
		RtspURL: input.RtspURL,
	}
	if input.ID != nil {
		req.ID = *input.ID
	}
	protocol := input.Protocol
	if protocol == "" {
		protocol = model.CameraProtocolRTSP
	}
	req.Protocol = protocol

	result, err := h.svc.ProbeCamera(c.Request.Context(), &req)
	if err != nil {
		c.Error(err) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	response.Success(c, result)
}
