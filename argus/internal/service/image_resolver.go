package service

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"argus/app/internal/pkg/errno"
)

// openImageFileWithFallback 负责在候选基础目录中安全查找并打开目标图片文件。
// 当 isThumbnail 为 true 时，优先尝试匹配 _thumb 后缀的缩略图文件；若缩略图不存在，则平滑回退至原图。
func openImageFileWithFallback(candidates []string, relPath string, isThumbnail bool) (io.ReadCloser, int64, string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return nil, 0, "", errno.NewError(errno.CodeNotFound)
	}

	cleanRel := filepath.Clean(relPath)
	if strings.HasPrefix(cleanRel, "..") || filepath.IsAbs(cleanRel) {
		return nil, 0, "", errno.NewError(errno.CodeNotFound)
	}

	// 1. 优先尝试解析缩略图
	if isThumbnail {
		ext := filepath.Ext(cleanRel)
		base := strings.TrimSuffix(cleanRel, ext)
		thumbRel := filepath.Clean(base + "_thumb" + ext)
		if !strings.HasPrefix(thumbRel, "..") && !filepath.IsAbs(thumbRel) {
			for _, baseDir := range candidates {
				if strings.TrimSpace(baseDir) == "" {
					continue
				}
				absBase, err := filepath.Abs(baseDir)
				if err != nil {
					continue
				}
				targetPath := filepath.Join(absBase, thumbRel)
				currClean := filepath.Clean(targetPath)
				if !strings.HasPrefix(currClean, absBase+string(filepath.Separator)) && currClean != absBase {
					continue
				}
				if info, err := os.Stat(currClean); err == nil && !info.IsDir() {
					if file, err := os.Open(currClean); err == nil {
						contentType := "image/jpeg"
						if strings.HasSuffix(strings.ToLower(currClean), ".png") {
							contentType = "image/png"
						}
						return file, info.Size(), contentType, nil
					}
				}
			}
		}
	}

	// 2. 匹配原图文件
	var cleanTarget string
	var fi os.FileInfo
	for _, baseDir := range candidates {
		if strings.TrimSpace(baseDir) == "" {
			continue
		}
		absBase, err := filepath.Abs(baseDir)
		if err != nil {
			continue
		}
		targetPath := filepath.Join(absBase, cleanRel)
		currClean := filepath.Clean(targetPath)
		if !strings.HasPrefix(currClean, absBase+string(filepath.Separator)) && currClean != absBase {
			continue
		}
		if info, err := os.Stat(currClean); err == nil && !info.IsDir() {
			cleanTarget = currClean
			fi = info
			break
		}
	}

	if cleanTarget == "" || fi == nil {
		return nil, 0, "", errno.NewError(errno.CodeNotFound)
	}

	file, err := os.Open(cleanTarget)
	if err != nil {
		return nil, 0, "", err
	}
	contentType := "image/jpeg"
	if strings.HasSuffix(strings.ToLower(cleanTarget), ".png") {
		contentType = "image/png"
	}
	return file, fi.Size(), contentType, nil
}
