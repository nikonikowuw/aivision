package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"argus/app/internal/middleware"
	"argus/app/internal/pkg/errno"
)

func TestNewErrorHandler_UnhandledInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.InfoLevel)
	testLogger := zap.New(core)

	r := gin.New()
	r.Use(middleware.NewErrorHandler(testLogger))
	r.GET("/test-unhandled", func(c *gin.Context) {
		_ = c.Error(errors.New("permission denied: disk write failed"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test-unhandled", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", w.Code)
	}

	errorLogs := logs.FilterMessage("unhandled internal error").All()
	if len(errorLogs) != 1 {
		t.Fatalf("expected 1 unhandled internal error log, got %d", len(errorLogs))
	}

	entry := errorLogs[0]
	if entry.ContextMap()["path"] != "/test-unhandled" {
		t.Errorf("expected path /test-unhandled, got %v", entry.ContextMap()["path"])
	}
	if entry.ContextMap()["method"] != http.MethodGet {
		t.Errorf("expected method GET, got %v", entry.ContextMap()["method"])
	}
	if entry.ContextMap()["error"] != "permission denied: disk write failed" {
		t.Errorf("expected error message to match, got %v", entry.ContextMap()["error"])
	}
}

func TestNewErrorHandler_BusinessInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.InfoLevel)
	testLogger := zap.New(core)

	r := gin.New()
	r.Use(middleware.NewErrorHandler(testLogger))
	r.GET("/test-business-internal", func(c *gin.Context) {
		_ = c.Error(errno.NewError(errno.CodeInternal))
	})

	req := httptest.NewRequest(http.MethodGet, "/test-business-internal", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", w.Code)
	}

	errorLogs := logs.FilterMessage("business internal error").All()
	if len(errorLogs) != 1 {
		t.Fatalf("expected 1 business internal error log, got %d", len(errorLogs))
	}
}

func TestNewErrorHandler_DefaultNilLoggerFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.GET("/test-nil-logger", func(c *gin.Context) {
		_ = c.Error(errors.New("some unexpected error"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test-nil-logger", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", w.Code)
	}
}
