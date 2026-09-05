package api

import (
	"encoding/json"
	"moongate-api/internal/domain"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func setupAboutTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	about1 := &domain.About{
		Title:       "My Story",
		Description: "My developer journey",
		Date:        time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Slug:        "story",
		Content:     "<p>Story content</p>",
	}

	about2 := &domain.About{
		Title:       "My Philosophy",
		Description: "How I think about code",
		Date:        time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
		Slug:        "philosophy",
		Content:     "<p>Philosophy content</p>",
	}

	storeBySlug := map[string]*domain.About{
		about1.Slug: about1,
		about2.Slug: about2,
	}

	handler := NewAboutHandler(storeBySlug)
	r.GET("/api/about", handler.GetAboutList)
	r.GET("/api/about/:slug", handler.GetAbout)

	return r
}

func TestGetAboutList(t *testing.T) {
	r := setupAboutTestRouter()

	w := performRequest(r, "GET", "/api/about")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var summaries []domain.AboutSummary
	if err := json.Unmarshal(w.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(summaries) != 2 {
		t.Fatalf("Summaries length = %d, want 2", len(summaries))
	}

	// 按日期降序：philosophy (2025-02) 在 story (2025-01) 前面
	if summaries[0].Slug != "philosophy" {
		t.Errorf("Summaries[0].Slug = %q, want %q", summaries[0].Slug, "philosophy")
	}
	if summaries[1].Slug != "story" {
		t.Errorf("Summaries[1].Slug = %q, want %q", summaries[1].Slug, "story")
	}
}

func TestGetAbout_Existing(t *testing.T) {
	r := setupAboutTestRouter()

	w := performRequest(r, "GET", "/api/about/story")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var about domain.About
	if err := json.Unmarshal(w.Body.Bytes(), &about); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if about.Title != "My Story" {
		t.Errorf("Title = %q, want %q", about.Title, "My Story")
	}
	if about.Content == "" {
		t.Error("Content should be included in single about response")
	}
}

func TestGetAbout_NotFound(t *testing.T) {
	r := setupAboutTestRouter()

	w := performRequest(r, "GET", "/api/about/nonexistent")
	if w.Code != http.StatusNotFound {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusNotFound)
	}
}