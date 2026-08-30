package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"argus/app/internal/middleware"
	"argus/app/internal/pkg/config"
)

func TestOpenPersonIPWhitelistMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Open: config.Open{
			PersonSyncAllowedIPs: []string{
				"192.168.1.100",
				"10.0.0.0/24",
				"2001:db8::1",
				"2001:db8:abcd::/48",
			},
		},
	}

	mw := middleware.NewOpenPersonIPWhitelistMiddleware(cfg)

	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(mw.Handler)
	r.PUT("/api/v1/open/person/:personId", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0})
	})

	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		wantStatus int
	}{
		{
			name:       "exact IPv4 allowed",
			remoteAddr: "192.168.1.100:12345",
			wantStatus: http.StatusOK,
		},
		{
			name:       "IPv4 in CIDR allowed",
			remoteAddr: "10.0.0.50:54321",
			wantStatus: http.StatusOK,
		},
		{
			name:       "exact IPv6 allowed",
			remoteAddr: "[2001:db8::1]:12345",
			wantStatus: http.StatusOK,
		},
		{
			name:       "IPv6 in CIDR allowed",
			remoteAddr: "[2001:db8:abcd::10]:12345",
			wantStatus: http.StatusOK,
		},
		{
			name:       "disallowed IPv4",
			remoteAddr: "192.168.1.101:12345",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "disallowed IPv4 with spoofed XFF",
			remoteAddr: "192.168.1.101:12345",
			xff:        "192.168.1.100",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "IPv4 mapped IPv6 allowed",
			remoteAddr: "[::ffff:192.168.1.100]:12345",
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid remote addr",
			remoteAddr: "invalid-host:port",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/v1/open/person/p1", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Fatalf("remoteAddr=%s, got status %d, want %d", tt.remoteAddr, w.Code, tt.wantStatus)
			}
		})
	}
}

func TestOpenPersonIPWhitelistDefaultDeny(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 空白配置默认拒绝所有
	mw := middleware.NewOpenPersonIPWhitelistMiddleware(&config.Config{
		Open: config.Open{
			PersonSyncAllowedIPs: []string{},
		},
	})

	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(mw.Handler)
	r.PUT("/api/v1/open/person/:personId", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0})
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/open/person/p1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden for empty allowed IPs, got %d", w.Code)
	}
}
