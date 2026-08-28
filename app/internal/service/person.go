package service

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/repository"
)

const (
	maxPersonIDLen = 64
	maxNameLen     = 64
	maxBatchDelete = 100
)

// PersonDTO 对外公开人员传输对象，严禁包含内部数据库 id。
type PersonDTO struct {
	PersonID  string `json:"personId"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// PersonPageQuery 页面查询参数。
type PersonPageQuery struct {
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"pageSize" binding:"omitempty,min=1,max=100"`
	PersonID string `form:"personId"`
	Name     string `form:"name"`
}

// PersonPageResult 页面分页结果。
type PersonPageResult struct {
	Items []*PersonDTO `json:"items"`
	Total int64        `json:"total"`
}

// CreatePersonInput 页面创建人员入参。
type CreatePersonInput struct {
	PersonID string `json:"personId"`
	Name     string `json:"name" binding:"required"`
}

// UpdatePersonInput 页面/同步更新人员入参。
type UpdatePersonInput struct {
	Name string `json:"name" binding:"required"`
}

// BatchDeletePersonInput 页面批量删除入参。
type BatchDeletePersonInput struct {
	PersonIDs []string `json:"personIds" binding:"required,min=1,max=100"`
}

// PersonService 人员管理与同步业务接口。
type PersonService interface {
	GetPage(ctx context.Context, query *PersonPageQuery) (*PersonPageResult, error)
	CreatePerson(ctx context.Context, input *CreatePersonInput) (*PersonDTO, error)
	UpdatePerson(ctx context.Context, personID string, input *UpdatePersonInput) (*PersonDTO, error)
	DeletePerson(ctx context.Context, personID string) error
	BatchDeletePerson(ctx context.Context, input *BatchDeletePersonInput) error
	SyncUpsertPerson(ctx context.Context, personID string, input *UpdatePersonInput) (*PersonDTO, error)
	SyncDeletePerson(ctx context.Context, personID string) error
}

type personService struct {
	repo repository.PersonRepository
}

// NewPersonService 创建 PersonService 实例。
func NewPersonService(repo repository.PersonRepository) PersonService {
	return &personService{repo: repo}
}

// GetPage 获取人员分页列表。
func (s *personService) GetPage(ctx context.Context, query *PersonPageQuery) (*PersonPageResult, error) {
	items, total, err := s.repo.ListPage(ctx, &repository.PersonFilter{
		Page:     query.Page,
		PageSize: query.PageSize,
		PersonID: strings.TrimSpace(query.PersonID),
		Name:     strings.TrimSpace(query.Name),
	})
	if err != nil {
		return nil, err
	}
	dtos := make([]*PersonDTO, 0, len(items))
	for i := range items {
		dtos = append(dtos, toPersonDTO(&items[i]))
	}
	return &PersonPageResult{Items: dtos, Total: total}, nil
}

// CreatePerson 创建人员；已软删除的同标识记录会被恢复并更新姓名。
func (s *personService) CreatePerson(ctx context.Context, input *CreatePersonInput) (*PersonDTO, error) {
	name, err := validateAndNormalizeName(input.Name)
	if err != nil {
		return nil, err
	}

	rawPersonID := strings.TrimSpace(input.PersonID)
	if rawPersonID == "" {
		rawPersonID = generateUUIDv4NoHyphen()
	} else {
		if err := validatePersonIDFormat(rawPersonID); err != nil {
			return nil, err
		}
	}

	// 检查是否有已有记录（包含已软删除）。
	existing, err := s.findPersonWithDeleted(ctx, rawPersonID)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		if existing.DeletedAt == 0 {
			// 活跃记录已存在 -> 返回 CodePersonIDTaken。
			return nil, errno.NewError(errno.CodePersonIDTaken)
		}
		return s.restoreDeletedPerson(ctx, existing, name)
	}

	person := &model.Person{
		PersonID: rawPersonID,
		Name:     name,
	}
	if err := s.repo.Create(ctx, person); err != nil {
		if errors.Is(err, repository.ErrDuplicateKey) {
			return nil, errno.NewError(errno.CodePersonIDTaken)
		}
		return nil, err
	}
	return toPersonDTO(person), nil
}

// UpdatePerson 更新人员姓名；personId 创建后不可修改。
func (s *personService) UpdatePerson(ctx context.Context, personID string, input *UpdatePersonInput) (*PersonDTO, error) {
	personID = strings.TrimSpace(personID)
	if err := validatePersonIDFormat(personID); err != nil {
		return nil, err
	}
	name, err := validateAndNormalizeName(input.Name)
	if err != nil {
		return nil, err
	}

	updated, err := s.repo.UpdateName(ctx, personID, name)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.NewError(errno.CodeNotFound)
		}
		return nil, err
	}
	return toPersonDTO(updated), nil
}

// DeletePerson 软删除人员；不存在或已删除时返回 CodeNotFound。
func (s *personService) DeletePerson(ctx context.Context, personID string) error {
	personID = strings.TrimSpace(personID)
	if err := validatePersonIDFormat(personID); err != nil {
		return err
	}
	deleted, err := s.repo.Delete(ctx, personID)
	if err != nil {
		return err
	}
	if !deleted {
		return errno.NewError(errno.CodeNotFound)
	}
	return nil
}

