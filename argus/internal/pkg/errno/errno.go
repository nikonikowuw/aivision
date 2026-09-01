// Package errno 集中定义业务错误码（全项目唯一错误码源，见父 design.md §6）。
package errno

import (
	"errors"
	"fmt"
	"strings"
)

// DefaultLang 默认文案语言；未指定或未收录语言时回退到它。
// 取值对齐前端 vben preferences.app.locale（zh-CN / en-US / zh-TW）。
const DefaultLang = "zh-CN"

// supportedLangs 支持文案的语言表：key 为小写规范形，value 为对外语言标识。
var supportedLangs = map[string]string{
	"zh-cn":   "zh-CN",
	"zh-tw":   "zh-TW",
	"zh-hk":   "zh-TW",
	"zh-mo":   "zh-TW",
	"zh-hant": "zh-TW",
	"zh-hans": "zh-CN",
	"en-us":   "en-US",
}

const (
	// CodeOK 成功。
	CodeOK = 0
	// CodeUnauthorized 未认证 / token 失效。
	CodeUnauthorized = 401
	// CodeForbidden 无权限。
	CodeForbidden = 403

	// 业务错误码 1xxx。
	CodeBadCredential        = 1001 // 用户名或密码错误
	CodeUserNotFound         = 1002 // 用户不存在
	CodeUsernameTaken        = 1003 // 用户名已存在
	CodeRoleCodeTaken        = 1004 // 角色 code 已存在
	CodeWrongOldPassword     = 1005 // 旧密码错误
	CodeMenuHasChildren      = 1006 // 菜单存在子节点
	CodeDeptHasChildren      = 1007 // 部门存在子部门
	CodeUserDisabled         = 1008 // 用户被禁用
	CodeInvalidParam         = 1009 // 请求参数错误
	CodeParentIsSelf         = 1010 // 父节点不能是自身
	CodeNotFound             = 1011 // 资源不存在
	CodeMethodNotAllowed     = 1012 // 请求方法不允许
	CodeParentIsDescendant   = 1013 // 父节点不能是当前节点的后代
	CodeSuperRoleProtected   = 1014 // 超级管理员角色不可删除、停用或修改编码
	CodeAdminUserProtected   = 1015 // 超级管理员账号不可删除、停用或修改用户名
	CodeFileTooLarge         = 1016 // 文件超过大小限制
	CodeFileTypeNotAllowed   = 1017 // 文件类型不允许
	CodePersonIDTaken        = 1018 // 人员标识已存在
	CodeAlgoPackageInvalid   = 1019 // 算法包格式非法或解析失败
	CodeAlgoInstallFailed    = 1020 // 算法包安装或自测失败
	CodeAlgoInUse            = 1021 // 算法包正在被任务使用，禁止卸载
	CodeEngineUnavailable    = 1022 // 推理引擎服务不可用
	CodeBuiltinAlgoProtected = 1023 // 系统内置算法受保护，禁止卸载

	// 人脸注册与样本管理业务错误码 1030 ~ 1037
	CodeFaceNoFaceDetected    = 1030 // 未检测到人脸
	CodeFaceMultipleDetected  = 1031 // 检测到多张人脸，请上传单人图片
	CodeFaceQualityTooLow     = 1032 // 人脸质量过低，请重新上传清晰人脸图片
	CodeFaceTooSmall          = 1033 // 人脸尺寸过小
	CodeFaceImageDecodeFailed = 1034 // 人脸图片解码失败
	CodeFaceLimitExceeded     = 1035 // 人员人脸样本已达上限（最多 10 张）
	CodeFaceDuplicateImage    = 1036 // 该人脸图片已注册，请勿重复上传
	CodeFaceAlgoUnavailable   = 1037 // 人脸识别算法服务不可用

	// 网络配置业务错误码 1100 ~ 1113
	CodeNetworkInvalidConfig         = 1100 // IPv4/prefix/gateway/DNS/primary 组合非法
	CodeNetworkTransactionPending    = 1101 // 已存在整机候选事务
	CodeNetworkTransactionNotFound   = 1102 // transaction ID 不存在/已完成
	CodeNetworkTransactionExpired    = 1103 // deadline 已过
	CodeNetworkInterfaceNotManaged   = 1104 // ID 不在当前可写集合或指纹变化
	CodeNetworkOwnershipConflict     = 1105 // Linux 外部管理器/漂移/Resolver 非本系统所有
	CodeNetworkUnsupported           = 1106 // 平台或能力不支持
	CodeNetworkApplyFailed           = 1107 // 平台应用失败且补偿完成或进入故障
	CodeNetworkRecoveryFailed        = 1108 // before/last-valid/factory 恢复失败
	CodeNetworkStateCorrupt          = 1109 // root-only envelope 损坏/版本未知/校验和不符
	CodeNetworkExternalDrift         = 1110 // 当前状态被外部修改，拒绝覆盖
	CodeNetworkNotReady              = 1111 // 启动恢复/能力检查未完成
	CodeNetworkBondSlaveInvalid      = 1112 // bond slave 数量/存在/可写/重复/占用/primary 不合法
	CodeNetworkBondModeConflict      = 1113 // 目标模式与当前拓扑冲突（已处于该模式等）
	CodeNetworkLacpNegotiationFailed = 1114 // LACP 聚合建立失败（内核/驱动拒绝）
	CodeNetworkGatewayPoolInvalid    = 1115 // 地址池、掩码或租约时长非法
	CodeNetworkDhcpServerConflict    = 1116 // 目标链路已存在 DHCP 服务，或接口为 DHCP client 模式

	// NTP 对时错误码 1201-1207
	CodeNTPManualNotAllowedInNTPMode  = 1201 // NTP 模式下不支持手动设时
	CodeNTPSyncNotAllowedInManualMode = 1202 // 手动模式下不支持触发 NTP 同步
	CodeNTPServersEmpty               = 1203 // NTP 模式下服务器列表不能为空
	CodeNTPInvalidMode                = 1204 // 无效的对时模式
	CodeNTPSetTimeFailed              = 1205 // 系统时间设置失败
	CodeNTPSyncFailed                 = 1206 // NTP 同步失败
	CodeNTPExecutorUnavailable        = 1207 // 底层对时执行器不可用

	// 任务配置业务错误码 1300 ~ 1308
	CodeCameraInUse       = 1300 // 摄像头已关联分析任务，禁止删除
	CodeResourceExceeded  = 1301 // 计算资源超出可分配上限
	CodeFPSTierExceeded   = 1302 // 请求 FPS 超过算法包声明的最高档位
	CodeRuleOutOfBounds   = 1303 // 检测规则坐标超出 [0,1] 归一化范围
	CodeRuleTooFewPoints  = 1304 // 检测规则顶点数量不足
	CodeRuleSelfIntersect = 1305 // 检测区域多边形自交
	CodeTaskNotFound      = 1306 // 分析任务不存在
	CodeTaskAlreadyExists = 1307 // 摄像头已存在分析任务
	CodeInstanceNotFound  = 1308 // 算法实例不存在

	// CodeInternal 服务器内部错误（非业务失败，仅作统一响应码）。
	CodeInternal = 1500
)

