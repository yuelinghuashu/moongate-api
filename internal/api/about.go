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

	// 日期降序；日期相同按 slug 升序，保证并列日期的顺序确定
	// （与 getDocsList 保持一致，避免 map 迭代随机序导致每次请求顺序不同）
	sort.Slice(summaries, func(i, j int) bool {
		if !summaries[i].Date.Equal(summaries[j].Date) {
			return summaries[i].Date.After(summaries[j].Date)
		}
		return summaries[i].Slug < summaries[j].Slug
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
