package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/response"
	"argus/app/internal/service"
)

const maxMultipartRequestOverhead int64 = 1 << 20

// FileHandler 处理文件上传接口。
type FileHandler struct {
	svc     service.FileService
	maxSize int64
}

// NewFileHandler 创建 FileHandler 实例。
func NewFileHandler(svc service.FileService, cfg *config.Config) *FileHandler {
	var maxSize int64
	if cfg != nil {
		maxSize = cfg.Storage.MaxSize
	}
	return &FileHandler{svc: svc, maxSize: maxSize}
}

// Upload 上传单个文件 (POST /api/file/upload)。
// @Summary 上传文件
// @Description 上传图片或 PDF 文件，返回不需要认证即可访问的文件 URL
// @Tags 文件模块
// @Security BearerAuth
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "上传文件"
// @Success 200 {object} FileUploadResponse "上传成功"
// @Failure 400 {object} response.Result "文件参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 500 {object} response.Result "存储失败"
// @Router /api/file/upload [post]
func (h *FileHandler) Upload(c *gin.Context) {
	if h.maxSize > 0 && c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxSize+maxMultipartRequestOverhead)
	}

	fileHeader, err := c.FormFile(service.UploadFileFieldName)
	if err != nil {
		if isMaxBytesError(err) {
			c.Error(errno.NewError(errno.CodeFileTooLarge)) //nolint:errcheck
			return
		}
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	if fileHeader == nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.Error(fmt.Errorf("open uploaded file: %w", err)) //nolint:errcheck
		return
	}
	defer func() { _ = file.Close() }()

	result, err := h.svc.Upload(c.Request.Context(), &service.UploadInput{
		Name:   fileHeader.Filename,
		Size:   fileHeader.Size,
		Reader: file,
	})
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}

	response.Success(c, result)
}

func isMaxBytesError(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}
