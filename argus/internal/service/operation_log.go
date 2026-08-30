package service

import (
	"context"
	"time"

	"argus/app/internal/model"
	"argus/app/internal/repository"
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

func (s *operationLogService) GetPage(ctx context.Context, query *LogPageQuery) (*LogPageResult, error) {
	if query.StartTime == nil && query.EndTime == nil {
		now := time.Now()
		startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		// use end of day (23:59:59) + 1 second = start of next day to act as exclusive upper bound
		endOfDay := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		query.StartTime = &startOfDay
		query.EndTime = &endOfDay
	}

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
