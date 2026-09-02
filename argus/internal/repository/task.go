package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	argusv1 "argus/app/internal/proto/argus/v1"

	"argus/app/internal/model"
)

// ErrRevisionMissing 期望状态版本计数器单行缺失（迁移未初始化或数据被破坏）。
// CurrentRevision / BumpRevision 返回本错误时 adapter fail closed，不让 Engine 拿到
// revision=0 的「配置被清空」快照。
var ErrRevisionMissing = errors.New("repository: desired_state_revision singleton row missing")

// TaskStatsRow 任务管理概览统计聚合结果：未软删任务/实例计数 + 在线（RUNNING）任务数。
type TaskStatsRow struct {
	TotalTasks       int64
	RunningTasks     int64
	TotalInstances   int64
	EnabledInstances int64
}

// TaskFilter 分析任务分页查询条件。
type TaskFilter struct {
	Page       int
	PageSize   int
	CameraID   string
	Name       string // 名称模糊匹配
	Configured *bool  // nil=全部；true=已有算法实例；false=无算法实例
}

// TaskRepository 任务配置模块数据访问接口。
// 改变 DesiredState 内容的写路径必须经 InTx 并在事务内调用 BumpRevision，
// 保证「配置写入 + revision 递增」原子提交（design §3.1 / D4）。
type TaskRepository interface {
	// 任务操作
	CreateTask(ctx context.Context, task *model.AnalysisTask) error
	UpdateTask(ctx context.Context, task *model.AnalysisTask) error
	DeleteTaskCascade(ctx context.Context, cameraID string) (bool, error)      // 同事务软删任务及其全部实例
	DeleteTasksCascade(ctx context.Context, cameraIDs []string) (int64, error) // 同事务批量软删任务及其全部实例
	GetTaskByCameraID(ctx context.Context, cameraID string) (*model.AnalysisTask, error)
	ListTaskPage(ctx context.Context, filter *TaskFilter) ([]model.AnalysisTask, int64, error)
	// GetTaskStats 统计未软删任务数、在线（RUNNING）任务数、实例总数与实际调度实例数，
	// 供任务管理页顶部统计条展示（设计对齐原型 prototype-task.html 的在线任务/实例计数）。
	GetTaskStats(ctx context.Context) (*TaskStatsRow, error)
	CountTasksByCameraID(ctx context.Context, cameraID string) (int64, error) // 供删摄像头保护
	// ListTaskCameraIDs 返回全部未软删任务的 camera_id，供 available-cameras
	// 的 service 层内存过滤（design §8）。
	ListTaskCameraIDs(ctx context.Context) ([]string, error)

	// 实例操作
	CreateInstance(ctx context.Context, inst *model.AlgorithmInstance) error
	UpdateInstance(ctx context.Context, inst *model.AlgorithmInstance) error
	DeleteInstance(ctx context.Context, instanceID string) (bool, error)
	GetInstance(ctx context.Context, instanceID string) (*model.AlgorithmInstance, error)
	ListInstancesByCameraID(ctx context.Context, cameraID string) ([]model.AlgorithmInstance, error)
	ListInstancesByCameraIDs(ctx context.Context, cameraIDs []string) ([]model.AlgorithmInstance, error)
	// ListEnabledInstanceQuotaRows 一次 JOIN 返回全部实际调度实例的配额计价行
	// （实例 + 算法激活版本 + 档位），供 service 层配额累加（避免 N+1）。
	ListEnabledInstanceQuotaRows(ctx context.Context) ([]EnabledInstanceQuotaRow, error)

	// 状态回报：仅状态码变化时调用（design D6）
	UpdateTaskStatus(ctx context.Context, cameraID string, status int8, msg string) error
	UpdateInstanceStatus(ctx context.Context, instanceID string, status int8, msg string) error

	// revision：必须在业务事务内调用
	LockRevision(ctx context.Context) error
	BumpRevision(ctx context.Context) (uint64, error)
	CurrentRevision(ctx context.Context) (uint64, error)

	// InTx 事务包装器：业务写 + BumpRevision 原子提交
	InTx(ctx context.Context, fn func(ctx context.Context, r TaskRepository) error) error

	// LoadDesiredSnapshot 组装全量期望状态（不含 device_id/revision，由 service 适配器填充）
	LoadDesiredSnapshot(ctx context.Context) (*argusv1.DesiredState, error)
	// LoadDesiredState 在同一个可重复读事务中返回 revision 与完整快照，避免并发写入
	// 让任务、实例、激活版本和 revision 来自不同的数据库时点。
	LoadDesiredState(ctx context.Context) (*argusv1.DesiredState, error)
}

