package api

import (
	"moongate-api/pkg"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// setupMiddlewareTestRouter 创建一个使用 AuthMiddleware 的测试路由
func setupMiddlewareTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	protected := r.Group("/protected")
	protected.Use(AuthMiddleware())
	protected.GET("/data", func(c *gin.Context) {
		userID := c.GetUint("user_id")
		username := c.GetString("username")
		c.JSON(http.StatusOK, gin.H{
			"user_id":  userID,
			"username": username,
		})
	})

	return r
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	r := setupMiddlewareTestRouter()

	w := performRequest(r, "GET", "/protected/data")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_WrongFormat(t *testing.T) {
	r := setupMiddlewareTestRouter()

	// 使用 Basic 而不是 Bearer
	req := httptest.NewRequest("GET", "/protected/data", nil)
	req.Header.Set("Authorization", "Basic sometoken")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_SinglePartBearer(t *testing.T) {
	r := setupMiddlewareTestRouter()

	// 只有 Bearer 没有 token
	req := httptest.NewRequest("GET", "/protected/data", nil)
	req.Header.Set("Authorization", "Bearer")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	r := setupMiddlewareTestRouter()

	req := httptest.NewRequest("GET", "/protected/data", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	// 确保 JWT_SECRET 已设置
	if err := setUpJWTSecret(); err != nil {
		t.Fatalf("Failed to set JWT_SECRET: %v", err)
	}

	token, err := pkg.GenerateJWT(42, "testuser")
	if err != nil {
		t.Fatalf("Failed to generate JWT: %v", err)
	}

	r := setupMiddlewareTestRouter()

	req := httptest.NewRequest("GET", "/protected/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}
}

// setUpJWTSecret 设置测试 JWT 密钥
func setUpJWTSecret() error {
	if os.Getenv("JWT_SECRET") == "" {
		os.Setenv("JWT_SECRET", "test-secret-key-for-middleware-tests")
	}
	return nil
}
