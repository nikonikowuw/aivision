package model

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// seedMenuItem 种子菜单声明（设计 §5 权限码契约表，全项目唯一权限码源）。
type seedMenuItem struct {
	Type       string
	Name       string // catalog/menu 为 ASCII 路由标识符；button 为 i18n key（如 system.user.addUser）
	Title      string // i18n key，如 routes.system.user（决策 17）
	Path       string
	Component  string
	Icon       string
	Permission string
	Affix      bool
	KeepAlive  bool
	Children   []seedMenuItem
}

// seedMenuTree 设计 §5 菜单树契约，严禁增删权限码。
// 顺序调整：实时预览(1) -> 智能记录(2) -> 资源管理(3) -> AI算法(4) -> 运维管理(5) -> 系统管理(6)
var seedMenuTree = []seedMenuItem{
	{
		Type: MenuTypeMenu, Name: "LivePreview", Title: "routes.live.live", Path: "/live", Component: "/live/index",
		Icon: "ant-design:video-camera-outlined", Permission: "live:preview", Affix: true,
		Children: []seedMenuItem{
			{Type: MenuTypeButton, Name: "live.preview.stream", Permission: "live:preview:stream"},
		},
	},
	{
		Type: MenuTypeCatalog, Name: "Record", Title: "routes.record.record", Path: "/record", Component: "BasicLayout",
		Icon: "ant-design:history-outlined",
		Children: []seedMenuItem{
			{
				Type: MenuTypeMenu, Name: "RecordAlarm", Title: "routes.record.alarm", Path: "/record/alarm", Component: "/record/alarm/index",
				Icon: "ant-design:alert-outlined", Permission: "record:alarm", KeepAlive: true,
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "record.alarm.query", Permission: "record:alarm:query"},
					{Type: MenuTypeButton, Name: "record.alarm.export", Permission: "record:alarm:export"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "RecordPlate", Title: "routes.record.plate", Path: "/record/plate", Component: "/record/plate/index",
				Icon: "ant-design:car-outlined", Permission: "record:plate", KeepAlive: true,
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "record.plate.query", Permission: "record:plate:query"},
					{Type: MenuTypeButton, Name: "record.plate.export", Permission: "record:plate:export"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "RecordCapture", Title: "routes.record.capture", Path: "/record/capture", Component: "/record/capture/index",
				Icon: "ant-design:camera-outlined", Permission: "record:capture", KeepAlive: true,
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "record.capture.query", Permission: "record:capture:query"},
					{Type: MenuTypeButton, Name: "record.capture.export", Permission: "record:capture:export"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "RecordFace", Title: "routes.record.face", Path: "/record/face", Component: "/record/face/index",
				Icon: "ant-design:user-outlined", Permission: "record:face", KeepAlive: true,
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "record.face.query", Permission: "record:face:query"},
					{Type: MenuTypeButton, Name: "record.face.export", Permission: "record:face:export"},
				},
			},
		},
	},
	{
		Type: MenuTypeCatalog, Name: "Resource", Title: "routes.resource.resource", Path: "/resource", Component: "BasicLayout",
		Icon: "ant-design:database-outlined",
		Children: []seedMenuItem{
			{
				Type: MenuTypeMenu, Name: "Camera", Title: "routes.resource.camera", Path: "/resource/camera", Component: "/resource/camera/index",
				Icon: "ant-design:video-camera-outlined", Permission: "resource:camera",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "resource.camera.add", Permission: "resource:camera:add"},
					{Type: MenuTypeButton, Name: "resource.camera.edit", Permission: "resource:camera:edit"},
					{Type: MenuTypeButton, Name: "resource.camera.delete", Permission: "resource:camera:delete"},
					{Type: MenuTypeButton, Name: "resource.camera.probe", Permission: "resource:camera:probe"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "ResourcePerson", Title: "routes.resource.person", Path: "/resource/person", Component: "/resource/person/index",
				Icon: "ant-design:idcard-outlined", Permission: "resource:person", KeepAlive: true,
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "resource.person.add", Permission: "resource:person:add"},
					{Type: MenuTypeButton, Name: "resource.person.edit", Permission: "resource:person:edit"},
					{Type: MenuTypeButton, Name: "resource.person.delete", Permission: "resource:person:delete"},
					{Type: MenuTypeButton, Name: "resource.person.faceManage", Permission: "resource:person:face:manage"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "ResourceTask", Title: "routes.resource.task", Path: "/resource/task", Component: "/resource/task/index",
				Icon: "ant-design:profile-outlined", Permission: "resource:task", KeepAlive: true,
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "resource.task.add", Permission: "resource:task:add"},
					{Type: MenuTypeButton, Name: "resource.task.edit", Permission: "resource:task:edit"},
					{Type: MenuTypeButton, Name: "resource.task.delete", Permission: "resource:task:delete"},
				},
			},
		},
	},
	{
		Type: MenuTypeCatalog, Name: "AI", Title: "routes.ai.ai", Path: "/ai", Component: "BasicLayout",
		Icon: "ant-design:robot-outlined",
		Children: []seedMenuItem{
			{
				Type: MenuTypeMenu, Name: "AiAlgorithm", Title: "routes.ai.algorithm", Path: "/ai/algorithm", Component: "/ai/algorithm/index",
				Icon: "ant-design:appstore-outlined", Permission: "ai:algorithm", KeepAlive: true,
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "ai.algorithm.upload", Permission: "ai:algorithm:upload"},
					{Type: MenuTypeButton, Name: "ai.algorithm.activate", Permission: "ai:algorithm:activate"},
					{Type: MenuTypeButton, Name: "ai.algorithm.uninstall", Permission: "ai:algorithm:uninstall"},
				},
			},
		},
	},
	{
		Type: MenuTypeCatalog, Name: "Ops", Title: "routes.ops.ops", Path: "/ops", Component: "BasicLayout",
		Icon: "ant-design:tool-outlined",
		Children: []seedMenuItem{
			{
				Type: MenuTypeMenu, Name: "Time", Title: "routes.ops.time", Path: "/ops/time", Component: "/ops/time/index",
				Icon: "ant-design:field-time-outlined", Permission: "ops:time",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "ops.time.read", Permission: "ops:time:read"},
					{Type: MenuTypeButton, Name: "ops.time.edit", Permission: "ops:time:edit"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "Network", Title: "routes.ops.network", Path: "/ops/network", Component: "/ops/network/index",
				Icon: "ant-design:global-outlined", Permission: "ops:network",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "system.common.edit", Permission: "ops:network:edit"},
					{Type: MenuTypeButton, Name: "ops.network.confirm", Permission: "ops:network:confirm"},
					{Type: MenuTypeButton, Name: "ops.network.cancel", Permission: "ops:network:cancel"},
					{Type: MenuTypeButton, Name: "ops.network.reset", Permission: "ops:network:reset"},
					{Type: MenuTypeButton, Name: "ops.network.mode", Permission: "ops:network:mode"},
				},
			},
		},
	},
	{
		Type: MenuTypeCatalog, Name: "System", Title: "routes.system.system", Path: "/system", Component: "BasicLayout",
		Icon: "ant-design:setting-outlined",
		Children: []seedMenuItem{
			{
				Type: MenuTypeMenu, Name: "User", Title: "routes.system.user", Path: "/system/user", Component: "/system/user/index",
				Icon: "ant-design:user-outlined", Permission: "system:user",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "system.user.addUser", Permission: "system:user:add"},
					{Type: MenuTypeButton, Name: "system.user.editUser", Permission: "system:user:edit"},
					{Type: MenuTypeButton, Name: "system.user.deleteUser", Permission: "system:user:delete"},
					{Type: MenuTypeButton, Name: "system.user.resetPassword", Permission: "system:user:reset-password"},
					{Type: MenuTypeButton, Name: "system.user.assignRole", Permission: "system:user:assign-role"},
					{Type: MenuTypeButton, Name: "system.user.status", Permission: "system:user:status"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "Role", Title: "routes.system.role", Path: "/system/role", Component: "/system/role/index",
				Icon: "ant-design:team-outlined", Permission: "system:role",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "system.role.addRole", Permission: "system:role:add"},
					{Type: MenuTypeButton, Name: "system.role.editRole", Permission: "system:role:edit"},
					{Type: MenuTypeButton, Name: "system.role.deleteRole", Permission: "system:role:delete"},
					{Type: MenuTypeButton, Name: "system.role.assignMenu", Permission: "system:role:assign-menu"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "Menu", Title: "routes.system.menu", Path: "/system/menu", Component: "/system/menu/index",
				Icon: "ant-design:menu-outlined", Permission: "system:menu",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "system.menu.addMenu", Permission: "system:menu:add"},
					{Type: MenuTypeButton, Name: "system.menu.editMenu", Permission: "system:menu:edit"},
					{Type: MenuTypeButton, Name: "system.menu.deleteMenu", Permission: "system:menu:delete"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "Dept", Title: "routes.system.dept", Path: "/system/dept", Component: "/system/dept/index",
				Icon: "ant-design:apartment-outlined", Permission: "system:dept",
				Children: []seedMenuItem{
					{Type: MenuTypeButton, Name: "system.dept.addDept", Permission: "system:dept:add"},
					{Type: MenuTypeButton, Name: "system.dept.editDept", Permission: "system:dept:edit"},
					{Type: MenuTypeButton, Name: "system.dept.deleteDept", Permission: "system:dept:delete"},
				},
			},
			{
				Type: MenuTypeMenu, Name: "Log", Title: "routes.system.log", Path: "/system/log", Component: "/system/log/index",
				Icon: "ant-design:file-text-outlined", Permission: "system:log",
			},
		},
	},
}