type taskRepository struct {
	db *gorm.DB
}

// NewTaskRepository 创建 TaskRepository 实例。
func NewTaskRepository(db *gorm.DB) TaskRepository {
	return &taskRepository{db: db}
}

// ── 任务 ──────────────────────────────────────────────────────────────

func (r *taskRepository) CreateTask(ctx context.Context, task *model.AnalysisTask) error {
	return writeError(r.db.WithContext(ctx).Create(task).Error)
}

func (r *taskRepository) UpdateTask(ctx context.Context, task *model.AnalysisTask) error {
	return writeError(r.db.WithContext(ctx).Save(task).Error)
}

// DeleteTaskCascade 软删除任务及其全部实例；任务不存在或已软删返回 (false, nil)。
// 不自行开事务：调用方（service.DeleteTask）已在 InTx 内调用，r.db 绑定事务连接，
// 两条 Delete 与外层 revision bump 原子提交（design §3.1），避免嵌套事务 savepoint。
func (r *taskRepository) DeleteTaskCascade(ctx context.Context, cameraID string) (bool, error) {
	var task model.AnalysisTask
	if err := r.db.WithContext(ctx).Where("camera_id = ?", cameraID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}

	deletedAt, err := r.nextTaskDeletedAt(ctx, cameraID)
	if err != nil {
		return false, err
	}
	res := r.db.WithContext(ctx).Model(&model.AnalysisTask{}).
		Where("id = ? AND deleted_at = 0", task.ID).
		Update("deleted_at", deletedAt)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil
	}

	// 级联软删该任务下全部实例（实例无法脱离任务存在，见 D9）
	err = r.db.WithContext(ctx).Where("camera_id = ?", cameraID).Delete(&model.AlgorithmInstance{}).Error
	return true, err
}

// nextTaskDeletedAt 为同一 camera_id 生成不会复用历史软删除值的时间戳。
// 复合唯一索引包含 deleted_at；快速删除并重新创建任务后，单纯使用当前毫秒可能与旧记录冲突。
func (r *taskRepository) nextTaskDeletedAt(ctx context.Context, cameraID string) (int64, error) {
	var maxDeletedAt int64
	if err := r.db.WithContext(ctx).Unscoped().Model(&model.AnalysisTask{}).
		Select("COALESCE(MAX(deleted_at), 0)").
		Where("camera_id = ?", cameraID).
		Scan(&maxDeletedAt).Error; err != nil {
		return 0, err
	}

	deletedAt := time.Now().UnixMilli()
	if deletedAt <= maxDeletedAt {
		deletedAt = maxDeletedAt + 1
	}
	return deletedAt, nil
}

// DeleteTasksCascade 批量软删除任务及其全部实例，返回受影响的任务数。
func (r *taskRepository) DeleteTasksCascade(ctx context.Context, cameraIDs []string) (int64, error) {
	if len(cameraIDs) == 0 {
		return 0, nil
	}
	var totalDeleted int64
	for _, cid := range cameraIDs {
		deleted, err := r.DeleteTaskCascade(ctx, cid)
		if err != nil {
			return totalDeleted, err
		}
		if deleted {
			totalDeleted++
		}
	}
	return totalDeleted, nil
}