// unknownCode 私有哨兵码：各语言下未知错误码的兜底文案。
const unknownCode = -1

// messages 按语言分组的业务文案（全项目唯一文案源）。
var messages = map[string]map[int]string{
	DefaultLang: {
		unknownCode:                       "未知错误",
		CodeOK:                            "ok",
		CodeUnauthorized:                  "未认证或 token 已失效",
		CodeForbidden:                     "无权限",
		CodeBadCredential:                 "用户名或密码错误",
		CodeUserNotFound:                  "用户不存在",
		CodeUsernameTaken:                 "用户名已存在",
		CodeRoleCodeTaken:                 "角色 code 已存在",
		CodeWrongOldPassword:              "旧密码错误",
		CodeMenuHasChildren:               "菜单存在子节点，无法删除",
		CodeDeptHasChildren:               "部门存在子部门，无法删除",
		CodeUserDisabled:                  "用户已被禁用",
		CodeInvalidParam:                  "请求参数错误",
		CodeParentIsSelf:                  "父节点不能是自身",
		CodeNotFound:                      "资源不存在",
		CodeMethodNotAllowed:              "请求方法不允许",
		CodeParentIsDescendant:            "父节点不能是当前节点的后代",
		CodeSuperRoleProtected:            "超级管理员角色不可删除、停用或修改编码",
		CodeAdminUserProtected:            "超级管理员账号受系统保护，不可删除、停用或修改用户名",
		CodeFileTooLarge:                  "文件大小超出限制",
		CodeFileTypeNotAllowed:            "不支持的文件类型",
		CodePersonIDTaken:                 "人员标识已存在",
		CodeAlgoPackageInvalid:            "算法包格式非法或解析失败",
		CodeAlgoInstallFailed:             "算法包安装或自测失败",
		CodeAlgoInUse:                     "算法包正在被任务使用，禁止卸载",
		CodeEngineUnavailable:             "推理引擎服务不可用",
		CodeBuiltinAlgoProtected:          "系统内置算法受保护，禁止卸载",
		CodeFaceNoFaceDetected:            "未检测到人脸，请上传包含清晰正脸的图片",
		CodeFaceMultipleDetected:          "检测到多张人脸，请上传仅包含单人正脸的图片",
		CodeFaceQualityTooLow:             "人脸质量过低，请重新上传清晰人脸图片",
		CodeFaceTooSmall:                  "人脸区域尺寸过小，请上传更清晰的人脸图片",
		CodeFaceImageDecodeFailed:         "人脸图片解码失败，请确认图片格式是否正确",
		CodeFaceLimitExceeded:             "人员人脸样本数量已达上限（最多 10 张）",
		CodeFaceDuplicateImage:            "该人脸图片已注册，请勿重复上传相同图片",
		CodeFaceAlgoUnavailable:           "人脸识别算法服务不可用或未启用",
		CodeNetworkInvalidConfig:          "网络配置参数非法或冲突",
		CodeNetworkTransactionPending:     "已有待确认的网络配置事务，请先确认或取消",
		CodeNetworkTransactionNotFound:    "网络事务不存在或已处理",
		CodeNetworkTransactionExpired:     "网络事务已超时回滚",
		CodeNetworkInterfaceNotManaged:    "网卡不受支持或硬件指纹发生变化",
		CodeNetworkOwnershipConflict:      "网卡存在外部网络管理器冲突或已被接管",
		CodeNetworkUnsupported:            "当前平台或运行环境不支持网络配置",
		CodeNetworkApplyFailed:            "网络配置应用失败已自动恢复",
		CodeNetworkRecoveryFailed:         "网络配置恢复失败，请检查设备连接",
		CodeNetworkStateCorrupt:           "网络配置文件校验失败或损坏",
		CodeNetworkExternalDrift:          "检测到外部网络配置漂移，已拒绝修改",
		CodeNetworkNotReady:               "网络配置服务未就绪，正在启动或恢复中",
		CodeNetworkBondSlaveInvalid:       "bond 绑定网卡不合法：需从可写物理网卡中选择恰好 2 块且 primary 在集合内",
		CodeNetworkBondModeConflict:       "目标网络模式与当前拓扑冲突，请先退回多址模式",
		CodeNetworkLacpNegotiationFailed:  "LACP 链路聚合创建失败，底层驱动或内核拒绝参数",
		CodeNetworkGatewayPoolInvalid:     "网关地址池参数非法：起止地址、掩码、租约时长或与接口子网不匹配",
		CodeNetworkDhcpServerConflict:     "目标链路已存在其他 DHCP 服务，或该接口处于 DHCP 客户端模式",
		CodeNTPManualNotAllowedInNTPMode:  "NTP 自动对时模式下不支持手动设置时间",
		CodeNTPSyncNotAllowedInManualMode: "手动对时模式下不支持触发 NTP 同步",
		CodeNTPServersEmpty:               "NTP 模式下服务器列表不能为空",
		CodeNTPInvalidMode:                "无效的对时模式（仅支持 ntp 或 manual）",
		CodeNTPSetTimeFailed:              "系统时间设置失败",
		CodeNTPSyncFailed:                 "NTP 同步失败",
		CodeNTPExecutorUnavailable:        "底层对时执行器不可用",
		CodeCameraInUse:                   "摄像头已关联分析任务，禁止删除",
		CodeResourceExceeded:              "计算资源超出可分配上限（已用 %d / 申请 %d / 上限 %d）",
		CodeFPSTierExceeded:               "请求的采样帧率超过算法包支持的最高档位",
		CodeRuleOutOfBounds:               "检测规则坐标超出有效画面范围",
		CodeRuleTooFewPoints:              "检测规则顶点数量不足",
		CodeRuleSelfIntersect:             "检测区域多边形存在自交",
		CodeTaskNotFound:                  "分析任务不存在",
		CodeTaskAlreadyExists:             "该摄像头已关联分析任务",
		CodeInstanceNotFound:              "算法实例不存在",
		CodeInternal:                      "服务器内部错误",
	},
	"en-US": {
		unknownCode:                       "Unknown error",
		CodeOK:                            "ok",
		CodeUnauthorized:                  "Unauthenticated or token expired",
		CodeForbidden:                     "Forbidden",
		CodeBadCredential:                 "Invalid username or password",
		CodeUserNotFound:                  "User not found",
		CodeUsernameTaken:                 "Username already exists",
		CodeRoleCodeTaken:                 "Role code already exists",
		CodeWrongOldPassword:              "Incorrect old password",
		CodeMenuHasChildren:               "Cannot delete: menu has child nodes",
		CodeDeptHasChildren:               "Cannot delete: department has child nodes",
		CodeUserDisabled:                  "User has been disabled",
		CodeInvalidParam:                  "Invalid parameter",
		CodeParentIsSelf:                  "Parent node cannot be itself",
		CodeNotFound:                      "Resource not found",
		CodeMethodNotAllowed:              "Method not allowed",
		CodeParentIsDescendant:            "Parent node cannot be a descendant of the current node",
		CodeSuperRoleProtected:            "Super admin role cannot be deleted, disabled, or renamed",
		CodeAdminUserProtected:            "Super admin user is protected and cannot be deleted, disabled, or renamed",
		CodeFileTooLarge:                  "File size exceeds the limit",
		CodeFileTypeNotAllowed:            "File type is not allowed",
		CodePersonIDTaken:                 "Person ID already exists",
		CodeAlgoPackageInvalid:            "Algorithm package is invalid or malformed",
		CodeAlgoInstallFailed:             "Algorithm package installation or self-test failed",
		CodeAlgoInUse:                     "Algorithm package is currently in use and cannot be uninstalled",
		CodeEngineUnavailable:             "Inference engine service unavailable",
		CodeBuiltinAlgoProtected:          "System built-in algorithm is protected and cannot be uninstalled",
		CodeFaceNoFaceDetected:            "No face detected in the uploaded image",
		CodeFaceMultipleDetected:          "Multiple faces detected, please upload an image with a single face",
		CodeFaceQualityTooLow:             "Face quality is too low, please upload a clearer face image",
		CodeFaceTooSmall:                  "Face area is too small, please upload a higher resolution face image",
		CodeFaceImageDecodeFailed:         "Failed to decode face image, please check the file format",
		CodeFaceLimitExceeded:             "Maximum number of face samples reached (up to 10 samples)",
		CodeFaceDuplicateImage:            "This face image has already been registered",
		CodeFaceAlgoUnavailable:           "Face recognition algorithm service is unavailable or not activated",
		CodeNetworkInvalidConfig:          "Invalid or conflicting network configuration",
		CodeNetworkTransactionPending:     "A network transaction is already pending confirmation",
		CodeNetworkTransactionNotFound:    "Network transaction not found or already completed",
		CodeNetworkTransactionExpired:     "Network transaction expired and was rolled back",
		CodeNetworkInterfaceNotManaged:    "Network interface is not managed or hardware fingerprint changed",
		CodeNetworkOwnershipConflict:      "Network interface ownership conflict detected",
		CodeNetworkUnsupported:            "Network configuration is not supported on this platform",
		CodeNetworkApplyFailed:            "Network configuration apply failed and was rolled back",
		CodeNetworkRecoveryFailed:         "Network recovery failed, please check host connection",
		CodeNetworkStateCorrupt:           "Network state file checksum mismatch or corrupted",
		CodeNetworkExternalDrift:          "External network drift detected, write operation rejected",
		CodeNetworkNotReady:               "Network service is not ready, starting up or recovering",
		CodeNetworkBondSlaveInvalid:       "Invalid bond slaves: exactly 2 writable physical interfaces required with primary in the set",
		CodeNetworkBondModeConflict:       "Target network mode conflicts with current topology, switch back to multi-address first",
		CodeNetworkLacpNegotiationFailed:  "LACP aggregation setup failed, rejected by underlying driver or kernel",
		CodeNetworkGatewayPoolInvalid:     "Invalid gateway address pool: pool range, prefix, lease duration, or interface subnet mismatch",
		CodeNetworkDhcpServerConflict:     "DHCP server conflict detected on target link or interface is in DHCP client mode",
		CodeNTPManualNotAllowedInNTPMode:  "Manual time setting is not allowed in NTP mode",
		CodeNTPSyncNotAllowedInManualMode: "NTP sync is not allowed in manual mode",
		CodeNTPServersEmpty:               "NTP server list cannot be empty in NTP mode",
		CodeNTPInvalidMode:                "Invalid time mode (must be ntp or manual)",
		CodeNTPSetTimeFailed:              "Failed to set system time",
		CodeNTPSyncFailed:                 "Failed to synchronize NTP",
		CodeNTPExecutorUnavailable:        "NTP executor is unavailable",
		CodeCameraInUse:                   "Camera is associated with an analysis task and cannot be deleted",
		CodeResourceExceeded:              "Compute resource request exceeds the allocatable limit (used %d / requested %d / available %d)",
		CodeFPSTierExceeded:               "Requested analysis FPS exceeds the highest declared tier",
		CodeRuleOutOfBounds:               "Detection rule coordinates are out of the normalized frame bounds",
		CodeRuleTooFewPoints:              "Detection rule has too few points",
		CodeRuleSelfIntersect:             "Detection region polygon is self-intersecting",
		CodeTaskNotFound:                  "Analysis task not found",
		CodeTaskAlreadyExists:             "An analysis task already exists for this camera",
		CodeInstanceNotFound:              "Algorithm instance not found",
		CodeInternal:                      "Internal server error",
	},
	"zh-TW": {
		unknownCode:                       "未知錯誤",
		CodeOK:                            "ok",
		CodeUnauthorized:                  "未認證或 token 已失效",
		CodeForbidden:                     "無權限",
		CodeBadCredential:                 "使用者名稱或密碼錯誤",
		CodeUserNotFound:                  "使用者不存在",
		CodeUsernameTaken:                 "使用者名稱已存在",
		CodeRoleCodeTaken:                 "角色代碼已存在",
		CodeWrongOldPassword:              "舊密碼錯誤",
		CodeMenuHasChildren:               "選單存在子節點，無法刪除",
		CodeDeptHasChildren:               "部門存在子部門，無法刪除",
		CodeUserDisabled:                  "使用者已被停用",
		CodeInvalidParam:                  "請求參數錯誤",
		CodeParentIsSelf:                  "父節點不能是自身",
		CodeNotFound:                      "資源不存在",
		CodeMethodNotAllowed:              "請求方法不允許",
		CodeParentIsDescendant:            "父節點不能是當前節點的後代",
		CodeSuperRoleProtected:            "超級管理員角色不可刪除、停用或修改編碼",
		CodeAdminUserProtected:            "超級管理員帳號受系統保護，不可刪除、停用或修改使用者名稱",
		CodeFileTooLarge:                  "檔案大小超出限制",
		CodeFileTypeNotAllowed:            "不支援的檔案類型",
		CodePersonIDTaken:                 "人員標識已存在",
		CodeAlgoPackageInvalid:            "演算法包格式非法或解析失敗",
		CodeAlgoInstallFailed:             "演算法包安裝或自我檢測失敗",
		CodeAlgoInUse:                     "演算法包正在被任務使用，禁止解除安裝",
		CodeEngineUnavailable:             "推論引擎服務不可用",
		CodeBuiltinAlgoProtected:          "系統內置演算法受保護，禁止解除安裝",
		CodeFaceNoFaceDetected:            "未檢測到人臉，請上傳包含清晰正臉的圖片",
		CodeFaceMultipleDetected:          "檢測到多張人臉，請上傳僅包含單人正臉的圖片",
		CodeFaceQualityTooLow:             "人臉品質過低，請重新上傳清晰人臉圖片",
		CodeFaceTooSmall:                  "人臉區域尺寸過小，請上傳更清晰的人臉圖片",
		CodeFaceImageDecodeFailed:         "人臉圖片解碼失敗，請確認圖片格式是否正確",
		CodeFaceLimitExceeded:             "人員人臉樣本數量已達上限（最多 10 張）",
		CodeFaceDuplicateImage:            "該人臉圖片已註冊，請勿重複上傳相同圖片",
		CodeFaceAlgoUnavailable:           "人臉識別演算法服務不可用或未啟用",
		CodeNetworkInvalidConfig:          "網路設定參數非法或衝突",
		CodeNetworkTransactionPending:     "已有待確認的網路設定事務，請先確認或取消",
		CodeNetworkTransactionNotFound:    "網路事務不存在或已處理",
		CodeNetworkTransactionExpired:     "網路事務已超時回滾",
		CodeNetworkInterfaceNotManaged:    "網卡不受支援或硬體指紋發生變化",
		CodeNetworkOwnershipConflict:      "網卡存在外部網路管理器衝突或已被接管",
		CodeNetworkUnsupported:            "當前平台或運行環境不支援網路設定",
		CodeNetworkApplyFailed:            "網路設定套用失敗已自動復原",
		CodeNetworkRecoveryFailed:         "網路設定復原失敗，請檢查設備連接",
		CodeNetworkStateCorrupt:           "網路設定檔案校驗失敗或損壞",
		CodeNetworkExternalDrift:          "偵測到外部網路設定漂移，已拒絕修改",
		CodeNetworkNotReady:               "網路設定服務未就緒，正在啟動或復原中",
		CodeNetworkBondSlaveInvalid:       "bond 綁定網卡不合法：需從可寫實體網卡中選擇恰好 2 塊且 primary 在集合內",
		CodeNetworkBondModeConflict:       "目標網路模式與目前拓撲衝突，請先退回多址模式",
		CodeNetworkLacpNegotiationFailed:  "LACP 鏈路聚合建立失敗，底層驅動或核心拒絕參數",
		CodeNetworkGatewayPoolInvalid:     "閘道位址池參數非法：起止位址、遮罩、租約時長或與介面子網不相符",
		CodeNetworkDhcpServerConflict:     "目標鏈路已存在其他 DHCP 服務，或該介面處於 DHCP 用戶端模式",
		CodeNTPManualNotAllowedInNTPMode:  "NTP 自動對時模式下不支援手動設定時間",
		CodeNTPSyncNotAllowedInManualMode: "手動對時模式下不支援觸發 NTP 同步",
		CodeNTPServersEmpty:               "NTP 模式下伺服器清單不能為空",
		CodeNTPInvalidMode:                "無效的對時模式（僅支援 ntp 或 manual）",
		CodeNTPSetTimeFailed:              "系統時間設定失敗",
		CodeNTPSyncFailed:                 "NTP 同步失敗",
		CodeNTPExecutorUnavailable:        "底層對時執行器不可用",
		CodeCameraInUse:                   "攝影機已關聯分析任務，禁止刪除",
		CodeResourceExceeded:              "計算資源超出可分配上限（已用 %d / 申請 %d / 上限 %d）",
		CodeFPSTierExceeded:               "請求的取樣幀率超過演算法包支援的最高檔位",
		CodeRuleOutOfBounds:               "偵測規則座標超出有效畫面範圍",
		CodeRuleTooFewPoints:              "偵測規則頂點數量不足",
		CodeRuleSelfIntersect:             "偵測區域多邊形存在自交",
		CodeTaskNotFound:                  "分析任務不存在",
		CodeTaskAlreadyExists:             "該攝影機已關聯分析任務",
		CodeInstanceNotFound:              "演算法實例不存在",
		CodeInternal:                      "伺服器內部錯誤",
	},
}

