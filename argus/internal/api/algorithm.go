package api

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/response"
	"argus/app/internal/repository"
	"argus/app/internal/service"
)

// AlgorithmHandler 算法包管理 HTTP Handler。
type AlgorithmHandler struct {
	svc service.AlgorithmService
}

// NewAlgorithmHandler 创建 AlgorithmHandler。
func NewAlgorithmHandler(svc service.AlgorithmService) *AlgorithmHandler {
	return &AlgorithmHandler{svc: svc}
}

// ListAlgorithms 获取算法列表（支持分页与搜索）。
func (h *AlgorithmHandler) ListAlgorithms(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	algoType := c.Query("algorithmType")
	keyword := c.Query("keyword")

	filter := &repository.AlgorithmFilter{
		Page:          page,
		PageSize:      pageSize,
		AlgorithmType: algoType,
		Keyword:       keyword,
	}

	items, total, err := h.svc.ListAlgorithms(c.Request.Context(), filter)
	if err != nil {
		c.Error(err)
		return
	}

	response.Success(c, gin.H{
		"items": items,
		"total": total,
	})
}

// GetAlgorithm 获取单个算法详情及所有版本。
func (h *AlgorithmHandler) GetAlgorithm(c *gin.Context) {
	algorithmID := c.Param("id")
	if algorithmID == "" {
		c.Error(errno.New(errno.CodeInvalidParam))
		return
	}

	algo, err := h.svc.GetAlgorithm(c.Request.Context(), algorithmID)
	if err != nil {
		c.Error(err)
		return
	}

	response.Success(c, algo)
}

// ListVersions 获取指定算法的所有版本。
func (h *AlgorithmHandler) ListVersions(c *gin.Context) {
	algorithmID := c.Param("id")
	if algorithmID == "" {
		c.Error(errno.New(errno.CodeInvalidParam))
		return
	}

	versions, err := h.svc.ListVersions(c.Request.Context(), algorithmID)
	if err != nil {
		c.Error(err)
		return
	}

	response.Success(c, versions)
}

// UploadAndInstall 接收上传的 .tar.gz 算法包并触发 Engine 安装校验。
func (h *AlgorithmHandler) UploadAndInstall(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.Error(errno.New(errno.CodeInvalidParam))
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.Error(errno.New(errno.CodeInvalidParam))
		return
	}
	defer file.Close()

	version, err := h.svc.UploadAndInstall(c.Request.Context(), file)
	if err != nil {
		c.Error(err)
		return
	}

	response.Success(c, version)
}

// ActivateVersion 激活指定算法版本。
func (h *AlgorithmHandler) ActivateVersion(c *gin.Context) {
	algorithmID := c.Param("id")
	version := c.Param("version")
	if algorithmID == "" || version == "" {
		c.Error(errno.New(errno.CodeInvalidParam))
		return
	}

	if err := h.svc.ActivateVersion(c.Request.Context(), algorithmID, version); err != nil {
		c.Error(err)
		return
	}

	response.Success(c, nil)
}

// UninstallVersion 卸载指定算法版本。
func (h *AlgorithmHandler) UninstallVersion(c *gin.Context) {
	algorithmID := c.Param("id")
	version := c.Param("version")
	if algorithmID == "" || version == "" {
		c.Error(errno.New(errno.CodeInvalidParam))
		return
	}

	if err := h.svc.UninstallVersion(c.Request.Context(), algorithmID, version); err != nil {
		c.Error(err)
		return
	}

	response.Success(c, nil)
}