func (r *taskRepository) GetTaskByCameraID(ctx context.Context, cameraID string) (*model.AnalysisTask, error) {
	var task model.AnalysisTask
	if err := r.db.WithContext(ctx).Where("camera_id = ?", cameraID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &task, nil
}

// ListTaskPage 分页查询任务；支持按摄像头、名称模糊、是否已挂实例筛选，结果按 id 倒序。
func (r *taskRepository) ListTaskPage(ctx context.Context, filter *TaskFilter) ([]model.AnalysisTask, int64, error) {
	db := r.db.WithContext(ctx).Model(&model.AnalysisTask{})
	if filter != nil {
		if filter.CameraID != "" {
			db = db.Where("camera_id = ?", filter.CameraID)
		}
		if filter.Name != "" {
			db = db.Where("name LIKE ?", "%"+filter.Name+"%")
		}
		if filter.Configured != nil {
			// 是否存在未软删实例的 EXISTS 子查询
			cond := "EXISTS (SELECT 1 FROM algorithm_instances ai WHERE ai.camera_id = analysis_tasks.camera_id AND ai.deleted_at = 0)"
			if !*filter.Configured {
				cond = "NOT " + cond
			}
			db = db.Where(cond)
		}
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.AnalysisTask{}, 0, nil
	}

	page, pageSize := normalizePage(0, 0)
	if filter != nil {
		page, pageSize = normalizePage(filter.Page, filter.PageSize)
	}
	var items []model.AnalysisTask
	offset := (page - 1) * pageSize
	if err := db.Order("id desc").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *taskRepository) CountTasksByCameraID(ctx context.Context, cameraID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.AnalysisTask{}).
		Where("camera_id = ?", cameraID).
		Count(&count).Error
	return count, err
}

// GetTaskStats 聚合统计未软删任务/实例计数。
// 使用条件聚合 SUM(CASE WHEN ... THEN 1 ELSE 0 END)，2 次查询替代原先 4 次独立 COUNT，
// 减少 DB round-trip 并兼容 SQLite / PostgreSQL 标准 SQL；deleted_at = 0 手动指定以匹配 soft_delete 插件语义。
func (r *taskRepository) GetTaskStats(ctx context.Context) (*TaskStatsRow, error) {
	row := &TaskStatsRow{}
	if err := r.db.WithContext(ctx).Raw(
		"SELECT COUNT(*) AS total_tasks, "+
			"COALESCE(SUM(CASE WHEN actual_status = ? THEN 1 ELSE 0 END), 0) AS running_tasks "+
			"FROM analysis_tasks WHERE deleted_at = 0",
		model.TaskStatusRunning,
	).Row().Scan(&row.TotalTasks, &row.RunningTasks); err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Raw(
		"SELECT COUNT(*) AS total_instances, "+
			"COALESCE(SUM(CASE WHEN ai.enabled = 1 AND task.desired_enabled = 1 THEN 1 ELSE 0 END), 0) AS enabled_instances "+
			"FROM algorithm_instances ai "+
			"LEFT JOIN analysis_tasks task ON task.camera_id = ai.camera_id AND task.deleted_at = 0 "+
			"WHERE ai.deleted_at = 0",
	).Row().Scan(&row.TotalInstances, &row.EnabledInstances); err != nil {
		return nil, err
	}
	return row, nil
}

func (r *taskRepository) ListTaskCameraIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).
		Model(&model.AnalysisTask{}).
		Pluck("camera_id", &ids).Error
	return ids, err
}

// ── 实例 ──────────────────────────────────────────────────────────────

func (r *taskRepository) CreateInstance(ctx context.Context, inst *model.AlgorithmInstance) error {
	return writeError(r.db.WithContext(ctx).Create(inst).Error)
}

func (r *taskRepository) UpdateInstance(ctx context.Context, inst *model.AlgorithmInstance) error {
	return writeError(r.db.WithContext(ctx).Save(inst).Error)
}

