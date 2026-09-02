// Package router 装配 gin engine 并注册 HTTP 路由。
package router

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "argus/app/docs"
	"argus/app/internal/api"
	"argus/app/internal/middleware"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/response"
	"argus/app/internal/web"
)

const (
	swaggerRoutePath     = "/swagger/*any"
	apiRoutePath         = "/api"
	authRoutePath        = "/auth"
	loginRoutePath       = "/login"
	refreshRoutePath     = "/refresh"
	logoutRoutePath      = "/logout"
	codesRoutePath       = "/codes"
	infoRoutePath        = "/info"
	menuRoutePath        = "/menu"
	roleRoutePath        = "/role"
	deptRoutePath        = "/dept"
	oplogRoutePath       = "/oplog"
	pageRoutePath        = "/page"
	fileRoutePath        = "/file"
	uploadRoutePath      = "/upload"
	idRoutePath          = "/:id"
	userRoutePath        = "/user"
	menuIDsRoutePath     = "/menu-ids"
	menusRoutePath       = "/menus"
	rolesRoutePath       = "/roles"
	statusRoutePath      = "/status"
	profileRoutePath     = "/profile"
	passwordRoutePath    = "/password"
	resetPasswordPath    = "/reset-password"
	batchRoutePath       = "/batch"
	batchStatusRoutePath = "/batch-status"
	ntpRoutePath         = "/ntp"
	configRoutePath      = "/config"
	syncRoutePath        = "/sync"
	setTimeRoutePath     = "/set-time"
	syncedRoutePath      = "/synced"
	cameraRoutePath      = "/camera"
	probeRoutePath       = "/probe"
	personRoutePath      = "/person"
	personIDRoutePath    = "/:personId"
	algorithmRoutePath   = "/algorithm"
	taskRoutePath        = "/task"
	instanceRoutePath    = "/instance"
	availableCamerasPath = "/available-cameras"
	statsRoutePath       = "/stats"
	enabledRoutePath     = "/enabled"
	listRoutePath        = "/list"
	recordRoutePath      = "/record"
	alarmsRoutePath      = "/alarms"
	imagesRoutePath      = "/images"
	openV1RoutePath      = "/v1/open"
)

// Deps 路由依赖集合：新增业务模块时扩展结构体字段，避免 New 签名随之膨胀
// （wire.Struct 按字段自动装配，见 cmd/api/wire.go）。
type Deps struct {
	ErrorHandler            gin.HandlerFunc
	AuthMiddleware          *middleware.AuthMiddleware
	PermMiddleware          *middleware.PermMiddleware
	OplogMiddleware         *middleware.OplogMiddleware
	OpenPersonIPMiddleware  *middleware.OpenPersonIPWhitelistMiddleware
	MenuHandler             *api.MenuHandler
	RoleHandler             *api.RoleHandler
	DepartmentHandler       *api.DepartmentHandler
	OperationLogHandler     *api.OperationLogHandler
	UserHandler             *api.UserHandler
	AuthHandler             *api.AuthHandler
	FileHandler             *api.FileHandler
	NTPHandler              *api.NTPHandler
	NetworkHandler          *api.NetworkHandler
	CameraHandler           *api.CameraHandler
	PersonHandler           *api.PersonHandler
	AlgorithmHandler        *api.AlgorithmHandler
	TaskHandler             *api.TaskHandler
	AlarmRecordHandler      *api.AlarmRecordHandler
	PlateObservationHandler *api.PlateObservationHandler
	FaceObservationHandler  *api.FaceObservationHandler
	FaceCaptureHandler      *api.FaceCaptureHandler
	StorageHandler          *api.StorageHandler
}