// BatchDeletePerson 按 personId 批量软删除当前页面选择的人员。
func (s *personService) BatchDeletePerson(ctx context.Context, input *BatchDeletePersonInput) error {
	if input == nil || len(input.PersonIDs) == 0 || len(input.PersonIDs) > maxBatchDelete {
		return errno.NewError(errno.CodeInvalidParam)
	}
	ids := make([]string, 0, len(input.PersonIDs))
	seen := make(map[string]struct{}, len(input.PersonIDs))
	for _, id := range input.PersonIDs {
		trimmed := strings.TrimSpace(id)
		if err := validatePersonIDFormat(trimmed); err != nil {
			return err
		}
		if _, exists := seen[trimmed]; !exists {
			seen[trimmed] = struct{}{}
			ids = append(ids, trimmed)
		}
	}
	return s.repo.BatchDelete(ctx, ids)
}

// SyncUpsertPerson 对开放同步请求执行幂等新增、更新或恢复。
func (s *personService) SyncUpsertPerson(ctx context.Context, personID string, input *UpdatePersonInput) (*PersonDTO, error) {
	personID = strings.TrimSpace(personID)
	if err := validatePersonIDFormat(personID); err != nil {
		return nil, err
	}
	name, err := validateAndNormalizeName(input.Name)
	if err != nil {
		return nil, err
	}

	existing, err := s.findPersonWithDeleted(ctx, personID)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		if existing.DeletedAt == 0 {
			// 活跃记录 -> 更新姓名。
			updated, err := s.repo.UpdateName(ctx, personID, name)
			if err != nil {
				return nil, err
			}
			return toPersonDTO(updated), nil
		}
		return s.restoreDeletedPerson(ctx, existing, name)
	}

	// 不存在 -> 新建
	person := &model.Person{
		PersonID: personID,
		Name:     name,
	}
	if err := s.repo.Create(ctx, person); err != nil {
		if errors.Is(err, repository.ErrDuplicateKey) {
			// 并发冲突时尝试再次按最新值更新
			return s.SyncUpsertPerson(ctx, personID, input)
		}
		return nil, err
	}
	return toPersonDTO(person), nil
}

// SyncDeletePerson 对开放同步请求执行幂等软删除。
func (s *personService) SyncDeletePerson(ctx context.Context, personID string) error {
	personID = strings.TrimSpace(personID)
	if err := validatePersonIDFormat(personID); err != nil {
		return err
	}
	_, err := s.repo.Delete(ctx, personID)
	return err
}

// findPersonWithDeleted 查询包含软删除记录的人员；未找到时返回 nil,nil。
func (s *personService) findPersonWithDeleted(ctx context.Context, personID string) (*model.Person, error) {
	person, err := s.repo.GetByPersonIDWithDeleted(ctx, personID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	return person, err
}

// restoreDeletedPerson 恢复指定软删除记录并返回公开 DTO。
func (s *personService) restoreDeletedPerson(ctx context.Context, person *model.Person, name string) (*PersonDTO, error) {
	restored, err := s.repo.RestoreAndUpdate(ctx, person.ID, name)
	if err != nil {
		return nil, err
	}
	return toPersonDTO(restored), nil
}

// validatePersonIDFormat 校验 personId 的 ASCII 格式和长度约束。
func validatePersonIDFormat(id string) error {
	if len(id) == 0 || len(id) > maxPersonIDLen {
		return errno.NewError(errno.CodeInvalidParam)
	}
	// 首字符必须为字母或数字
	first := rune(id[0])
	if !isASCIILetterOrDigit(first) {
		return errno.NewError(errno.CodeInvalidParam)
	}
	// 后续字符只允许字母、数字、_、-、.、:
	for i := 1; i < len(id); i++ {
		c := rune(id[i])
		if !isASCIILetterOrDigit(c) && c != '_' && c != '-' && c != '.' && c != ':' {
			return errno.NewError(errno.CodeInvalidParam)
		}
	}
	return nil
}

// isASCIILetterOrDigit 判断 rune 是否为 ASCII 字母或数字。
func isASCIILetterOrDigit(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// validateAndNormalizeName 去除姓名首尾空白并校验 Unicode 长度和控制字符。
func validateAndNormalizeName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", errno.NewError(errno.CodeInvalidParam)
	}
	if utf8.RuneCountInString(trimmed) > maxNameLen {
		return "", errno.NewError(errno.CodeInvalidParam)
	}
	// 拒绝控制字符
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return "", errno.NewError(errno.CodeInvalidParam)
		}
	}
	return trimmed, nil
}

// generateUUIDv4NoHyphen 生成符合 personId 规则的无连字符 UUIDv4。
func generateUUIDv4NoHyphen() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}

// toPersonDTO 将模型映射为不包含内部 id 的公开 DTO。
func toPersonDTO(p *model.Person) *PersonDTO {
	if p == nil {
		return nil
	}
	return &PersonDTO{
		PersonID:  p.PersonID,
		Name:      p.Name,
		CreatedAt: p.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt: p.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}