// DeleteInstance 软删除指定实例；不存在或已删除返回 (false, nil)。
func (r *taskRepository) DeleteInstance(ctx context.Context, instanceID string) (bool, error) {
	res := r.db.WithContext(ctx).Where("instance_id = ?", instanceID).Delete(&model.AlgorithmInstance{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *taskRepository) GetInstance(ctx context.Context, instanceID string) (*model.AlgorithmInstance, error) {
	var inst model.AlgorithmInstance
	if err := r.db.WithContext(ctx).Where("instance_id = ?", instanceID).First(&inst).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &inst, nil
}

func (r *taskRepository) ListInstancesByCameraID(ctx context.Context, cameraID string) ([]model.AlgorithmInstance, error) {
	var items []model.AlgorithmInstance
	err := r.db.WithContext(ctx).
		Where("camera_id = ?", cameraID).
		Order("id asc").
		Find(&items).Error
	return items, err
}

func (r *taskRepository) ListInstancesByCameraIDs(ctx context.Context, cameraIDs []string) ([]model.AlgorithmInstance, error) {
	if len(cameraIDs) == 0 {
		return []model.AlgorithmInstance{}, nil
	}
	var items []model.AlgorithmInstance
	err := r.db.WithContext(ctx).
		Where("camera_id IN ?", cameraIDs).
		Order("id asc").
		Find(&items).Error
	return items, err
}

// EnabledInstanceQuotaRow 配额累加行：已启用实例 + 算法激活版本 + FPS 档位。
// AlgoExists 为空表示算法行缺失（LEFT JOIN 未命中）；ActiveVersion 为空表示算法未激活；
// FPSTiers 为空表示激活版本行缺失或已软删——service 层均按「不占资源」跳过（与原 N+1 语义一致）。
type EnabledInstanceQuotaRow struct {
	InstanceID    string        `gorm:"column:instance_id"`
	AlgorithmID   string        `gorm:"column:algorithm_id"`
	AlgoExists    string        `gorm:"column:algo_exists"`
	ActiveVersion string        `gorm:"column:active_version"`
	AnalysisFPS   int32         `gorm:"column:analysis_fps"`
	FPSTiers      model.JSONRaw `gorm:"column:fps_tiers"`
}

// ListEnabledInstanceQuotaRows 一次 JOIN 返回全部实际调度实例的配额计价行，
// 替代原先「每实例 GetAlgorithmByID + GetVersion」的 N+1 查询（design §5 配额累加）。
// 算法行用 LEFT JOIN：算法缺失/未激活的实例仍出现在结果中（AlgoExists/ActiveVersion
// 为空），由 service 层记 warn 并按不占资源跳过——与原 instanceUnits 诊断语义一致。
func (r *taskRepository) ListEnabledInstanceQuotaRows(ctx context.Context) ([]EnabledInstanceQuotaRow, error) {
	var rows []EnabledInstanceQuotaRow
	err := r.db.WithContext(ctx).
		Table("algorithm_instances ai").
		Select("ai.instance_id, ai.algorithm_id, a.algorithm_id AS algo_exists, ai.analysis_fps, a.active_version, av.fps_tiers").
		Joins("JOIN analysis_tasks task ON task.camera_id = ai.camera_id AND task.deleted_at = 0 AND task.desired_enabled = ?", true).
		Joins("LEFT JOIN algorithms a ON a.algorithm_id = ai.algorithm_id AND a.deleted_at = 0").
		Joins("LEFT JOIN algorithm_versions av ON av.algorithm_id = ai.algorithm_id AND av.version = a.active_version AND av.deleted_at = 0").
		Where("ai.enabled = ? AND ai.deleted_at = 0", true).
		Order("ai.id ASC").
		Scan(&rows).Error
	return rows, err
}

// ── 状态回报 ──────────────────────────────────────────────────────────

// UpdateTaskStatus 更新任务实际状态与消息。行不存在或已软删时不返回错误：
// 状态回报是幂等追加（D6），过期回报（任务刚被删除）静默丢弃，
// 避免 Engine 收到失败 ACK 后对已不存在的实体反复重试。
func (r *taskRepository) UpdateTaskStatus(ctx context.Context, cameraID string, status int8, msg string) error {
	return r.db.WithContext(ctx).Model(&model.AnalysisTask{}).
		Where("camera_id = ?", cameraID).
		Updates(map[string]any{"actual_status": status, "status_message": msg}).Error
}

// UpdateInstanceStatus 更新实例实际状态与消息；行不存在时静默丢弃（语义同 UpdateTaskStatus）。
func (r *taskRepository) UpdateInstanceStatus(ctx context.Context, instanceID string, status int8, msg string) error {
	return r.db.WithContext(ctx).Model(&model.AlgorithmInstance{}).
		Where("instance_id = ?", instanceID).
		Updates(map[string]any{"actual_status": status, "status_message": msg}).Error
}

// ── revision ─────────────────────────────────────────────────────────

// LockRevision 在事务内对 desired_state_revision 单行加排他锁，串行化并发配额判定与状态提交。
func (r *taskRepository) LockRevision(ctx context.Context) error {
	var row model.DesiredStateRevision
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", 1).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrRevisionMissing
	}
	return err
}

// BumpRevision 单行计数器 revision+1 并返回新值（UPDATE ... RETURNING，见 design D4）。
// 必须在业务事务内调用：改变 DesiredState 内容的写入与版本递增必须原子提交。
// 返回 0 视为单行缺失：revision 只增不减，合法的 bump 结果恒 ≥ 1。
func (r *taskRepository) BumpRevision(ctx context.Context) (uint64, error) {
	return BumpRevisionTx(ctx, r.db)
}

// BumpRevisionTx 在指定事务连接上执行单行计数器 revision+1 并返回新值。
// TaskRepository.InTx / AlgorithmRepository.InTx 闭包内的 BumpRevision 均落到本实现，
// 保证「业务写 + revision 递增」在同一事务内原子提交（design §3.1/§3.2 / D4）。
// 返回 0 视为单行缺失：revision 只增不减，合法的 bump 结果恒 ≥ 1。
func BumpRevisionTx(ctx context.Context, tx *gorm.DB) (uint64, error) {
	var rev uint64
	err := tx.WithContext(ctx).Raw(
		"UPDATE desired_state_revision SET revision = revision + 1 WHERE id = 1 RETURNING revision",
	).Scan(&rev).Error
	if err != nil {
		return 0, err
	}
	if rev == 0 {
		return 0, ErrRevisionMissing
	}
	return rev, nil
}

// CurrentRevision 返回当前期望状态版本；单行缺失返回 ErrRevisionMissing。
func (r *taskRepository) CurrentRevision(ctx context.Context) (uint64, error) {
	var row model.DesiredStateRevision
	err := r.db.WithContext(ctx).Where("id = ?", 1).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, ErrRevisionMissing
	}
	if err != nil {
		return 0, err
	}
	return uint64(row.Revision), nil
}

