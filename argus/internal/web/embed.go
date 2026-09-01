// Package web 内嵌前端 SPA 静态产物并提供 HTTP 服务与路由回退能力。
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed all:dist
var distFS embed.FS

// SubFS 返回剥离了 dist/ 前缀的子文件系统。
func SubFS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// Handler 返回用于分发静态资源与 SPA (HTML5 History 模式) 回退的 Gin HandlerFunc。
func Handler() gin.HandlerFunc {
	subFS := SubFS()
	fileServer := http.FileServer(http.FS(subFS))

	return func(c *gin.Context) {
		reqPath := strings.TrimPrefix(path.Clean(c.Request.URL.Path), "/")

		// 如果请求了具体的静态文件路径且该文件存在且非目录
		if reqPath != "" && reqPath != "." {
			if f, err := subFS.Open(reqPath); err == nil {
				defer f.Close()
				if stat, err := f.Stat(); err == nil && !stat.IsDir() {
					// 对带 hash 的静态资产（如 js/、css/、assets/）设置长效强缓存
					if strings.HasPrefix(reqPath, "assets/") ||
						strings.HasPrefix(reqPath, "js/") ||
						strings.HasPrefix(reqPath, "css/") ||
						strings.HasPrefix(reqPath, "jse/") {
						c.Header("Cache-Control", "public, max-age=31536000, immutable")
					} else {
						// _app-config 等配置文件与 favicon.ico 不使用长期强缓存
						c.Header("Cache-Control", "no-cache")
					}
					fileServer.ServeHTTP(c.Writer, c.Request)
					c.Abort()
					return
				}
			}
		}

		// 页面路由或根路径：回退到 index.html，并设置协商缓存（禁止长期缓存避免版本更新无法刷新）
		indexData, err := fs.ReadFile(subFS, "index.html")
		if err != nil {
			c.String(http.StatusNotFound, "index.html not found")
			c.Abort()
			return
		}

		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexData)
		c.Abort()
	}
}
