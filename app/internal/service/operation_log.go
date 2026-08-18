package service

import (
	"context"
	"time"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/repository"
)

// LogPageQuery 日志查询请求参数。
type LogPageQuery struct {
	Page       int        `form:"page"`
	PageSize   int        `form:"pageSize"`
	Username   string     `form:"username"`
	Module     string     `form:"module"`
	StatusCode int        `form:"statusCode"`
	StartTime  *time.Time `form:"startTime" time_format:"2006-01-02T15:04:05Z07:00"`
	EndTime    *time.Time `form:"endTime" time_format:"2006-01-02T15:04:05Z07:00"`
}

// LogPageResult 日志分页结果。
type LogPageResult struct {
	Items []model.OperationLog `json:"items"`
	Total int64                `json:"total"`
}

// OperationLogService 操作日志服务接口。
type OperationLogService interface {
	Record(ctx context.Context, log *model.OperationLog) error
	GetByID(ctx context.Context, id uint64) (*model.OperationLog, error)
	Delete(ctx context.Context, id uint64) error
	BatchDelete(ctx context.Context, ids []uint64) error
	GetPage(ctx context.Context, query *LogPageQuery) (*LogPageResult, error)
}

type operationLogService struct {
	repo repository.OperationLogRepository
}

// NewOperationLogService 创建 OperationLogService 实例。
func NewOperationLogService(repo repository.OperationLogRepository) OperationLogService {
	return &operationLogService{repo: repo}
}

func (s *operationLogService) Record(ctx context.Context, log *model.OperationLog) error {
	return s.repo.Create(ctx, log)
}

func (s *operationLogService) GetByID(ctx context.Context, id uint64) (*model.OperationLog, error) {
	log, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, mapRepoError(err)
	}
	return log, nil
}

func (s *operationLogService) Delete(ctx context.Context, id uint64) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return mapRepoError(err)
	}
	return s.repo.Delete(ctx, id)
}

func (s *operationLogService) BatchDelete(ctx context.Context, ids []uint64) error {
	uniqueIDs, err := normalizeBatchIDs(ids)
	if err != nil {
		return err
	}
	return s.repo.BatchDelete(ctx, uniqueIDs)
}

func (s *operationLogService) GetPage(ctx context.Context, query *LogPageQuery) (*LogPageResult, error) {
	filter := &repository.OperationLogFilter{
		Page:       query.Page,
		PageSize:   query.PageSize,
		Username:   query.Username,
		Module:     query.Module,
		StatusCode: query.StatusCode,
		StartTime:  query.StartTime,
		EndTime:    query.EndTime,
	}

	items, total, err := s.repo.ListPage(ctx, filter)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []model.OperationLog{}
	}
	return &LogPageResult{
		Items: items,
		Total: total,
	}, nil
}