// ── 事务 ──────────────────────────────────────────────────────────────

// InTx 在单事务内执行 fn；fn 收到的 TaskRepository 绑定到该事务连接。
// 改变 DesiredState 内容的写路径必须经本方法并调用 BumpRevision，
// 二者同事务提交或同事务回滚（design §3.1 / §10）。
func (r *taskRepository) InTx(ctx context.Context, fn func(ctx context.Context, r TaskRepository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(ctx, &taskRepository{db: tx})
	})
}

// ── 期望状态快照 ──────────────────────────────────────────────────────

// taskSnapshotRow 快照组装行：desired_enabled=true 的任务及其摄像头 rtsp_url。
type taskSnapshotRow struct {
	CameraID string `gorm:"column:camera_id"`
	Name     string `gorm:"column:name"`
	RtspURL  string `gorm:"column:rtsp_url"`
}

// instanceSnapshotRow 快照组装行：enabled=true 的实例及其算法激活版本。
type instanceSnapshotRow struct {
	InstanceID          string        `gorm:"column:instance_id"`
	CameraID            string        `gorm:"column:camera_id"`
	AlgorithmID         string        `gorm:"column:algorithm_id"`
	ActiveVersion       string        `gorm:"column:active_version"`
	AnalysisFPS         int32         `gorm:"column:analysis_fps"`
	ParamsJSON          model.JSONRaw `gorm:"column:params_json"`
	RulesJSON           model.JSONRaw `gorm:"column:rules_json"`
	MotionGateJSON      model.JSONRaw `gorm:"column:motion_gate_json"`
	FaceRecognitionJSON model.JSONRaw `gorm:"column:face_recognition_json"`
}