// New 创建 gin engine 并注册路由。
func New(cfg *config.Config, deps Deps) *gin.Engine {
	// 与 logger.New 的 zap 解析一致，级别比较大小写不敏感。
	if strings.EqualFold(cfg.Log.Level, "debug") {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	engine := gin.New()
	// 启用方法不匹配时触发 NoMethod（否则统一走 NoRoute，无法区分 404/405）。
	engine.HandleMethodNotAllowed = true
	// 操作日志包住 recovery 与统一错误处理，以便记录最终 HTTP 状态（包括 panic 的 500）。
	engine.Use(deps.OplogMiddleware.Handler)
	engine.Use(gin.CustomRecovery(func(c *gin.Context, _ any) {
		response.WriteFail(c, http.StatusInternalServerError, errno.CodeInternal)
	}))
	// 统一错误处理：位于 recovery 之后、业务路由之前；handler 仅 c.Error，由中间件输出。
	engine.Use(deps.ErrorHandler)
	// 不信任任何代理：ClientIP 直接用 RemoteAddr，避免伪造 X-Forwarded-For。
	// 生产若置于反向代理后，需在此显式配置代理网段。
	_ = engine.SetTrustedProxies(nil)
	// NoRoute / NoMethod 输出统一 404 / 405 响应，或对前端 SPA 路由进行回退。
	engine.NoRoute(func(c *gin.Context) {
		reqPath := c.Request.URL.Path

		// 如果是 API 请求、Swagger 或本地文件上传路径，严格输出 404 JSON，不回退前端页面
		if reqPath == apiRoutePath ||
			strings.HasPrefix(reqPath, apiRoutePath+"/") ||
			reqPath == "/swagger" ||
			strings.HasPrefix(reqPath, "/swagger/") ||
			(cfg.Storage.Driver == config.StorageDriverLocal &&
				cfg.Storage.Local.URLPrefix != "" &&
				(reqPath == cfg.Storage.Local.URLPrefix || strings.HasPrefix(reqPath, cfg.Storage.Local.URLPrefix+"/"))) {
			response.WriteFail(c, http.StatusNotFound, errno.CodeNotFound)
			return
		}

		// 仅对 GET / HEAD 方法进行前端页面与静态资源分发
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
			web.Handler()(c)
			return
		}

		response.WriteFail(c, http.StatusNotFound, errno.CodeNotFound)
	})
	engine.NoMethod(func(c *gin.Context) {
		response.WriteFail(c, http.StatusMethodNotAllowed, errno.CodeMethodNotAllowed)
	})

	// 注册 Swagger 接口文档 UI，访问路径为 /swagger/index.html。
	engine.GET(swaggerRoutePath, ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 本地文件由后端提供公开读取路径；MinIO 文件直接使用存储实现返回的公开 URL。
	if cfg.Storage.Driver == config.StorageDriverLocal && cfg.Storage.Local.Root != "" && cfg.Storage.Local.URLPrefix != "" {
		engine.StaticFS(cfg.Storage.Local.URLPrefix, http.Dir(cfg.Storage.Local.Root))
	}

	apiGroup := engine.Group(apiRoutePath)
	// 所有 API 路由默认先认证，再执行写操作权限默认拒绝；公共认证接口由中间件白名单放行。
	apiGroup.Use(deps.AuthMiddleware.Handler)
	apiGroup.Use(deps.PermMiddleware.Handler)
	{
		authGroup := apiGroup.Group(authRoutePath)
		{
			authGroup.POST(loginRoutePath, deps.AuthHandler.Login)
			authGroup.POST(refreshRoutePath, deps.AuthHandler.RefreshToken)
			authGroup.POST(logoutRoutePath, deps.AuthHandler.Logout)
			authGroup.GET(codesRoutePath, deps.AuthHandler.GetAccessCodes)
		}

		userGroup := apiGroup.Group(userRoutePath)
		{
			userGroup.GET(infoRoutePath, deps.AuthHandler.GetUserInfo)
			userGroup.GET(profileRoutePath, deps.UserHandler.GetProfile)
			userGroup.PUT(profileRoutePath, deps.UserHandler.UpdateProfile)
			userGroup.PUT(profileRoutePath+passwordRoutePath, deps.UserHandler.ChangePassword)
			userGroup.GET(pageRoutePath, deps.UserHandler.GetPage)
			userGroup.POST("", deps.UserHandler.CreateUser)
			userGroup.DELETE(batchRoutePath, deps.UserHandler.BatchDeleteUser)
			userGroup.PUT(batchStatusRoutePath, deps.UserHandler.BatchUpdateStatus)
			userGroup.PUT(idRoutePath, deps.UserHandler.UpdateUser)
			userGroup.DELETE(idRoutePath, deps.UserHandler.DeleteUser)
			userGroup.PUT(idRoutePath+resetPasswordPath, deps.UserHandler.ResetPassword)
			userGroup.GET(idRoutePath+rolesRoutePath, deps.UserHandler.GetRoleIDs)
			userGroup.PUT(idRoutePath+rolesRoutePath, deps.UserHandler.AssignRoles)
			userGroup.PUT(idRoutePath+statusRoutePath, deps.UserHandler.UpdateStatus)
		}
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+userRoutePath+profileRoutePath, middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+userRoutePath+profileRoutePath+passwordRoutePath, middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+userRoutePath, "system:user:add")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+userRoutePath+batchRoutePath, "system:user:delete")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+userRoutePath+batchStatusRoutePath, "system:user:status")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+userRoutePath+idRoutePath, "system:user:edit")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+userRoutePath+idRoutePath, "system:user:delete")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+userRoutePath+idRoutePath+resetPasswordPath, "system:user:reset-password")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+userRoutePath+idRoutePath+rolesRoutePath, "system:user:assign-role")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+userRoutePath+idRoutePath+statusRoutePath, "system:user:status")

		menuGroup := apiGroup.Group(menuRoutePath)
		{
			menuGroup.GET("/tree", deps.MenuHandler.GetMenuTree)
			menuGroup.GET("/all", deps.MenuHandler.GetUserMenuTree)
			menuGroup.POST("", deps.MenuHandler.CreateMenu)
			menuGroup.PUT(idRoutePath, deps.MenuHandler.UpdateMenu)
			menuGroup.DELETE(idRoutePath, deps.MenuHandler.DeleteMenu)
		}
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+menuRoutePath, "system:menu:add")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+menuRoutePath+idRoutePath, "system:menu:edit")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+menuRoutePath+idRoutePath, "system:menu:delete")

		roleGroup := apiGroup.Group(roleRoutePath)
		{
			roleGroup.GET(pageRoutePath, deps.RoleHandler.GetPage)
			roleGroup.POST("", deps.RoleHandler.CreateRole)
			roleGroup.DELETE(batchRoutePath, deps.RoleHandler.BatchDeleteRole)
			roleGroup.PUT(idRoutePath, deps.RoleHandler.UpdateRole)
			roleGroup.DELETE(idRoutePath, deps.RoleHandler.DeleteRole)
			roleGroup.GET(idRoutePath+menuIDsRoutePath, deps.RoleHandler.GetMenuIDs)
			roleGroup.PUT(idRoutePath+menusRoutePath, deps.RoleHandler.AssignMenus)
		}
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+roleRoutePath, "system:role:add")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+roleRoutePath+batchRoutePath, "system:role:delete")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+roleRoutePath+idRoutePath, "system:role:edit")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+roleRoutePath+idRoutePath, "system:role:delete")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+roleRoutePath+idRoutePath+menusRoutePath, "system:role:assign-menu")

		deptGroup := apiGroup.Group(deptRoutePath)
		{
			deptGroup.GET("/tree", deps.DepartmentHandler.GetDeptTree)
			deptGroup.POST("", deps.DepartmentHandler.CreateDept)
			deptGroup.PUT(idRoutePath, deps.DepartmentHandler.UpdateDept)
			deptGroup.DELETE(idRoutePath, deps.DepartmentHandler.DeleteDept)
		}
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+deptRoutePath, "system:dept:add")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+deptRoutePath+idRoutePath, "system:dept:edit")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+deptRoutePath+idRoutePath, "system:dept:delete")

		fileGroup := apiGroup.Group(fileRoutePath)
		{
			fileGroup.POST(uploadRoutePath, deps.FileHandler.Upload)
		}
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+fileRoutePath+uploadRoutePath, middleware.PermCodeAuthenticated)

		oplogGroup := apiGroup.Group(oplogRoutePath)
		{
			oplogGroup.GET(pageRoutePath, deps.OperationLogHandler.GetPage)
			oplogGroup.GET(idRoutePath, deps.OperationLogHandler.GetByID)
		}
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+oplogRoutePath+pageRoutePath, "system:log")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+oplogRoutePath+idRoutePath, "system:log")

		ntpGroup := apiGroup.Group(ntpRoutePath)
		{
			ntpGroup.GET(configRoutePath, deps.NTPHandler.GetConfig)
			ntpGroup.PUT(configRoutePath, deps.NTPHandler.UpdateConfig)
			ntpGroup.GET(statusRoutePath, deps.NTPHandler.GetStatus)
			ntpGroup.POST(syncRoutePath, deps.NTPHandler.SyncNow)
			ntpGroup.POST(setTimeRoutePath, deps.NTPHandler.SetTime)
			ntpGroup.GET(syncedRoutePath, deps.NTPHandler.IsSynced)
		}
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+ntpRoutePath+configRoutePath, "ops:time:read")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+ntpRoutePath+configRoutePath, "ops:time:edit")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+ntpRoutePath+statusRoutePath, "ops:time:read")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+ntpRoutePath+syncRoutePath, "ops:time:edit")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+ntpRoutePath+setTimeRoutePath, "ops:time:edit")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+ntpRoutePath+syncedRoutePath, middleware.PermCodeAuthenticated)

		networkGroup := apiGroup.Group("/network")
		{
			networkGroup.GET("", deps.NetworkHandler.GetOverview)
			networkGroup.GET("/transactions/:transactionId", deps.NetworkHandler.GetTransaction)
			networkGroup.PUT("/interfaces/:interfaceId", deps.NetworkHandler.ApplyInterface)
			networkGroup.PUT("/mode", deps.NetworkHandler.SwitchMode)
			networkGroup.POST("/transactions/:transactionId/confirm", deps.NetworkHandler.ConfirmTransaction)
			networkGroup.POST("/transactions/:transactionId/cancel", deps.NetworkHandler.CancelTransaction)
			networkGroup.POST("/interfaces/:interfaceId/factory-reset", deps.NetworkHandler.FactoryReset)
		}
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+"/network", "ops:network")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+"/network/transactions/:transactionId", "ops:network")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+"/network/interfaces/:interfaceId", "ops:network:edit")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+"/network/mode", "ops:network:mode")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+"/network/transactions/:transactionId/confirm", "ops:network:confirm")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+"/network/transactions/:transactionId/cancel", "ops:network:cancel")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+"/network/interfaces/:interfaceId/factory-reset", "ops:network:reset")

		storageGroup := apiGroup.Group("/storage")
		{
			storageGroup.GET(statusRoutePath, deps.StorageHandler.GetStorageStatus)
			storageGroup.GET(configRoutePath, deps.StorageHandler.GetStorageConfig)
			storageGroup.PUT(configRoutePath, deps.StorageHandler.UpdateStorageConfig)
			storageGroup.POST("/cleanup", deps.StorageHandler.TriggerCleanup)
		}
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+"/storage"+statusRoutePath, "ops:storage:read")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+"/storage"+configRoutePath, "ops:storage:read")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+"/storage"+configRoutePath, "ops:storage:edit")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+"/storage/cleanup", "ops:storage:edit")

		cameraGroup := apiGroup.Group(cameraRoutePath)
		{
			cameraGroup.GET(pageRoutePath, deps.CameraHandler.GetPage)
			cameraGroup.POST("", deps.CameraHandler.CreateCamera)
			cameraGroup.DELETE(batchRoutePath, deps.CameraHandler.BatchDeleteCamera)
			cameraGroup.PUT(idRoutePath, deps.CameraHandler.UpdateCamera)
			cameraGroup.DELETE(idRoutePath, deps.CameraHandler.DeleteCamera)
			cameraGroup.POST(probeRoutePath, deps.CameraHandler.ProbeCamera)
			cameraGroup.POST(idRoutePath+"/preview/start", deps.CameraHandler.StartLivePreview)
			cameraGroup.POST(idRoutePath+"/preview/stop", deps.CameraHandler.StopLivePreview)
		}
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+cameraRoutePath+pageRoutePath, "resource:camera")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+cameraRoutePath, "resource:camera:add")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+cameraRoutePath+batchRoutePath, "resource:camera:delete")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+cameraRoutePath+idRoutePath, "resource:camera:edit")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+cameraRoutePath+idRoutePath, "resource:camera:delete")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+cameraRoutePath+probeRoutePath, "resource:camera:probe")

		personGroup := apiGroup.Group(personRoutePath)
		{
			personGroup.GET(pageRoutePath, deps.PersonHandler.GetPage)
			personGroup.POST("", deps.PersonHandler.CreatePerson)
			personGroup.DELETE(batchRoutePath, deps.PersonHandler.BatchDeletePerson)
			personGroup.PUT(personIDRoutePath, deps.PersonHandler.UpdatePerson)
			personGroup.DELETE(personIDRoutePath, deps.PersonHandler.DeletePerson)
			personGroup.POST(personIDRoutePath+"/faces", deps.PersonHandler.RegisterFace)
			personGroup.GET(personIDRoutePath+"/faces", deps.PersonHandler.ListFaces)
			personGroup.DELETE(personIDRoutePath+"/faces/:faceId", deps.PersonHandler.DeleteFace)
			personGroup.PUT(personIDRoutePath+"/primary-face", deps.PersonHandler.SetPrimaryFace)
			personGroup.GET(personIDRoutePath+"/faces/:faceId/image", deps.PersonHandler.GetRawImage)
			personGroup.GET(personIDRoutePath+"/faces/:faceId/aligned-image", deps.PersonHandler.GetAlignedImage)
		}
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+personRoutePath+pageRoutePath, "resource:person")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+personRoutePath, "resource:person:add")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+personRoutePath+batchRoutePath, "resource:person:delete")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+personRoutePath+personIDRoutePath, "resource:person:edit")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+personRoutePath+personIDRoutePath, "resource:person:delete")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+personRoutePath+personIDRoutePath+"/faces", "resource:person:face:manage")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+personRoutePath+personIDRoutePath+"/faces", "resource:person:face:manage")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+personRoutePath+personIDRoutePath+"/faces/:faceId", "resource:person:face:manage")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+personRoutePath+personIDRoutePath+"/primary-face", "resource:person:face:manage")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+personRoutePath+personIDRoutePath+"/faces/:faceId/image", "resource:person:face:manage")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+personRoutePath+personIDRoutePath+"/faces/:faceId/aligned-image", "resource:person:face:manage")

		algoGroup := apiGroup.Group(algorithmRoutePath)
		{
			algoGroup.GET("", deps.AlgorithmHandler.ListAlgorithms)
			algoGroup.GET(idRoutePath, deps.AlgorithmHandler.GetAlgorithm)
			algoGroup.GET(idRoutePath+"/versions", deps.AlgorithmHandler.ListVersions)
			algoGroup.POST("/upload", deps.AlgorithmHandler.UploadAndInstall)
			algoGroup.PUT(idRoutePath+"/versions/:version/activate", deps.AlgorithmHandler.ActivateVersion)
			algoGroup.DELETE(idRoutePath+"/versions/:version", deps.AlgorithmHandler.UninstallVersion)
		}
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+algorithmRoutePath, "ai:algorithm")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+algorithmRoutePath+idRoutePath, "ai:algorithm")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+algorithmRoutePath+idRoutePath+"/versions", "ai:algorithm")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+algorithmRoutePath+"/upload", "ai:algorithm:upload")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+algorithmRoutePath+idRoutePath+"/versions/:version/activate", "ai:algorithm:activate")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+algorithmRoutePath+idRoutePath+"/versions/:version", "ai:algorithm:uninstall")

		taskGroup := apiGroup.Group(taskRoutePath)
		{
			taskGroup.GET(listRoutePath, deps.TaskHandler.ListTasks)
			taskGroup.GET(statsRoutePath, deps.TaskHandler.GetTaskStats)
			taskGroup.POST("", deps.TaskHandler.CreateTask)
			taskGroup.DELETE(batchRoutePath, deps.TaskHandler.BatchDeleteTasks)
			taskGroup.GET(availableCamerasPath, deps.TaskHandler.ListAvailableCameras)
			taskGroup.PUT("/:cameraId", deps.TaskHandler.UpdateTask)
			taskGroup.PUT("/:cameraId"+enabledRoutePath, deps.TaskHandler.SetTaskEnabled)
			taskGroup.DELETE("/:cameraId", deps.TaskHandler.DeleteTask)

			taskGroup.GET(instanceRoutePath+listRoutePath, deps.TaskHandler.ListInstances)
			taskGroup.POST(instanceRoutePath, deps.TaskHandler.CreateInstance)
			taskGroup.PUT(instanceRoutePath+"/:instanceId", deps.TaskHandler.UpdateInstance)
			taskGroup.PUT(instanceRoutePath+"/:instanceId"+enabledRoutePath, deps.TaskHandler.SetInstanceEnabled)
			taskGroup.DELETE(instanceRoutePath+"/:instanceId", deps.TaskHandler.DeleteInstance)
		}
		// 页面权限 resource:task；按钮权限 add/edit/delete（对齐 camera 的权限注册方式）。
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+taskRoutePath+listRoutePath, "resource:task")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+taskRoutePath+statsRoutePath, "resource:task")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+taskRoutePath, "resource:task:add")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+taskRoutePath+batchRoutePath, "resource:task:delete")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+taskRoutePath+availableCamerasPath, "resource:task:add")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+taskRoutePath+"/:cameraId", "resource:task:edit")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+taskRoutePath+"/:cameraId"+enabledRoutePath, "resource:task:edit")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+taskRoutePath+"/:cameraId", "resource:task:delete")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+taskRoutePath+instanceRoutePath+listRoutePath, "resource:task")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+taskRoutePath+instanceRoutePath, "resource:task:add")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+taskRoutePath+instanceRoutePath+"/:instanceId", "resource:task:edit")
		deps.PermMiddleware.Register(http.MethodPut, apiRoutePath+taskRoutePath+instanceRoutePath+"/:instanceId"+enabledRoutePath, "resource:task:edit")
		deps.PermMiddleware.Register(http.MethodDelete, apiRoutePath+taskRoutePath+instanceRoutePath+"/:instanceId", "resource:task:delete")

		recordGroup := apiGroup.Group(recordRoutePath)
		{
			recordGroup.GET(alarmsRoutePath, deps.AlarmRecordHandler.ListPage)
			recordGroup.GET(alarmsRoutePath+idRoutePath, deps.AlarmRecordHandler.GetDetail)
			recordGroup.GET(imagesRoutePath+idRoutePath, deps.AlarmRecordHandler.ReadImageStream)
			recordGroup.GET("/plates", deps.PlateObservationHandler.ListPage)
			recordGroup.GET("/plates"+idRoutePath, deps.PlateObservationHandler.GetDetail)
			recordGroup.GET("/plates"+idRoutePath+"/panorama", deps.PlateObservationHandler.ReadPanoramaImage)
			recordGroup.GET("/plates"+idRoutePath+"/plate", deps.PlateObservationHandler.ReadPlateImage)
			recordGroup.GET("/faces", deps.FaceObservationHandler.ListPage)
			recordGroup.GET("/faces"+idRoutePath, deps.FaceObservationHandler.GetDetail)
			recordGroup.GET("/faces"+idRoutePath+"/panorama", deps.FaceObservationHandler.ReadPanoramaImage)
			recordGroup.GET("/faces"+idRoutePath+"/face", deps.FaceObservationHandler.ReadFaceImage)
			recordGroup.GET("/captures", deps.FaceCaptureHandler.ListPage)
			recordGroup.GET("/captures"+idRoutePath, deps.FaceCaptureHandler.GetDetail)
			recordGroup.GET("/captures"+idRoutePath+"/panorama", deps.FaceCaptureHandler.ReadPanoramaImage)
			recordGroup.GET("/captures"+idRoutePath+"/face", deps.FaceCaptureHandler.ReadFaceImage)
			recordGroup.GET("/captures"+idRoutePath+"/snapshots/:index/panorama", deps.FaceCaptureHandler.ReadSnapshotPanoramaImage)
			recordGroup.GET("/captures"+idRoutePath+"/snapshots/:index/face", deps.FaceCaptureHandler.ReadSnapshotFaceImage)
		}
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+alarmsRoutePath, "record:alarm")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+alarmsRoutePath+idRoutePath, "record:alarm")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+imagesRoutePath+idRoutePath, middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+"/plates", "record:plate")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+"/plates"+idRoutePath, "record:plate")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+"/plates"+idRoutePath+"/panorama", middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+"/plates"+idRoutePath+"/plate", middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+"/faces", "record:face")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+"/faces"+idRoutePath, "record:face")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+"/faces"+idRoutePath+"/panorama", middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+"/faces"+idRoutePath+"/face", middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+"/captures", "record:capture")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+"/captures"+idRoutePath, "record:capture")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+"/captures"+idRoutePath+"/panorama", middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+"/captures"+idRoutePath+"/face", middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+"/captures"+idRoutePath+"/snapshots/:index/panorama", middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+recordRoutePath+"/captures"+idRoutePath+"/snapshots/:index/face", middleware.PermCodeAuthenticated)

		plateObsGroup := apiGroup.Group("/v1/plate-observations")
		{
			plateObsGroup.GET("", deps.PlateObservationHandler.ListPage)
			plateObsGroup.GET(idRoutePath, deps.PlateObservationHandler.GetDetail)
			plateObsGroup.GET(idRoutePath+"/panorama", deps.PlateObservationHandler.ReadPanoramaImage)
			plateObsGroup.GET(idRoutePath+"/plate", deps.PlateObservationHandler.ReadPlateImage)
		}
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+"/v1/plate-observations", "record:plate")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+"/v1/plate-observations"+idRoutePath, "record:plate")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+"/v1/plate-observations"+idRoutePath+"/panorama", middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+"/v1/plate-observations"+idRoutePath+"/plate", middleware.PermCodeAuthenticated)

		capturesV1Group := apiGroup.Group("/v1/record/captures")
		{
			capturesV1Group.GET("", deps.FaceCaptureHandler.ListPage)
			capturesV1Group.GET(idRoutePath, deps.FaceCaptureHandler.GetDetail)
			capturesV1Group.GET(idRoutePath+"/panorama", deps.FaceCaptureHandler.ReadPanoramaImage)
			capturesV1Group.GET(idRoutePath+"/face", deps.FaceCaptureHandler.ReadFaceImage)
			capturesV1Group.GET(idRoutePath+"/snapshots/:index/panorama", deps.FaceCaptureHandler.ReadSnapshotPanoramaImage)
			capturesV1Group.GET(idRoutePath+"/snapshots/:index/face", deps.FaceCaptureHandler.ReadSnapshotFaceImage)
		}
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+"/v1/record/captures", "record:capture")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+"/v1/record/captures"+idRoutePath, "record:capture")
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+"/v1/record/captures"+idRoutePath+"/panorama", middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+"/v1/record/captures"+idRoutePath+"/face", middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+"/v1/record/captures"+idRoutePath+"/snapshots/:index/panorama", middleware.PermCodeAuthenticated)
		deps.PermMiddleware.Register(http.MethodGet, apiRoutePath+"/v1/record/captures"+idRoutePath+"/snapshots/:index/face", middleware.PermCodeAuthenticated)
	}

	// 外部开放同步 API：位于认证与权限中间件之外，使用受控 IP 白名单保护
	openV1Group := engine.Group(apiRoutePath + openV1RoutePath)
	openV1Group.Use(deps.OpenPersonIPMiddleware.Handler)
	{
		openPersonGroup := openV1Group.Group(personRoutePath)
		{
			openPersonGroup.PUT(personIDRoutePath, deps.PersonHandler.SyncUpsertPerson)
			openPersonGroup.DELETE(personIDRoutePath, deps.PersonHandler.SyncDeletePerson)
		}
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+cameraRoutePath+idRoutePath+"/preview/start", "live:preview:stream")
		deps.PermMiddleware.Register(http.MethodPost, apiRoutePath+cameraRoutePath+idRoutePath+"/preview/stop", "live:preview:stream")
	}

	return engine
}
