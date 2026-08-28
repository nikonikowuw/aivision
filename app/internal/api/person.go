package api

import (
	"github.com/gin-gonic/gin"

	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/response"
	"niko-vue-admin/app/internal/service"
)

// PersonHandler 人员管理 API。
type PersonHandler struct {
	svc service.PersonService
}

// NewPersonHandler 创建 PersonHandler 实例。
func NewPersonHandler(svc service.PersonService) *PersonHandler {
	return &PersonHandler{svc: svc}
}

// GetPage 获取人员分页列表 (GET /api/person/page)。
func (h *PersonHandler) GetPage(c *gin.Context) {
	var query service.PersonPageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	res, err := h.svc.GetPage(c.Request.Context(), &query)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, res)
}

// CreatePerson 页面创建人员 (POST /api/person)。
func (h *PersonHandler) CreatePerson(c *gin.Context) {
	var input service.CreatePersonInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	res, err := h.svc.CreatePerson(c.Request.Context(), &input)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, res)
}

// UpdatePerson 页面更新人员姓名 (PUT /api/person/:personId)。
func (h *PersonHandler) UpdatePerson(c *gin.Context) {
	personID := c.Param("personId")
	if personID == "" {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	var input service.UpdatePersonInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	res, err := h.svc.UpdatePerson(c.Request.Context(), personID, &input)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, res)
}

// DeletePerson 页面删除人员 (DELETE /api/person/:personId)。
func (h *PersonHandler) DeletePerson(c *gin.Context) {
	personID := c.Param("personId")
	if personID == "" {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	if err := h.svc.DeletePerson(c.Request.Context(), personID); err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, nil)
}

// BatchDeletePerson 页面批量删除人员 (DELETE /api/person/batch)。
func (h *PersonHandler) BatchDeletePerson(c *gin.Context) {
	var input service.BatchDeletePersonInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	if err := h.svc.BatchDeletePerson(c.Request.Context(), &input); err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, nil)
}

// SyncUpsertPerson 外部开放同步单人员新增/更新 (PUT /api/v1/open/person/:personId)。
func (h *PersonHandler) SyncUpsertPerson(c *gin.Context) {
	personID := c.Param("personId")
	if personID == "" {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	var input service.UpdatePersonInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	res, err := h.svc.SyncUpsertPerson(c.Request.Context(), personID, &input)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, res)
}

// SyncDeletePerson 外部开放同步单人员删除 (DELETE /api/v1/open/person/:personId)。
func (h *PersonHandler) SyncDeletePerson(c *gin.Context) {
	personID := c.Param("personId")
	if personID == "" {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	if err := h.svc.SyncDeletePerson(c.Request.Context(), personID); err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, nil)
}
