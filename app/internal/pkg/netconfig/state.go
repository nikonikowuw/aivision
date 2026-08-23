package netconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	CurrentSchemaVersion = 1

	FactoryFilename   = "factory.json"
	LastValidFilename = "last-valid.json"
	PendingFilename   = "pending.json"
	LockFilename      = "lock"
)

// StateEnvelope 包含校验和与元数据的通用包装容器。
type StateEnvelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	Generation    uint64          `json:"generation"`
	Platform      PlatformType    `json:"platform"`
	CreatedAt     time.Time       `json:"createdAt"`
	Checksum      string          `json:"checksum"` // SHA-256 of Data JSON
	Data          json.RawMessage `json:"data"`
}

// FactoryData 工厂出厂基线内容。
type FactoryData struct {
	Plan     HostPlan     `json:"plan"`
	Snapshot HostSnapshot `json:"snapshot"`
}

// LastValidData 经用户确认并生效的最后有效配置。
type LastValidData struct {
	Plan     HostPlan     `json:"plan"`
	Snapshot HostSnapshot `json:"snapshot"`
}

// PendingData 待确认的候选事务持久化数据。
type PendingData struct {
	Transaction PendingTransaction `json:"transaction"`
	Before      HostSnapshot       `json:"before"`
	Candidate   HostPlan           `json:"candidate"`
}

// StateStore 状态存储接口。
type StateStore interface {
	Init(platform PlatformType) error
	GetFactory() (*FactoryData, error)
	SetFactory(data *FactoryData) error
	GetLastValid() (*LastValidData, error)
	SetLastValid(data *LastValidData) error
	ClearLastValid() error
	GetPending() (*PendingData, error)
	SetPending(data *PendingData) error
	ClearPending() error
	Lock() (func(), error)
}

// FileStateStore 基于本地安全 root-only 目录的文件状态存储实现。
type FileStateStore struct {
	dir      string
	platform PlatformType
	mu       sync.Mutex
	fileLock *os.File
}

// NewFileStateStore 创建基于文件的状态存储。
func NewFileStateStore(dir string, platform PlatformType) *FileStateStore {
	return &FileStateStore{
		dir:      dir,
		platform: platform,
	}
}

// Init 确保存储目录存在且具有严格的权限（0700）。
func (s *FileStateStore) Init(platform PlatformType) error {
	s.platform = platform
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create state dir %q: %w", s.dir, err)
	}
	// 确保自身为 0700 目录
	_ = os.Chmod(s.dir, 0o700)
	return nil
}

// Lock 获取跨进程状态锁（及内存互斥）。
func (s *FileStateStore) Lock() (func(), error) {
	s.mu.Lock()
	lockPath := filepath.Join(s.dir, LockFilename)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("open lock file %q: %w", lockPath, err)
	}
	s.fileLock = f

	unlock := func() {
		if s.fileLock != nil {
			_ = s.fileLock.Close()
			s.fileLock = nil
		}
		s.mu.Unlock()
	}
	return unlock, nil
}

func (s *FileStateStore) writeEnvelope(filename string, data any) error {
	rawBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal data: %w", err)
	}

	// 重新规范化 json 确保字段顺序完全一致
	var targetVal any
	if err := json.Unmarshal(rawBytes, &targetVal); err != nil {
		return fmt.Errorf("unmarshal for canonical: %w", err)
	}
	canonicalData, err := json.Marshal(targetVal)
	if err != nil {
		return fmt.Errorf("marshal canonical: %w", err)
	}

	hasher := sha256.New()
	hasher.Write(canonicalData)
	checksum := hex.EncodeToString(hasher.Sum(nil))

	envelope := StateEnvelope{
		SchemaVersion: CurrentSchemaVersion,
		Generation:    uint64(time.Now().UnixNano()),
		Platform:      s.platform,
		CreatedAt:     time.Now().UTC(),
		Checksum:      checksum,
		Data:          canonicalData,
	}

	envBytes, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}

	targetPath := filepath.Join(s.dir, filename)
	tmpPath := fmt.Sprintf("%s.tmp.%d", targetPath, time.Now().UnixNano())

	// 写入临时文件并执行 fsync
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create tmp state file %q: %w", tmpPath, err)
	}
	if _, err := tmpFile.Write(envBytes); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write tmp state file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("sync tmp state file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close tmp state file: %w", err)
	}

	// 原子 rename
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomic rename %q to %q: %w", tmpPath, targetPath, err)
	}

	// 目录 fsync
	if dirFile, err := os.Open(s.dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}

	return nil
}

func (s *FileStateStore) readEnvelope(filename string, target any) error {
	targetPath := filepath.Join(s.dir, filename)
	data, err := os.ReadFile(targetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		return fmt.Errorf("read state file %q: %w", targetPath, err)
	}

	var envelope StateEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("%w: unmarshal envelope %q: %v", ErrStateCorrupt, targetPath, err)
	}

	if envelope.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("%w: unknown schema version %d in %q", ErrStateCorrupt, envelope.SchemaVersion, targetPath)
	}

	// 重新规范化 json 比较或者对原始 raw data 校验
	var targetVal any
	if err := json.Unmarshal(envelope.Data, &targetVal); err != nil {
		return fmt.Errorf("%w: unmarshal data in %q: %v", ErrStateCorrupt, targetPath, err)
	}
	canonicalData, err := json.Marshal(targetVal)
	if err != nil {
		return fmt.Errorf("%w: marshal canonical data: %v", ErrStateCorrupt, err)
	}

	hasher := sha256.New()
	hasher.Write(canonicalData)
	computed := hex.EncodeToString(hasher.Sum(nil))
	if computed != envelope.Checksum {
		return fmt.Errorf("%w: checksum mismatch in %q: got %s, want %s", ErrStateCorrupt, targetPath, computed, envelope.Checksum)
	}

	if err := json.Unmarshal(canonicalData, target); err != nil {
		return fmt.Errorf("%w: unmarshal data into target in %q: %v", ErrStateCorrupt, targetPath, err)
	}

	return nil
}

func (s *FileStateStore) GetFactory() (*FactoryData, error) {
	var data FactoryData
	if err := s.readEnvelope(FactoryFilename, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (s *FileStateStore) SetFactory(data *FactoryData) error {
	// 出厂基线只允许创建一次，防止被覆盖
	targetPath := filepath.Join(s.dir, FactoryFilename)
	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("factory baseline already exists and is immutable")
	}
	return s.writeEnvelope(FactoryFilename, data)
}

func (s *FileStateStore) GetLastValid() (*LastValidData, error) {
	var data LastValidData
	if err := s.readEnvelope(LastValidFilename, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (s *FileStateStore) SetLastValid(data *LastValidData) error {
	return s.writeEnvelope(LastValidFilename, data)
}

func (s *FileStateStore) ClearLastValid() error {
	targetPath := filepath.Join(s.dir, LastValidFilename)
	if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *FileStateStore) GetPending() (*PendingData, error) {
	var data PendingData
	if err := s.readEnvelope(PendingFilename, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (s *FileStateStore) SetPending(data *PendingData) error {
	return s.writeEnvelope(PendingFilename, data)
}

func (s *FileStateStore) ClearPending() error {
	targetPath := filepath.Join(s.dir, PendingFilename)
	if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