const (
	seedAdminPassword = "admin123"
	seedDeptName      = "演示部门"

	seedSingletonID                int16 = 1
	faceGalleryRevisionInitialSync int64 = 1
)

// Seed 幂等播种种子数据。
// 1. 惰性清理过期 refresh token；
// 2. 幂等播种系统单例与内置算法（版本计数器、系统配置、内置算法等）；
// 3. 检查 admin 用户：
//   - 若已存在：增量同步菜单树给超级管理员，返回 false, nil；
//   - 若不存在：创建初始部门、超级管理员角色、全量菜单绑定、admin 用户及角色绑定，返回 true, nil。
func Seed(db *gorm.DB) (bool, error) {
	// 惰性清理过期 refresh token（父 design.md §2：不做定时任务）。
	if err := db.Where("expires_at < ?", time.Now()).Delete(&RefreshToken{}).Error; err != nil {
		return false, fmt.Errorf("clean expired refresh tokens: %w", err)
	}

	seeded := false
	if err := db.Transaction(func(tx *gorm.DB) error {
		// 系统单例与内置算法必须在每次 seed 时幂等补齐，不能依赖 admin 是否存在。
		if err := seedSystemSingletons(tx); err != nil {
			return fmt.Errorf("seed system singletons: %w", err)
		}

		var count int64
		if err := tx.Model(&User{}).Where("username = ?", AdminUsername).Count(&count).Error; err != nil {
			return fmt.Errorf("check admin exists: %w", err)
		}
		if count > 0 {
			// 增量同步菜单树给超级管理员（幂等补充新增的系统菜单）。
			var superRole Role
			if err := tx.Where("code = ?", RoleSuperCode).First(&superRole).Error; err == nil {
				if err := seedMenuBranch(tx, superRole.ID, 0, seedMenuTree); err != nil {
					return fmt.Errorf("sync incremental menu tree: %w", err)
				}
			}
			return nil
		}

		if err := seedInitialRBAC(tx); err != nil {
			return err
		}
		seeded = true
		return nil
	}); err != nil {
		return false, err
	}
	return seeded, nil
}