// LangFromHeader 从 Accept-Language 请求头解析出支持的语言。
// 浏览器会发送 "zh-CN,zh;q=0.9,en;q=0.8" 这类带逗号与 q 权重的列表，
// 不能把原始 header 直接当 lang 用：取首个 tag、去掉 q 权重、大小写不敏感匹配；
// 仅基础 tag（如 en/zh）时按前缀/变体匹配；无匹配回退 DefaultLang。
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
	// 基础 tag / 变体前缀匹配：en → en-US，zh-TW/HK/Hant/MO → zh-TW，zh → zh-CN。
	base := tag
	if i := strings.IndexByte(base, '-'); i >= 0 {
		base = base[:i]
	}
	switch base {
	case "zh":
		if strings.Contains(tag, "tw") || strings.Contains(tag, "hk") || strings.Contains(tag, "hant") || strings.Contains(tag, "mo") {
			return "zh-TW"
		}
		return "zh-CN"
	case "en":
		return "en-US"
	}
	return DefaultLang
}

// Error 携带业务错误码的业务错误。文案不在此固化，统一由
// response/middleware 按请求语言经 Message 获取，避免绕过 i18n。
// args 是可选的文案插值参数：文案模板保留在 errno 表内，占位符按
// fmt.Sprintf 语义填充（如 CodeResourceExceeded 的已用/申请/上限）。
type Error struct {
	Code int
	args []any
}

func (e *Error) Error() string {
	return MessageWithArgs(DefaultLang, e.Code, e.args...)
}

// Args 返回该错误携带的文案插值参数（未携带时返回 nil）。
func (e *Error) Args() []any {
	return e.args
}

// Is 判定错误码是否一致。
func Is(err error, code int) bool {
	if err == nil {
		return false
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code == code
	}
	return false
}

// NewError 构造携带业务错误码的 Error。
func NewError(code int) *Error {
	return &Error{Code: code}
}

// NewErrorArgs 构造携带业务错误码与文案插值参数的 Error。
// 只有 errno 文案表明确声明占位符的错误码才应携带 args（当前仅 CodeResourceExceeded）。
func NewErrorArgs(code int, args ...any) *Error {
	return &Error{Code: code, args: args}
}

// New 是 NewError 的简写别名。
func New(code int) *Error {
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

// MessageWithArgs 返回指定语言 lang 下错误码 code 的文案，并按 fmt.Sprintf
// 将 args 插值到文案模板的占位符中；args 为空时等价于 Message。
func MessageWithArgs(lang string, code int, args ...any) string {
	msg := Message(lang, code)
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}
