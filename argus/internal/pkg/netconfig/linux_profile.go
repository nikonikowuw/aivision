//go:build linux

package netconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// LinuxProfile 定义网络配置 Profile 文件格式 (/etc/aivision/network-profile.json)。
type LinuxProfile struct {
	Version    int                     `json:"version"`
	Interfaces []LinuxProfileInterface `json:"interfaces"`
	Resolver   LinuxProfileResolver    `json:"resolver"`
}

type LinuxProfileInterface struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
}

type LinuxProfileResolver struct {
	Path             string `json:"path"`
	RequireExclusive bool   `json:"requireExclusive"`
}

// LoadProfile 加载并验证 Profile 文件。
func LoadProfile(path string) (*LinuxProfile, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read profile %s: %v", ErrProfileInvalid, path, err)
	}
	var p LinuxProfile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("%w: parse profile %s: %v", ErrProfileInvalid, path, err)
	}
	if p.Version != 1 {
		return nil, fmt.Errorf("%w: unsupported profile version %d (expected 1)", ErrProfileInvalid, p.Version)
	}
	return &p, nil
}

// IsAllowlisted 判断接口名是否在 Profile allowlist 中。
func (p *LinuxProfile) IsAllowlisted(name string) bool {
	if p == nil {
		return false
	}
	for _, iface := range p.Interfaces {
		if iface.Name == name {
			return true
		}
	}
	return false
}

// CheckResolverExclusive 检查 resolver（如 /etc/resolv.conf）是否为本系统独占普通文件。
func (p *LinuxProfile) CheckResolverExclusive() error {
	if p == nil || !p.Resolver.RequireExclusive {
		return nil
	}
	path := p.Resolver.Path
	if path == "" {
		path = "/etc/resolv.conf"
	}
	return validateResolverFile(path)
}

// AnchorStore 管理永久 MAC 锚点 (<state_dir>/anchors.json)。
type AnchorStore struct {
	mu       sync.Mutex
	filePath string
	anchors  map[string]string // ifname -> permMAC
}

type anchorPayload struct {
	Version int               `json:"version"`
	Anchors map[string]string `json:"anchors"`
}

func NewAnchorStore(stateDir string) (*AnchorStore, error) {
	if stateDir == "" {
		return &AnchorStore{
			anchors: make(map[string]string),
		}, nil
	}
	filePath := filepath.Join(stateDir, "anchors.json")
	s := &AnchorStore{
		filePath: filePath,
		anchors:  make(map[string]string),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *AnchorStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.filePath == "" {
		return nil
	}
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read anchors file: %w", err)
	}
	var payload anchorPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("parse anchors file: %w", err)
	}
	if payload.Anchors != nil {
		s.anchors = payload.Anchors
	}
	return nil
}

// CheckOrAnchor 校验或锚定接口的永久 MAC。
func (s *AnchorStore) CheckOrAnchor(name string, permMAC string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if permMAC == "" || permMAC == "00:00:00:00:00:00" {
		return false, fmt.Errorf("%w: interface %s has empty or zero permanent MAC", ErrOwnershipConflict, name)
	}

	existing, exists := s.anchors[name]
	if exists {
		if existing != permMAC {
			return false, fmt.Errorf("%w: interface %s permanent MAC changed from %s to %s", ErrOwnershipConflict, name, existing, permMAC)
		}
		return true, nil
	}

	// 首次接管：记录锚点
	s.anchors[name] = permMAC
	if err := s.saveLocked(); err != nil {
		delete(s.anchors, name)
		return false, err
	}
	return true, nil
}

func (s *AnchorStore) saveLocked() error {
	if s.filePath == "" {
		return nil
	}

	payload := anchorPayload{
		Version: 1,
		Anchors: s.anchors,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal anchors: %w", err)
	}

	if err := atomicWriteFile(s.filePath, data, 0600); err != nil {
		return fmt.Errorf("write anchors file: %w", err)
	}
	return nil
}
