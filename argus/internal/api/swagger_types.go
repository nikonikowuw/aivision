package api

import (
	"argus/app/internal/model"
	"argus/app/internal/pkg/response"
	"argus/app/internal/service"
)

// Swagger 通用响应类型定义（用于 swaggo 文档生成）

// StringResponse 裸字符串/Token响应包装
type StringResponse struct {
	response.Result
	Data string `json:"data" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
}

// NilResponse 无数据响应包装
type NilResponse struct {
	response.Result
	Data any `json:"data"`
}

// LoginResponse 登录成功响应
type LoginResponse struct {
	response.Result
	Data service.LoginResult `json:"data"`
}

// UserInfoResponse 用户信息响应
type UserInfoResponse struct {
	response.Result
	Data service.UserInfoDTO `json:"data"`
}

// AccessCodesResponse 权限码列表响应
type AccessCodesResponse struct {
	response.Result
	Data []string `json:"data"`
}

// UserPageResponse 用户分页响应
type UserPageResponse struct {
	response.Result
	Data service.UserPageResult `json:"data"`
}

// UserResponse 单个用户响应
type UserResponse struct {
	response.Result
	Data model.User `json:"data"`
}

// RoleIDsResponse 角色ID列表响应
type RoleIDsResponse struct {
	response.Result
	Data []uint64 `json:"data"`
}

// RolePageResponse 角色分页响应
type RolePageResponse struct {
	response.Result
	Data service.RolePageResult `json:"data"`
}

// RoleResponse 单个角色响应
type RoleResponse struct {
	response.Result
	Data model.Role `json:"data"`
}

// MenuIDsResponse 菜单ID列表响应
type MenuIDsResponse struct {
	response.Result
	Data []uint64 `json:"data"`
}

// MenuTreeResponse 全量菜单树响应
type MenuTreeResponse struct {
	response.Result
	Data []*model.MenuTreeNode `json:"data"`
}

// UserMenuTreeResponse 用户前端动态路由响应
type UserMenuTreeResponse struct {
	response.Result
	Data []*service.VbenRouteRecord `json:"data"`
}

// MenuResponse 单个菜单响应
type MenuResponse struct {
	response.Result
	Data model.Menu `json:"data"`
}

// DeptTreeResponse 全量部门树响应
type DeptTreeResponse struct {
	response.Result
	Data []*model.DepartmentTreeNode `json:"data"`
}

// DeptResponse 单个部门响应
type DeptResponse struct {
	response.Result
	Data model.Department `json:"data"`
}

// FileUploadResponse 文件上传成功响应
type FileUploadResponse struct {
	response.Result
	Data service.UploadedFile `json:"data"`
}

// LogPageResponse 操作日志分页响应
type LogPageResponse struct {
	response.Result
	Data service.LogPageResult `json:"data"`
}

// LogResponse 单个操作日志响应
type LogResponse struct {
	response.Result
	Data model.OperationLog `json:"data"`
}

// CameraPageResponse 摄像头分页响应
type CameraPageResponse struct {
	response.Result
	Data service.CameraPageResult `json:"data"`
}

// CameraResponse 单个摄像头响应
type CameraResponse struct {
	response.Result
	Data model.Camera `json:"data"`
}

// ProbeResultResponse 摄像头测活结果响应
type ProbeResultResponse struct {
	response.Result
	Data service.ProbeCameraResult `json:"data"`
}

// LiveStreamResponse 实时拉流地址响应
type LiveStreamResponse struct {
	response.Result
	Data service.CameraLiveStreamResult `json:"data"`
}
