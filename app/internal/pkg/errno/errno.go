// Package errno 集中定义业务错误码（全项目唯一错误码源，见父 design.md §6）。
package errno

import "strings"

// DefaultLang 默认文案语言；未指定或未收录语言时回退到它。
// 取值对齐前端 vben preferences.app.locale（zh-CN / en-US）。
const DefaultLang = "zh-CN"

// supportedLangs 支持文案的语言表：key 为小写规范形，value 为对外语言标识。
var supportedLangs = map[string]string{
	"zh-cn": "zh-CN",
	"en-us": "en-US",
}

const (
	// CodeOK 成功。
	CodeOK = 0
	// CodeUnauthorized 未认证 / token 失效。
	CodeUnauthorized = 401
	// CodeForbidden 无权限。
	CodeForbidden = 403

	// 业务错误码 1xxx。
	CodeBadCredential      = 1001 // 用户名或密码错误
	CodeUserNotFound       = 1002 // 用户不存在
	CodeUsernameTaken      = 1003 // 用户名已存在
	CodeRoleCodeTaken      = 1004 // 角色 code 已存在
	CodeWrongOldPassword   = 1005 // 旧密码错误
	CodeMenuHasChildren    = 1006 // 菜单存在子节点
	CodeDeptHasChildren    = 1007 // 部门存在子部门
	CodeUserDisabled       = 1008 // 用户被禁用
	CodeInvalidParam       = 1009 // 请求参数错误
	CodeParentIsSelf       = 1010 // 父节点不能是自身
	CodeNotFound           = 1011 // 资源不存在
	CodeMethodNotAllowed   = 1012 // 请求方法不允许
	CodeParentIsDescendant = 1013 // 父节点不能是当前节点的后代
	CodeSuperRoleProtected = 1014 // 超级管理员角色不可删除、停用或修改编码
	CodeAdminUserProtected = 1015 // 超级管理员账号不可删除、停用或修改用户名

	// CodeInternal 服务器内部错误（非业务失败，仅作统一响应码）。
	CodeInternal = 1500
)

// unknownCode 私有哨兵码：各语言下未知错误码的兜底文案。
const unknownCode = -1

// messages 按语言分组的业务文案（全项目唯一文案源）。
var messages = map[string]map[int]string{
	DefaultLang: {
		unknownCode:            "未知错误",
		CodeOK:                 "ok",
		CodeUnauthorized:       "未认证或 token 已失效",
		CodeForbidden:          "无权限",
		CodeBadCredential:      "用户名或密码错误",
		CodeUserNotFound:       "用户不存在",
		CodeUsernameTaken:      "用户名已存在",
		CodeRoleCodeTaken:      "角色 code 已存在",
		CodeWrongOldPassword:   "旧密码错误",
		CodeMenuHasChildren:    "菜单存在子节点，无法删除",
		CodeDeptHasChildren:    "部门存在子部门，无法删除",
		CodeUserDisabled:       "用户已被禁用",
		CodeInvalidParam:       "请求参数错误",
		CodeParentIsSelf:       "父节点不能是自身",
		CodeNotFound:           "资源不存在",
		CodeMethodNotAllowed:   "请求方法不允许",
		CodeParentIsDescendant: "父节点不能是当前节点的后代",
		CodeSuperRoleProtected: "超级管理员角色不可删除、停用或修改编码",
		CodeAdminUserProtected: "超级管理员账号受系统保护，不可删除、停用或修改用户名",
		CodeInternal:           "服务器内部错误",
	},
	"en-US": {
		unknownCode:            "Unknown error",
		CodeOK:                 "ok",
		CodeUnauthorized:       "Unauthenticated or token expired",
		CodeForbidden:          "Forbidden",
		CodeBadCredential:      "Invalid username or password",
		CodeUserNotFound:       "User not found",
		CodeUsernameTaken:      "Username already exists",
		CodeRoleCodeTaken:      "Role code already exists",
		CodeWrongOldPassword:   "Incorrect old password",
		CodeMenuHasChildren:    "Cannot delete: menu has child nodes",
		CodeDeptHasChildren:    "Cannot delete: department has child nodes",
		CodeUserDisabled:       "User has been disabled",
		CodeInvalidParam:       "Invalid parameter",
		CodeParentIsSelf:       "Parent node cannot be itself",
		CodeNotFound:           "Resource not found",
		CodeMethodNotAllowed:   "Method not allowed",
		CodeParentIsDescendant: "Parent node cannot be a descendant of the current node",
		CodeSuperRoleProtected: "Super admin role cannot be deleted, disabled, or renamed",
		CodeAdminUserProtected: "Super admin user is protected and cannot be deleted, disabled, or renamed",
		CodeInternal:           "Internal server error",
	},
}

// LangFromHeader 从 Accept-Language 请求头解析出支持的语言。
// 浏览器会发送 "zh-CN,zh;q=0.9,en;q=0.8" 这类带逗号与 q 权重的列表，
// 不能把原始 header 直接当 lang 用：取首个 tag、去掉 q 权重、大小写不敏感匹配；
// 仅基础 tag（如 en/zh）时按前缀匹配；无匹配回退 DefaultLang。
func LangFromHeader(header string) string {
	if header == "" {
		return DefaultLang
	}
	tag := strings.TrimSpace(strings.Split(header, ",")[0])
	if i := strings.IndexByte(tag, ';'); i >= 0 {
		tag = strings.TrimSpace(tag[:i])
	}
	if tag == "" || tag == "*" {
		return DefaultLang
	}
	tag = strings.ToLower(tag)
	if lang, ok := supportedLangs[tag]; ok {
		return lang
	}
	// 基础 tag 前缀匹配：en → en-US，zh → zh-CN。
	base := tag
	if i := strings.IndexByte(base, '-'); i >= 0 {
		base = base[:i]
	}
	for k, lang := range supportedLangs {
		if strings.HasPrefix(k, base) {
			return lang
		}
	}
	return DefaultLang
}

// Error 携带业务错误码的业务错误。文案不在此固化，统一由
// response/middleware 按请求语言经 Message 获取，避免绕过 i18n。
type Error struct {
	Code int
}

func (e *Error) Error() string {
	return Message(DefaultLang, e.Code)
}

func NewError(code int) *Error {
	return &Error{Code: code}
}

// Message 返回指定语言 lang 下错误码 code 的用户文案；
// lang 为空或未收录时回退 DefaultLang，未知码返回对应语言的兜底文案；
// 若该语言文案表不完整（缺 unknownCode），再回退到 DefaultLang 的兜底文案。
func Message(lang string, code int) string {
	msgs := messages[lang]
	if msgs == nil {
		msgs = messages[DefaultLang]
	}
	if msg, ok := msgs[code]; ok {
		return msg
	}
	if fallback, ok := msgs[unknownCode]; ok {
		return fallback
	}
	return messages[DefaultLang][unknownCode]
}
