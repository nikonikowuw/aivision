package api

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"argus/app/internal/model"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/response"
	"argus/app/internal/service"
)

// StorageHandler 边缘存储清理与容量状态 API 处理器
type StorageHandler struct {
	srv service.StorageCleanupService
	log *zap.Logger
}

// NewStorageHandler 创建 StorageHandler 实例
func NewStorageHandler(srv service.StorageCleanupService, log *zap.Logger) *StorageHandler {
	return &StorageHandler{
		srv: srv,
		log: log,
	}
}

// GetStorageStatus 获取磁盘容量、各业务表记录数及清理状态 (GET /api/storage/status)
func (h *StorageHandler) GetStorageStatus(c *gin.Context) {
	status, err := h.srv.GetStatus(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Success(c, status)
}

// GetStorageConfig 获取存储保留天数与高低水位配置 (GET /api/storage/config)
func (h *StorageHandler) GetStorageConfig(c *gin.Context) {
	cfg, err := h.srv.GetConfig(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Success(c, cfg)
}

// UpdateStorageConfig 更新存储保留策略配置 (PUT /api/storage/config)
func (h *StorageHandler) UpdateStorageConfig(c *gin.Context) {
	var req model.StorageRetentionConfigValue
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errno.NewError(errno.CodeInvalidParam))
		return
	}

	if err := h.srv.UpdateConfig(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}

	response.Success(c, &req)
}

// TriggerCleanup 手动触发一次存储巡检与清理 (POST /api/storage/cleanup)
func (h *StorageHandler) TriggerCleanup(c *gin.Context) {
	if err := h.srv.TriggerCleanup(c.Request.Context()); err != nil {
		_ = c.Error(err)
		return
	}
	response.Success(c, nil)
}
