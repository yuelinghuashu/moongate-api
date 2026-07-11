package api

import (
	"moongate-api/internal/domain"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// DocsHandler 处理技术文章相关的 HTTP 请求
type DocsHandler struct {
	Store       map[string]*domain.Doc // key = permalink
	StoreBySlug map[string]*domain.Doc // key = slug
}

// NewDocsHandler 创建一个 DocsHandler 实例
func NewDocsHandler(store map[string]*domain.Doc, storeBySlug map[string]*domain.Doc) *DocsHandler {
	return &DocsHandler{Store: store, StoreBySlug: storeBySlug}
}

// GetDocs 路由入口：根据参数决定返回格式
func (h *DocsHandler) GetDocs(c *gin.Context) {
	if c.Query("group") == "series" {
		h.getSeriesGroup(c)
		return
	}
	h.getDocsList(c)
}

// GetDocs 返回分页后的文章列表，支持按日期排序和筛选
// GET /api/docs?page=1&limit=20&search=vue&level=P3&tag=go&tag=vue&content=true
func (h *DocsHandler) getDocsList(c *gin.Context) {
	// 1. 获取查询参数
	page := c.DefaultQuery("page", "1")
	limit := c.DefaultQuery("limit", "10")
	search := c.Query("search")
	searchMode := domain.SearchMode(c.DefaultQuery("searchMode", "all"))
	level := c.Query("level")
	tags := c.QueryArray("tag")
	includeContent := c.Query("content") == "true" // 是否返回 content

	// 2. 校验 searchMode 参数
	if !searchMode.IsValid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "searchMode 参数只能是 all、title 或 description"})
		return
	}

	// 3. 字符串转整数
	pageNumber, _ := strconv.Atoi(page)
	limitNumber, _ := strconv.Atoi(limit)

	// 4. 边界保护
	if pageNumber < 1 {
		pageNumber = 1
	}
	if limitNumber < 1 {
		limitNumber = 10
	}

	// 5. 把所有文章转成切片
	docs := make([]*domain.Doc, 0, len(h.Store))
	for _, doc := range h.Store {
		docs = append(docs, doc)
	}

	// 6. 按日期排序（最新的在前）
	sort.Slice(docs, func(i, j int) bool {
		return docs[i].Date.After(docs[j].Date)
	})

	// 7. 筛选逻辑
	filtered := make([]*domain.Doc, 0, len(docs))
	for _, doc := range docs {
		// 7.1. 按 level 筛选
		if level != "" && doc.Level != domain.Level(strings.ToUpper(level)) {
			continue
		}

		// 7.2. 按搜索关键词筛选
		if search != "" {
			keyword := strings.ToLower(search)
			matchTitle := strings.Contains(strings.ToLower(doc.Title), keyword)
			matchDescription := strings.Contains(strings.ToLower(doc.Description), keyword)

			switch searchMode {
			case domain.SearchModeTitle:
				if !matchTitle {
					continue
				}
			case domain.SearchModeDescription:
				if !matchDescription {
					continue
				}
			default:
				if !matchTitle && !matchDescription {
					continue
				}
			}
		}

		// 7.3. 按标签筛选
		if len(tags) > 0 && !doc.ContainsAllTags(tags) {
			continue
		}

		// 通过所有筛选
		filtered = append(filtered, doc)
	}

	// 8. 计算总数
	total := len(filtered)

	// 9. 计算起止索引
	start := (pageNumber - 1) * limitNumber
	end := start + limitNumber

	// 10. 边界保护
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	// 11. 分页切片
	pagedDocs := filtered[start:end]

	// 12. 返回分页结果，根据 includeContent 决定返回摘要还是完整内容
	var result interface{}

	if includeContent {
		// 返回完整文档（包含 content）
		result = pagedDocs
	} else {
		// 返回摘要（不含 content）
		summaries := make([]domain.DocSummary, 0, len(pagedDocs))
		for _, doc := range pagedDocs {
			summaries = append(summaries, domain.DocSummary{
				Title:       doc.Title,
				Description: doc.Description,
				Date:        doc.Date,
				Permalink:   doc.Permalink,
				Slug:        doc.Slug,
				Level:       doc.Level,
				Series:      doc.Series,
				Tags:        doc.Tags,
			})
		}
		result = summaries
	}

	c.JSON(http.StatusOK, gin.H{
		"data":       result,
		"total":      total,
		"page":       pageNumber,
		"limit":      limitNumber,
		"totalPages": (total + limitNumber - 1) / limitNumber,
	})
}

// GetSeriesGroup 返回按系列分组的文章列表
// GET /api/docs?group=series
func (h *DocsHandler) getSeriesGroup(c *gin.Context) {
	groups := make(map[string][]domain.DocSummary)

	for _, doc := range h.Store {
		if doc.Series == nil || *doc.Series == "" {
			continue
		}
		slug := *doc.Series

		groups[slug] = append(groups[slug], domain.DocSummary{
			Title:       doc.Title,
			Description: doc.Description,
			Date:        doc.Date,
			Permalink:   doc.Permalink,
			Slug:        doc.Slug,
			Level:       doc.Level,
			Series:      doc.Series,
			Tags:        doc.Tags,
		})
	}
	for slug := range groups {
		sort.Slice(groups[slug], func(i, j int) bool {
			return groups[slug][i].Date.After(groups[slug][j].Date)
		})
	}

	result := make([]domain.SeriesGroup, 0, len(groups))
	for slug, docs := range groups {
		result = append(result, domain.SeriesGroup{
			Slug: slug,
			Docs: docs,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Slug < result[j].Slug
	})

	c.JSON(http.StatusOK, result)
}

// GetDoc 返回指定文章的详细内容
// GET /api/docs/:slug
func (h *DocsHandler) GetDocBySlug(c *gin.Context) {
	slug := c.Param("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 slug 参数"})
		return
	}

	doc, ok := h.StoreBySlug[slug]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在"})
		return
	}

	c.JSON(http.StatusOK, doc)
}
