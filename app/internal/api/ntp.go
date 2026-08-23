package api

import (
	"time"

	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/response"
	"niko-vue-admin/app/internal/service"
)

// NTPHandler 对时服务 API 处理器
type NTPHandler struct {
	srv service.NTPService
}

// NewNTPHandler 创建对时 API 处理器实例
func NewNTPHandler(srv service.NTPService) *NTPHandler {
	return &NTPHandler{srv: srv}
}

// GetConfig 获取当前对时配置 (GET /api/ntp/config)
func (h *NTPHandler) GetConfig(c *gin.Context) {
	cfg, err := h.srv.GetConfig(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Success(c, cfg)
}

// UpdateConfigRequest 更新对时配置请求
type UpdateConfigRequest struct {
	Mode    string   `json:"mode" binding:"required"`
	Servers []string `json:"servers"`
}

// UpdateConfig 更新对时配置 (PUT /api/ntp/config)
func (h *NTPHandler) UpdateConfig(c *gin.Context) {
	var req UpdateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errno.NewError(errno.CodeInvalidParam))
		return
	}

	err := h.srv.UpdateConfig(c.Request.Context(), &service.UpdateNTPConfigInput{
		Mode:    req.Mode,
		Servers: req.Servers,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}

	response.Success(c, nil)
}

// GetStatus 实时获取同步状态 (GET /api/ntp/status)
func (h *NTPHandler) GetStatus(c *gin.Context) {
	status, err := h.srv.GetStatus(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Success(c, status)
}

// SyncNow 触发立即同步 (POST /api/ntp/sync)
func (h *NTPHandler) SyncNow(c *gin.Context) {
	if err := h.srv.SyncNow(c.Request.Context()); err != nil {
		_ = c.Error(err)
		return
	}
	response.Success(c, nil)
}

// SetTimeRequest 手动设时请求
type SetTimeRequest struct {
	Time time.Time `json:"time" binding:"required"`
}

// SetTime 手动设置系统时间 (POST /api/ntp/set-time)
func (h *NTPHandler) SetTime(c *gin.Context) {
	var req SetTimeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errno.NewError(errno.CodeInvalidParam))
		return
	}

	err := h.srv.SetTime(c.Request.Context(), &service.SetTimeInput{
		Time: req.Time,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}

	response.Success(c, nil)
}

// IsSynced 内部同步状态查询 (GET /api/ntp/synced)
func (h *NTPHandler) IsSynced(c *gin.Context) {
	synced, err := h.srv.IsSynced(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Success(c, gin.H{"synced": synced})
}
