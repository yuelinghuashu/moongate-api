package api

import (
	"moongate-api/internal/domain"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
)

type AboutHandler struct {
	StoreBySlug map[string]*domain.About
}

func NewAboutHandler(storeBySlug map[string]*domain.About) *AboutHandler {
	return &AboutHandler{StoreBySlug: storeBySlug}
}

// GetAboutList 返回所有 about 页面摘要
// GET /api/about
func (h *AboutHandler) GetAboutList(c *gin.Context) {
	summaries := make([]domain.AboutSummary, 0, len(h.StoreBySlug))
	for _, about := range h.StoreBySlug {
		summaries = append(summaries, domain.AboutSummary{
			Title:       about.Title,
			Description: about.Description,
			Date:        about.Date,
			Slug:        about.Slug,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Date.After(summaries[j].Date)
	})

	c.JSON(http.StatusOK, summaries)
}

// GetAbout 返回单个 about 页面（含 content）
// GET /api/about/:slug
func (h *AboutHandler) GetAbout(c *gin.Context) {
	slug := c.Param("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "slug 参数不能为空"})
		return
	}

	about, ok := h.StoreBySlug[slug]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "页面不存在"})
		return
	}

	c.JSON(http.StatusOK, about)
}