// seedInitialRBAC 初始化演示部门、超级管理员角色、菜单权限、admin 用户及其角色绑定。
func seedInitialRBAC(tx *gorm.DB) error {
	// demo 部门
	dept := Department{Name: seedDeptName, ParentID: 0, Sort: 0, Status: StatusEnabled}
	if err := tx.Where("name = ?", dept.Name).FirstOrCreate(&dept).Error; err != nil {
		return fmt.Errorf("seed department: %w", err)
	}

	// super 角色
	role := Role{Name: "超级管理员", Code: RoleSuperCode, Status: StatusEnabled, Sort: 0}
	if err := tx.Where("code = ?", role.Code).FirstOrCreate(&role).Error; err != nil {
		return fmt.Errorf("seed role: %w", err)
	}

	// 菜单树 + super 角色全量绑定
	if err := seedMenuBranch(tx, role.ID, 0, seedMenuTree); err != nil {
		return err
	}

	// admin 用户（bcrypt，仅在指定密码或非空时创建，若环境未设则通过 cmd/bootstrap 手动初始化）
	adminPassword := os.Getenv("APP_BOOTSTRAP_ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = seedAdminPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	user := User{
		Username: AdminUsername,
		Password: string(hash),
		Nickname: "管理员",
		Email:    "admin@example.com",
		DeptID:   dept.ID,
		Status:   StatusEnabled,
	}
	if err := tx.Where("username = ?", user.Username).FirstOrCreate(&user).Error; err != nil {
		return fmt.Errorf("seed admin user: %w", err)
	}

	// admin → super 绑定
	ur := UserRole{UserID: user.ID, RoleID: role.ID}
	if err := tx.Where("user_id = ? AND role_id = ?", ur.UserID, ur.RoleID).FirstOrCreate(&ur).Error; err != nil {
		return fmt.Errorf("seed user_role: %w", err)
	}

	return nil
}

