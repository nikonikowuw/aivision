package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestWebHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(Handler())

	t.Run("root path returns index.html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
		assert.Equal(t, "no-cache, no-store, must-revalidate", w.Header().Get("Cache-Control"))
		assert.Contains(t, w.Body.String(), "<html")
	})

	t.Run("spa history route returns index.html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/system/user", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
		assert.Equal(t, "no-cache, no-store, must-revalidate", w.Header().Get("Cache-Control"))
		assert.Contains(t, w.Body.String(), "<html")
	})
}
