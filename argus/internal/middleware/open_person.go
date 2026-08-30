package middleware

import (
	"net"
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"

	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
)

// OpenPersonIPWhitelistMiddleware 对开放同步接口执行真实 TCP 连接 IP 白名单匹配。
// 绝不读取或信任 X-Forwarded-For 等代理头。
type OpenPersonIPWhitelistMiddleware struct {
	singleIPs []netip.Addr
	prefixes  []netip.Prefix
}

// NewOpenPersonIPWhitelistMiddleware 基于应用启动配置构建白名单匹配器。
func NewOpenPersonIPWhitelistMiddleware(cfg *config.Config) *OpenPersonIPWhitelistMiddleware {
	m := &OpenPersonIPWhitelistMiddleware{
		singleIPs: make([]netip.Addr, 0),
		prefixes:  make([]netip.Prefix, 0),
	}
	if cfg == nil {
		return m
	}
	for _, raw := range cfg.Open.PersonSyncAllowedIPs {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(item); err == nil {
			m.prefixes = append(m.prefixes, prefix)
			continue
		}
		if addr, err := netip.ParseAddr(item); err == nil {
			m.singleIPs = append(m.singleIPs, addr)
			continue
		}
	}
	return m
}

// Handler 执行白名单拦截。
func (m *OpenPersonIPWhitelistMiddleware) Handler(c *gin.Context) {
	// RemoteAddr 格式为 "ip:port" 或 "ip"
	remoteAddr := c.Request.RemoteAddr
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	host = strings.TrimSpace(host)
	addr, err := netip.ParseAddr(host)
	if err != nil {
		c.Error(errno.NewError(errno.CodeForbidden)) //nolint:errcheck
		c.Abort()
		return
	}

	// 规范化 IPv4-mapped IPv6 地址 (如 ::ffff:192.168.1.10)
	addr = addr.Unmap()

	if !m.isAllowed(addr) {
		c.Error(errno.NewError(errno.CodeForbidden)) //nolint:errcheck
		c.Abort()
		return
	}

	c.Next()
}

func (m *OpenPersonIPWhitelistMiddleware) isAllowed(addr netip.Addr) bool {
	for _, allowed := range m.singleIPs {
		if allowed.Unmap() == addr {
			return true
		}
	}
	for _, prefix := range m.prefixes {
		if prefix.Contains(addr) {
			return true
		}
		// Special handling for IPv4-mapped IPv6 in an IPv4 prefix context isn't automatically supported by Contains() if addr was already unmapped.
		// However, unmap() creates an IPv4 if it was mapped, so if prefix is IPv4 and addr is IPv4, Contains() works out-of-the-box in Go 1.18+.
	}
	return false
}
