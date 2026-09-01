package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"argus/app/internal/model"
	"argus/app/internal/pkg/engineipc"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/storage"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
)

const (
	maxPersonIDLen = 64
	maxNameLen     = 64
	maxBatchDelete = 100
)

// PersonDTO 对外公开人员传输对象，严禁包含内部数据库 id。
type PersonDTO struct {
	PersonID      string `json:"personId"`
	Name          string `json:"name"`
	PrimaryFaceID string `json:"primaryFaceId"`
	FaceCount     int64  `json:"faceCount"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

// FaceBoundingBoxDTO 归一化人脸框 DTO。
type FaceBoundingBoxDTO struct {
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

// PersonFaceDTO 对外公开人脸样本传输对象，严禁包含 embedding 原始特征。
type PersonFaceDTO struct {
	FaceID           string              `json:"faceId"`
	AlgorithmID      string              `json:"algorithmId"`
	AlgorithmVersion string              `json:"algorithmVersion"`
	QualityScore     float32             `json:"qualityScore"`
	DetectionScore   float32             `json:"detectionScore"`
	BoundingBox      *FaceBoundingBoxDTO `json:"boundingBox,omitempty"`
	RawImageSize     int64               `json:"rawImageSize"`
	RawImageMime     string              `json:"rawImageMime"`
	AlignedFaceSize  int64               `json:"alignedFaceSize"`
	AlignedFaceMime  string              `json:"alignedFaceMime"`
	IsPrimary        bool                `json:"isPrimary"`
	CreatedAt        string              `json:"createdAt"`
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

// FaceFeatureExtractor 抽象人脸特征提取客户端接口。
type FaceFeatureExtractor interface {
	ExtractFaceFeature(ctx context.Context, req *argusv1.ExtractFaceFeatureRequest, opts ...grpc.CallOption) (*argusv1.ExtractFaceFeatureResponse, error)
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

	RegisterFace(ctx context.Context, personID string, fileHeader *multipart.FileHeader) (*PersonFaceDTO, error)
	ListFaces(ctx context.Context, personID string) ([]*PersonFaceDTO, error)
	DeleteFace(ctx context.Context, personID, faceID string) error
	SetPrimaryFace(ctx context.Context, personID, faceID string) error
	GetRawImage(ctx context.Context, personID, faceID string) (io.ReadCloser, string, int64, error)
	GetAlignedImage(ctx context.Context, personID, faceID string) (io.ReadCloser, string, int64, error)
}

type personService struct {
	repo      repository.PersonRepository
	faceRepo  repository.PersonFaceRepository
	storage   storage.FileStorage
	extractor FaceFeatureExtractor
}

// NewPersonService 创建 PersonService 实例。
func NewPersonService(
	repo repository.PersonRepository,
	faceRepo repository.PersonFaceRepository,
	store storage.FileStorage,
	extractor *engineipc.EngineClient,
) PersonService {
	return &personService{
		repo:      repo,
		faceRepo:  faceRepo,
		storage:   store,
		extractor: extractor,
	}
}

// NewPersonServiceWithExtractor 供单测注入 mock extractor 创建 PersonService。
func NewPersonServiceWithExtractor(
	repo repository.PersonRepository,
	faceRepo repository.PersonFaceRepository,
	store storage.FileStorage,
	extractor FaceFeatureExtractor,
) PersonService {
	return &personService{
		repo:      repo,
		faceRepo:  faceRepo,
		storage:   store,
		extractor: extractor,
	}
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

	personIDs := make([]string, len(items))
	for i := range items {
		personIDs[i] = items[i].PersonID
	}
	counts, err := s.faceRepo.CountByPersonIDs(ctx, personIDs)
	if err != nil {
		return nil, err
	}

	dtos := make([]*PersonDTO, 0, len(items))
	for i := range items {
		dto := toPersonDTO(&items[i])
		if c, ok := counts[items[i].PersonID]; ok {
			dto.FaceCount = c
		}
		dtos = append(dtos, dto)
	}
	return &PersonPageResult{Items: dtos, Total: total}, nil
}

// CreatePerson 创建人员；已软删除的同标识记录会被恢复并更新姓名。
func (s *personService) CreatePerson(ctx context.Context, input *CreatePersonInput) (*PersonDTO, error) {
	if input == nil {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}
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

// DeletePerson 软删除人员及名下所有人脸样本。
func (s *personService) DeletePerson(ctx context.Context, personID string) error {
	personID = strings.TrimSpace(personID)
	if err := validatePersonIDFormat(personID); err != nil {
		return err
	}
	faces, _ := s.faceRepo.DeleteAllByPersonID(ctx, personID)
	deleted, err := s.repo.Delete(ctx, personID)
	if err != nil {
		return err
	}
	if !deleted {
		return errno.NewError(errno.CodeNotFound)
	}
	for _, f := range faces {
		_ = s.storage.Delete(ctx, f.RawImageKey)
		_ = s.storage.Delete(ctx, f.AlignedFaceKey)
	}
	return nil
}

// BatchDeletePerson 按 personId 批量软删除当前页面选择的人员及样本。
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
	for _, id := range ids {
		faces, _ := s.faceRepo.DeleteAllByPersonID(ctx, id)
		for _, f := range faces {
			_ = s.storage.Delete(ctx, f.RawImageKey)
			_ = s.storage.Delete(ctx, f.AlignedFaceKey)
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

// SyncDeletePerson 对开放同步请求执行幂等软删除并清理关联人脸样本私有存储。
func (s *personService) SyncDeletePerson(ctx context.Context, personID string) error {
	personID = strings.TrimSpace(personID)
	if err := validatePersonIDFormat(personID); err != nil {
		return err
	}
	faces, _ := s.faceRepo.DeleteAllByPersonID(ctx, personID)
	_, err := s.repo.Delete(ctx, personID)
	if err != nil {
		return err
	}
	for _, f := range faces {
		_ = s.storage.Delete(ctx, f.RawImageKey)
		_ = s.storage.Delete(ctx, f.AlignedFaceKey)
	}
	return nil
}

// RegisterFace 为人员注册单张人脸样本。
func (s *personService) RegisterFace(ctx context.Context, personID string, fileHeader *multipart.FileHeader) (*PersonFaceDTO, error) {
	personID = strings.TrimSpace(personID)
	if err := validatePersonIDFormat(personID); err != nil {
		return nil, err
	}
	if fileHeader == nil {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}
	if fileHeader.Size > 10*1024*1024 {
		return nil, errno.NewError(errno.CodeFileTooLarge)
	}

	// 1. 确认人员存在
	if _, err := s.repo.GetByPersonID(ctx, personID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.NewError(errno.CodeNotFound)
		}
		return nil, err
	}

	// 2. 预检数量限制
	count, err := s.faceRepo.CountByPersonID(ctx, personID)
	if err != nil {
		return nil, err
	}
	if count >= 10 {
		return nil, errno.NewError(errno.CodeFaceLimitExceeded)
	}

	// 3. 读取与校验图片
	file, err := fileHeader.Open()
	if err != nil {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}
	defer file.Close()

	rawBytes, err := io.ReadAll(io.LimitReader(file, 10*1024*1024+1))
	if err != nil {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}
	if int64(len(rawBytes)) > 10*1024*1024 {
		return nil, errno.NewError(errno.CodeFileTooLarge)
	}
	if len(rawBytes) == 0 {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}

	mime, ext, ok := detectImageMimeAndExt(rawBytes)
	if !ok {
		return nil, errno.NewError(errno.CodeFileTypeNotAllowed)
	}

	// 4. 计算 SHA-256 全局去重
	hasher := sha256.New()
	hasher.Write(rawBytes)
	rawSHA256 := hex.EncodeToString(hasher.Sum(nil))

	existingFace, err := s.faceRepo.GetActiveFaceBySHA256(ctx, rawSHA256)
	if err == nil && existingFace != nil {
		return nil, errno.NewError(errno.CodeFaceDuplicateImage)
	}

	// 5. 跨进程 RPC 请求 Engine 提取特征
	if s.extractor == nil {
		return nil, errno.NewError(errno.CodeEngineUnavailable)
	}
	resp, err := s.extractor.ExtractFaceFeature(ctx, &argusv1.ExtractFaceFeatureRequest{
		ImageData: rawBytes,
	})
	if err != nil {
		var remoteErr *engineipc.RemoteError
		if errors.As(err, &remoteErr) {
			switch remoteErr.Code {
			case "NO_FACE_DETECTED":
				return nil, errno.NewError(errno.CodeFaceNoFaceDetected)
			case "MULTIPLE_FACES_DETECTED":
				return nil, errno.NewError(errno.CodeFaceMultipleDetected)
			case "FACE_QUALITY_TOO_LOW":
				return nil, errno.NewError(errno.CodeFaceQualityTooLow)
			case "FACE_TOO_SMALL":
				return nil, errno.NewError(errno.CodeFaceTooSmall)
			case "IMAGE_DECODE_FAILED":
				return nil, errno.NewError(errno.CodeFaceImageDecodeFailed)
			case "IMAGE_TOO_LARGE":
				return nil, errno.NewError(errno.CodeFileTooLarge)
			case "ALGORITHM_NOT_AVAILABLE":
				return nil, errno.NewError(errno.CodeFaceAlgoUnavailable)
			default:
				return nil, errno.NewError(errno.CodeInternal)
			}
		}
		return nil, errno.NewError(errno.CodeEngineUnavailable)
	}

	if len(resp.GetEmbedding()) != 2048 || len(resp.GetAlignedFaceImage()) == 0 {
		return nil, errno.NewError(errno.CodeInternal)
	}

	// 6. 持久化存储图片
	faceID := strings.ReplaceAll(uuid.New().String(), "-", "")
	rawKey := fmt.Sprintf("persons/%s/faces/%s_raw%s", personID, faceID, ext)
	alignedKey := fmt.Sprintf("persons/%s/faces/%s_aligned.jpg", personID, faceID)

	_, err = s.storage.Put(ctx, storage.PutInput{
		Key:         rawKey,
		Reader:      bytes.NewReader(rawBytes),
		Size:        int64(len(rawBytes)),
		ContentType: mime,
	})
	if err != nil {
		return nil, err
	}

	alignedBytes := resp.GetAlignedFaceImage()
	_, err = s.storage.Put(ctx, storage.PutInput{
		Key:         alignedKey,
		Reader:      bytes.NewReader(alignedBytes),
		Size:        int64(len(alignedBytes)),
		ContentType: "image/jpeg",
	})
	if err != nil {
		_ = s.storage.Delete(ctx, rawKey)
		return nil, err
	}

	var bboxJSON string
	if resp.GetFaceBox() != nil {
		b := FaceBoundingBoxDTO{
			X:      resp.GetFaceBox().GetX(),
			Y:      resp.GetFaceBox().GetY(),
			Width:  resp.GetFaceBox().GetWidth(),
			Height: resp.GetFaceBox().GetHeight(),
		}
		bytes, _ := json.Marshal(b)
		bboxJSON = string(bytes)
	}

	faceModel := &model.PersonFace{
		PersonID:         personID,
		FaceID:           faceID,
		AlgorithmID:      resp.GetAlgorithmId(),
		AlgorithmVersion: resp.GetAlgorithmVersion(),
		Embedding:        resp.GetEmbedding(),
		QualityScore:     resp.GetQualityScore(),
		DetectionScore:   resp.GetDetectionScore(),
		BoundingBox:      bboxJSON,
		RawImageKey:      rawKey,
		RawImageSHA256:   rawSHA256,
		RawImageSize:     int64(len(rawBytes)),
		RawImageMime:     mime,
		AlignedFaceKey:   alignedKey,
		AlignedFaceSize:  int64(len(alignedBytes)),
		AlignedFaceMime:  "image/jpeg",
	}

	if err := s.faceRepo.Create(ctx, faceModel); err != nil {
		_ = s.storage.Delete(ctx, rawKey)
		_ = s.storage.Delete(ctx, alignedKey)
		if errors.Is(err, repository.ErrLimitExceeded) {
			return nil, errno.NewError(errno.CodeFaceLimitExceeded)
		}
		if errors.Is(err, repository.ErrDuplicateKey) {
			return nil, errno.NewError(errno.CodeFaceDuplicateImage)
		}
		return nil, err
	}

	isPrimary := false
	if person, err := s.repo.GetByPersonID(ctx, personID); err == nil && person != nil {
		if person.PrimaryFaceID == "" {
			if _, updateErr := s.repo.UpdatePrimaryFaceID(ctx, personID, faceID); updateErr == nil {
				isPrimary = true
			}
		}
	}

	return toPersonFaceDTO(faceModel, isPrimary), nil
}

// ListFaces 查询人员的所有有效人脸样本列表。
func (s *personService) ListFaces(ctx context.Context, personID string) ([]*PersonFaceDTO, error) {
	personID = strings.TrimSpace(personID)
	if err := validatePersonIDFormat(personID); err != nil {
		return nil, err
	}
	person, err := s.repo.GetByPersonID(ctx, personID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.NewError(errno.CodeNotFound)
		}
		return nil, err
	}
	faces, err := s.faceRepo.ListByPersonID(ctx, personID)
	if err != nil {
		return nil, err
	}
	dtos := make([]*PersonFaceDTO, 0, len(faces))
	for i := range faces {
		isPrimary := (person.PrimaryFaceID != "" && faces[i].FaceID == person.PrimaryFaceID)
		dtos = append(dtos, toPersonFaceDTO(&faces[i], isPrimary))
	}
	return dtos, nil
}

// DeleteFace 软删除单个人脸样本并清理存储。
func (s *personService) DeleteFace(ctx context.Context, personID, faceID string) error {
	personID = strings.TrimSpace(personID)
	faceID = strings.TrimSpace(faceID)
	if err := validatePersonIDFormat(personID); err != nil || faceID == "" {
		return errno.NewError(errno.CodeInvalidParam)
	}
	deletedFace, err := s.faceRepo.Delete(ctx, personID, faceID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errno.NewError(errno.CodeNotFound)
		}
		return err
	}
	if deletedFace != nil {
		_ = s.storage.Delete(ctx, deletedFace.RawImageKey)
		_ = s.storage.Delete(ctx, deletedFace.AlignedFaceKey)
	}

	// 若删除的是主图，自动将剩余样本中最合适的一张设为主图，若无剩余则置空
	if person, err := s.repo.GetByPersonID(ctx, personID); err == nil && person != nil && person.PrimaryFaceID == faceID {
		remainingFaces, listErr := s.faceRepo.ListByPersonID(ctx, personID)
		newPrimaryID := ""
		if listErr == nil && len(remainingFaces) > 0 {
			newPrimaryID = remainingFaces[0].FaceID
		}
		_, _ = s.repo.UpdatePrimaryFaceID(ctx, personID, newPrimaryID)
	}

	return nil
}

// SetPrimaryFace 设置人员的主图/封面图样本。
func (s *personService) SetPrimaryFace(ctx context.Context, personID, faceID string) error {
	personID = strings.TrimSpace(personID)
	faceID = strings.TrimSpace(faceID)
	if err := validatePersonIDFormat(personID); err != nil || faceID == "" {
		return errno.NewError(errno.CodeInvalidParam)
	}
	// 确认人员存在
	if _, err := s.repo.GetByPersonID(ctx, personID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errno.NewError(errno.CodeNotFound)
		}
		return err
	}
	// 确认该 faceId 存在且属于该人员
	if _, err := s.faceRepo.GetByFaceID(ctx, personID, faceID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errno.NewError(errno.CodeNotFound)
		}
		return err
	}
	if _, err := s.repo.UpdatePrimaryFaceID(ctx, personID, faceID); err != nil {
		return err
	}
	return nil
}

func (s *personService) getFaceImageStream(ctx context.Context, personID, faceID string, selectKey func(f *model.PersonFace) (string, string)) (io.ReadCloser, string, int64, error) {
	personID = strings.TrimSpace(personID)
	faceID = strings.TrimSpace(faceID)
	if err := validatePersonIDFormat(personID); err != nil || faceID == "" {
		return nil, "", 0, errno.NewError(errno.CodeInvalidParam)
	}
	face, err := s.faceRepo.GetByFaceID(ctx, personID, faceID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, "", 0, errno.NewError(errno.CodeNotFound)
		}
		return nil, "", 0, err
	}
	key, mime := selectKey(face)
	rc, size, err := s.storage.Get(ctx, key)
	if err != nil {
		return nil, "", 0, errno.NewError(errno.CodeNotFound)
	}
	return rc, mime, size, nil
}

// GetRawImage 获取人脸样本原始图片流。
func (s *personService) GetRawImage(ctx context.Context, personID, faceID string) (io.ReadCloser, string, int64, error) {
	return s.getFaceImageStream(ctx, personID, faceID, func(f *model.PersonFace) (string, string) {
		return f.RawImageKey, f.RawImageMime
	})
}

// GetAlignedImage 获取人脸样本标准化图片流。
func (s *personService) GetAlignedImage(ctx context.Context, personID, faceID string) (io.ReadCloser, string, int64, error) {
	return s.getFaceImageStream(ctx, personID, faceID, func(f *model.PersonFace) (string, string) {
		return f.AlignedFaceKey, f.AlignedFaceMime
	})
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
	first := rune(id[0])
	if !isASCIILetterOrDigit(first) {
		return errno.NewError(errno.CodeInvalidParam)
	}
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
		PersonID:      p.PersonID,
		Name:          p.Name,
		PrimaryFaceID: p.PrimaryFaceID,
		CreatedAt:     p.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:     p.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

// toPersonFaceDTO 将人脸样本模型映射为公开 DTO。
func toPersonFaceDTO(f *model.PersonFace, isPrimary bool) *PersonFaceDTO {
	if f == nil {
		return nil
	}
	var bbox *FaceBoundingBoxDTO
	if f.BoundingBox != "" {
		var b FaceBoundingBoxDTO
		if err := json.Unmarshal([]byte(f.BoundingBox), &b); err == nil {
			bbox = &b
		}
	}
	return &PersonFaceDTO{
		FaceID:           f.FaceID,
		AlgorithmID:      f.AlgorithmID,
		AlgorithmVersion: f.AlgorithmVersion,
		QualityScore:     f.QualityScore,
		DetectionScore:   f.DetectionScore,
		BoundingBox:      bbox,
		RawImageSize:     f.RawImageSize,
		RawImageMime:     f.RawImageMime,
		AlignedFaceSize:  f.AlignedFaceSize,
		AlignedFaceMime:  f.AlignedFaceMime,
		IsPrimary:        isPrimary,
		CreatedAt:        f.CreatedAt.Format("2006-01-02 15:04:05"),
	}
}

// detectImageMimeAndExt 严格基于文件魔数校验 JPEG/PNG/WebP 格式。
func detectImageMimeAndExt(data []byte) (mime string, ext string, ok bool) {
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg", ".jpg", true
	}
	if len(data) >= 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' &&
		data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A {
		return "image/png", ".png", true
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp", ".webp", true
	}
	return "", "", false
}