// activeVersionRow 算法激活版本行。
type activeVersionRow struct {
	AlgorithmID   string `gorm:"column:algorithm_id"`
	ActiveVersion string `gorm:"column:active_version"`
}

// LoadDesiredSnapshot 组装全量期望状态（不含 device_id/revision，由 service 适配器填充）。
// 过滤规则（design §3.3）：
//   - tasks：仅 desired_enabled=true，且其摄像头未软删（JOIN 过滤）；
//   - instances：仅 enabled=true 且所属任务 desired_enabled=true；
//   - instance.algorithm_version 从 algorithms.active_version 动态填充（D11），
//     为空（算法包未激活任何版本）时跳过该实例；
//   - active_package_versions：全部 active_version != ” 的算法。
func (r *taskRepository) LoadDesiredSnapshot(ctx context.Context) (*argusv1.DesiredState, error) {
	return r.loadDesiredSnapshot(ctx)
}

// LoadDesiredState 使用 PostgreSQL REPEATABLE READ 读取 revision 与构成 DesiredState
// 的三组数据。SQLite 单测会忽略隔离级别，但仍验证所有读取共用一个事务连接。
func (r *taskRepository) LoadDesiredState(ctx context.Context) (*argusv1.DesiredState, error) {
	var state *argusv1.DesiredState
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := &taskRepository{db: tx}
		revision, err := txRepo.CurrentRevision(ctx)
		if err != nil {
			return err
		}
		state, err = txRepo.loadDesiredSnapshot(ctx)
		if err != nil {
			return err
		}
		state.Revision = revision
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return state, err
}