// seedSystemSingletons 幂等补齐系统配置、版本计数器与内置算法。
func seedSystemSingletons(tx *gorm.DB) error {
	// 基础系统配置 (system:time)
	timeCfg := SystemConfig{
		Key:    ConfigKeyTime,
		Value:  `{"mode":"ntp","servers":["pool.ntp.org","ntp.aliyun.com"]}`,
		Remark: "系统对时配置",
	}
	if err := tx.Where("key = ?", timeCfg.Key).FirstOrCreate(&timeCfg).Error; err != nil {
		return fmt.Errorf("seed system config (%s): %w", timeCfg.Key, err)
	}

	// 任务版本计数器单行初始化 (id=1, revision=0)
	rev := DesiredStateRevision{ID: seedSingletonID, Revision: 0}
	if err := tx.Where("id = ?", seedSingletonID).FirstOrCreate(&rev).Error; err != nil {
		return fmt.Errorf("seed desired_state_revision: %w", err)
	}

	// 人脸底库版本计数器单行初始化 (id=1, revision=0)
	galleryRev := FaceGalleryRevision{ID: seedSingletonID, Revision: 0}
	if err := tx.Where("id = ?", seedSingletonID).FirstOrCreate(&galleryRev).Error; err != nil {
		return fmt.Errorf("seed face_gallery_revision: %w", err)
	}
	// 旧库可能已经有样本，但迁移只创建了 revision=0 的初始行；将其标记为一次变更，
	// 让 Engine 冷启动时拉取已有底库，而不是误判为 unchanged。
	if galleryRev.Revision == 0 {
		var faceCount int64
		if err := tx.Model(&PersonFace{}).Count(&faceCount).Error; err != nil {
			return fmt.Errorf("check existing face gallery: %w", err)
		}
		if faceCount > 0 {
			if err := tx.Model(&FaceGalleryRevision{}).
				Where("id = ?", seedSingletonID).
				Update("revision", faceGalleryRevisionInitialSync).Error; err != nil {
				return fmt.Errorf("initialize face_gallery_revision for existing faces: %w", err)
			}
		}
	}

	// 内置算法: 通用目标检测 (general_detection)
	builtinAlgo := Algorithm{
		AlgorithmID:   "general_detection",
		Name:          "通用目标检测",
		AlgorithmType: "object_detection",
		AlarmTypeID:   "object_detect",
		ActiveVersion: "1.0.0",
		Description:   "系统内置通用目标检测算法，支持人员、车辆、动物、随身物品等多类别自由过滤与自定义业务标签",
		IsBuiltin:     true,
	}
	if err := tx.Where("algorithm_id = ?", builtinAlgo.AlgorithmID).FirstOrCreate(&builtinAlgo).Error; err != nil {
		return fmt.Errorf("seed builtin algorithm (%s): %w", builtinAlgo.AlgorithmID, err)
	}

	fpsTiersRaw := []byte(`[{"fps":5,"units":60},{"fps":15,"units":150},{"fps":30,"units":300}]`)
	configSchemaRaw := []byte(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","title":"通用目标检测配置","description":"系统内置通用目标检测算法配置，支持多目标类别选择与自定义业务标签","additionalProperties":false,"required":["confidence_threshold","iou_threshold","target_classes"],"properties":{"confidence_threshold":{"type":"number","title":"置信度阈值","description":"检测框置信度低于该值的检测结果将被过滤","minimum":0,"maximum":1,"default":0.45},"iou_threshold":{"type":"number","title":"IoU 阈值","description":"非极大值抑制使用的交并比阈值","minimum":0,"maximum":1,"default":0.45},"target_classes":{"type":"array","title":"检测目标类别","description":"选择需要检测和跟踪的目标类别（支持多选）","items":{"type":"string","enum":["person","bicycle","car","motorcycle","airplane","bus","train","truck","boat","traffic light","fire hydrant","stop sign","parking meter","bench","bird","cat","dog","horse","sheep","cow","elephant","bear","zebra","giraffe","backpack","umbrella","handbag","tie","suitcase","frisbee","skis","snowboard","sports ball","kite","baseball bat","baseball glove","skateboard","surfboard","tennis racket","bottle","wine glass","cup","fork","knife","spoon","bowl","banana","apple","sandwich","orange","broccoli","carrot","hot dog","pizza","donut","cake","chair","couch","potted plant","bed","dining table","toilet","tv","laptop","mouse","remote","keyboard","cell phone","microwave","oven","toaster","sink","refrigerator","book","clock","vase","scissors","teddy bear","hair drier","toothbrush"]},"uniqueItems":true,"minItems":1,"default":["person","car","motorcycle","bicycle","bus","truck"]},"custom_alarm_label":{"type":"string","title":"自定义业务标签","description":"为该任务生成的告警附加自定义场景标识或业务备注（可选）","maxLength":64,"default":""}}}`)
	manifestRaw := []byte(`{"manifest_version":1,"algorithm_id":"general_detection","version":"1.0.0","name":"General Object Detection","description":"Built-in general object detection model powered by CoreML on Apple Silicon with customizable target class filtering","algorithm_type":"object_detection","alarm_type_id":"object_detect","platform_id":"macos-arm64-coreml","min_adapter_version":"1.0.0","runtime_constraints":{"min_os_version":"14.0"},"resource_profile":{"min_free_memory_mb":256,"fps_tiers":[{"fps":5,"units":60},{"fps":15,"units":150},{"fps":30,"units":300}]},"self_test":{"timeout_ms":10000,"input_mode":"test_image"}}`)

	builtinVersion := AlgorithmVersion{
		AlgorithmID:       "general_detection",
		Version:           "1.0.0",
		PlatformID:        "macos-arm64-coreml",
		MinAdapterVersion: "1.0.0",
		PackageRoot:       "var/packages/general_detection/1.0.0",
		FPSTiers:          fpsTiersRaw,
		ConfigSchema:      configSchemaRaw,
		ManifestRaw:       manifestRaw,
		PackageSizeBytes:  0,
		IsActive:          true,
		IsBuiltin:         true,
	}
	if err := tx.Where("algorithm_id = ? AND version = ? AND platform_id = ?", builtinVersion.AlgorithmID, builtinVersion.Version, builtinVersion.PlatformID).FirstOrCreate(&builtinVersion).Error; err != nil {
		return fmt.Errorf("seed builtin algorithm version (%s:%s): %w", builtinVersion.AlgorithmID, builtinVersion.Version, err)
	}

	// 内置算法: 车牌识别 (license_plate_recognition)
	lprAlgo := Algorithm{
		AlgorithmID:   "license_plate_recognition",
		Name:          "车牌识别",
		AlgorithmType: "license_plate_recognition",
		AlarmTypeID:   "",
		ActiveVersion: "1.0.0",
		Description:   "系统内置多语言车牌识别算法，支持中国标准车牌及国际车牌检测与高精度文本识别",
		IsBuiltin:     true,
	}
	if err := tx.Where("algorithm_id = ?", lprAlgo.AlgorithmID).FirstOrCreate(&lprAlgo).Error; err != nil {
		return fmt.Errorf("seed builtin algorithm (%s): %w", lprAlgo.AlgorithmID, err)
	}

	lprFpsTiersRaw := []byte(`[{"fps":5,"units":100},{"fps":15,"units":250},{"fps":30,"units":500}]`)
	lprConfigSchemaRaw := []byte(`{"$schema":"http://json-schema.org/draft-07/schema#","title":"车牌识别配置","type":"object","properties":{"confidence_threshold":{"type":"number","title":"置信度阈值","minimum":0,"maximum":1,"default":0.5,"description":"车牌检测框置信度阈值，低于该值的候选目标将被过滤"},"iou_threshold":{"type":"number","title":"IoU 阈值","minimum":0,"maximum":1,"default":0.45,"description":"非极大值抑制（NMS）使用的交并比重叠度阈值"},"ocr_confidence_threshold":{"type":"number","title":"字符识别置信度阈值","minimum":0,"maximum":1,"default":0.6,"description":"车牌文本识别最低置信度阈值，低于该阈值的模糊字符将被过滤"},"voting_window_frames":{"type":"integer","title":"多帧平滑投票窗口","minimum":1,"maximum":30,"default":5,"description":"连续跟踪观测窗口帧数，用于多帧置信度加权投票以获得最稳定的识别结果"},"observation_cooldown_seconds":{"type":"integer","title":"抓拍冷却时间 (秒)","minimum":1,"maximum":3600,"default":10,"description":"同一车辆轨迹连续被捕获时，两次上报抓拍记录之间的最小冷却时间（秒）"},"allowed_plate_colors":{"type":"array","title":"允许上报车牌颜色","items":{"type":"string","enum":["black","blue","green","white","yellow"]},"minItems":1,"uniqueItems":true,"default":["black","blue","green","white","yellow"],"description":"选择允许抓拍上报的车牌颜色集合（支持多选，默认全部）"},"save_plate_crop":{"type":"boolean","title":"保存车牌特写抠图","default":true,"description":"是否在生成抓拍记录时自动保存车牌局部特写高清切图"}},"additionalProperties":false}`)
	lprManifestRaw := []byte(`{"manifest_version":1,"algorithm_id":"license_plate_recognition","version":"1.0.0","name":"License Plate Recognition","description":"Universal multilingual vehicle license plate detection, perspective rectification, and PP-OCRv4 text recognition on Apple Silicon Core ML","algorithm_type":"license_plate_recognition","platform_id":"macos-arm64-coreml","min_adapter_version":"1.0.0","runtime_constraints":{"min_os_version":"14.0"},"resource_profile":{"min_free_memory_mb":256,"fps_tiers":[{"fps":5,"units":100},{"fps":15,"units":250},{"fps":30,"units":500}]},"self_test":{"timeout_ms":10000,"input_mode":"test_image"}}`)

	lprVersion := AlgorithmVersion{
		AlgorithmID:       "license_plate_recognition",
		Version:           "1.0.0",
		PlatformID:        "macos-arm64-coreml",
		MinAdapterVersion: "1.0.0",
		PackageRoot:       "var/packages/license_plate_recognition/1.0.0",
		FPSTiers:          lprFpsTiersRaw,
		ConfigSchema:      lprConfigSchemaRaw,
		ManifestRaw:       lprManifestRaw,
		PackageSizeBytes:  0,
		IsActive:          true,
		IsBuiltin:         true,
	}
	if err := tx.Where("algorithm_id = ? AND version = ? AND platform_id = ?", lprVersion.AlgorithmID, lprVersion.Version, lprVersion.PlatformID).FirstOrCreate(&lprVersion).Error; err != nil {
		return fmt.Errorf("seed builtin algorithm version (%s:%s): %w", lprVersion.AlgorithmID, lprVersion.Version, err)
	}

	return nil
}

func seedMenuBranch(tx *gorm.DB, roleID, parentID uint64, items []seedMenuItem) error {
	for i, item := range items {
		m := Menu{
			ParentID:   parentID,
			Type:       item.Type,
			Name:       item.Name,
			Title:      item.Title,
			Path:       item.Path,
			Component:  item.Component,
			Icon:       item.Icon,
			Sort:       i + 1,
			Status:     StatusEnabled,
			Permission: item.Permission,
			Affix:      item.Affix,
			KeepAlive:  item.KeepAlive,
		}
		if err := tx.Where("parent_id = ? AND name = ?", parentID, item.Name).FirstOrCreate(&m).Error; err != nil {
			return fmt.Errorf("seed menu %s: %w", item.Name, err)
		}
		rm := RoleMenu{RoleID: roleID, MenuID: m.ID}
		if err := tx.Where("role_id = ? AND menu_id = ?", roleID, m.ID).FirstOrCreate(&rm).Error; err != nil {
			return fmt.Errorf("seed role_menu %s: %w", item.Name, err)
		}
		if err := seedMenuBranch(tx, roleID, m.ID, item.Children); err != nil {
			return err
		}
	}
	return nil
}