func (r *taskRepository) loadDesiredSnapshot(ctx context.Context) (*argusv1.DesiredState, error) {
	var taskRows []taskSnapshotRow
	if err := r.db.WithContext(ctx).
		Model(&model.AnalysisTask{}).
		Select("analysis_tasks.camera_id, cameras.name, cameras.rtsp_url").
		Joins("JOIN cameras ON cameras.camera_id = analysis_tasks.camera_id AND cameras.deleted_at = 0").
		Where("analysis_tasks.desired_enabled = ?", true).
		Order("analysis_tasks.id ASC").
		Scan(&taskRows).Error; err != nil {
		return nil, err
	}

	var instanceRows []instanceSnapshotRow
	if err := r.db.WithContext(ctx).
		Model(&model.AlgorithmInstance{}).
		Select("algorithm_instances.instance_id, algorithm_instances.camera_id, algorithm_instances.algorithm_id, "+
			"algorithms.active_version, algorithm_instances.analysis_fps, "+
			"algorithm_instances.params_json, algorithm_instances.rules_json, algorithm_instances.motion_gate_json, algorithm_instances.face_recognition_json").
		Joins("JOIN algorithms ON algorithms.algorithm_id = algorithm_instances.algorithm_id AND algorithms.deleted_at = 0").
		Where("algorithm_instances.enabled = ?", true).
		Order("algorithm_instances.id ASC").
		Scan(&instanceRows).Error; err != nil {
		return nil, err
	}

	var versionRows []activeVersionRow
	if err := r.db.WithContext(ctx).
		Model(&model.Algorithm{}).
		Select("algorithm_id, active_version").
		Where("active_version != ?", "").
		Order("algorithm_id ASC").
		Scan(&versionRows).Error; err != nil {
		return nil, err
	}

	enabledCameras := make(map[string]struct{}, len(taskRows))
	state := &argusv1.DesiredState{
		Tasks:                 make([]*argusv1.CameraTaskConfig, 0, len(taskRows)),
		Instances:             make([]*argusv1.AlgorithmInstanceConfig, 0, len(instanceRows)),
		ActivePackageVersions: make([]*argusv1.ActivePackageVersion, 0, len(versionRows)),
	}
	for _, row := range taskRows {
		enabledCameras[row.CameraID] = struct{}{}
		state.Tasks = append(state.Tasks, &argusv1.CameraTaskConfig{
			CameraId:   row.CameraID,
			CameraName: row.Name,
			RtspUrl:    row.RtspURL,
			Enabled:    true,
		})
	}
	for _, row := range instanceRows {
		if _, ok := enabledCameras[row.CameraID]; !ok {
			continue // 所属任务停用或已软删，不进快照
		}
		if row.ActiveVersion == "" {
			continue // 算法包未激活任何版本，跳过该实例（design §3.3）
		}
		rules, err := parseRulesJSON(row.InstanceID, row.RulesJSON)
		if err != nil {
			return nil, err
		}
		var mgCfg *argusv1.MotionGateConfig
		if len(row.MotionGateJSON) > 0 {
			var mg model.MotionGateConfig
			if err := json.Unmarshal(row.MotionGateJSON, &mg); err == nil {
				mgCfg = &argusv1.MotionGateConfig{
					Enabled:             mg.Enabled,
					Threshold:           mg.Threshold,
					ContourArea:         mg.ContourArea,
					FrameAlpha:          mg.FrameAlpha,
					KeepaliveIntervalMs: mg.KeepaliveIntervalMs,
				}
			}
		}
		var frCfg *argusv1.FaceRecognitionConfig
		if len(row.FaceRecognitionJSON) > 0 {
			var values map[string]json.RawMessage
			if err := json.Unmarshal(row.FaceRecognitionJSON, &values); err != nil {
				return nil, fmt.Errorf("parse face_recognition_json of instance %s: %w", row.InstanceID, err)
			}
			if rawThreshold, ok := values["similarityThreshold"]; ok {
				var threshold float32
				if err := json.Unmarshal(rawThreshold, &threshold); err != nil ||
					math.IsNaN(float64(threshold)) || math.IsInf(float64(threshold), 0) ||
					threshold < 0 || threshold > 1 {
					return nil, fmt.Errorf("face similarity threshold of instance %s is invalid", row.InstanceID)
				}
				frCfg = &argusv1.FaceRecognitionConfig{SimilarityThreshold: threshold}
			}
		}
		state.Instances = append(state.Instances, &argusv1.AlgorithmInstanceConfig{
			InstanceId:       row.InstanceID,
			CameraId:         row.CameraID,
			AlgorithmId:      row.AlgorithmID,
			AlgorithmVersion: row.ActiveVersion,
			AnalysisFps:      row.AnalysisFPS,
			ParamsJson:       string(row.ParamsJSON),
			Enabled:          true,
			Rules:            rules,
			MotionGate:       mgCfg,
			FaceRecognition:  frCfg,
		})
	}
	for _, row := range versionRows {
		state.ActivePackageVersions = append(state.ActivePackageVersions, &argusv1.ActivePackageVersion{
			AlgorithmId: row.AlgorithmID,
			Version:     row.ActiveVersion,
		})
	}
	return state, nil
}

// parseRulesJSON 解析实例规则 JSON 并转换为 proto 形态；非法 JSON 视为内部数据损坏，
// 返回错误让快照组装 fail closed（Engine 不拿到部分规则）。
func parseRulesJSON(instanceID string, raw model.JSONRaw) ([]*argusv1.DetectionRule, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var rules []model.DetectionRule
	if err := json.Unmarshal(raw, &rules); err != nil {
		return nil, fmt.Errorf("parse rules_json of instance %s: %w", instanceID, err)
	}
	return toProtoRules(rules), nil
}

// toProtoRules 把 model.DetectionRule 转换为 proto DetectionRule（坐标 float64 → float32）。
func toProtoRules(rules []model.DetectionRule) []*argusv1.DetectionRule {
	out := make([]*argusv1.DetectionRule, 0, len(rules))
	for _, r := range rules {
		pr := &argusv1.DetectionRule{
			Role:          argusv1.DetectionRuleRole(r.Role),
			LineDirection: argusv1.DetectionLineDirection(r.LineDirection),
			Points:        make([]*argusv1.DetectionPoint, 0, len(r.Points)),
		}
		for _, p := range r.Points {
			pr.Points = append(pr.Points, &argusv1.DetectionPoint{X: float32(p.X), Y: float32(p.Y)})
		}
		out = append(out, pr)
	}
	return out
}
