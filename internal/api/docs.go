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
	Store       map[string]*domain.Doc // key = permalink（中文/默认）
	StoreBySlug map[string]*domain.Doc // key = slug（中文/默认）
	StoreEn     map[string]*domain.Doc // key = slug（英文译文）
}

// NewDocsHandler 创建一个 DocsHandler 实例
func NewDocsHandler(store map[string]*domain.Doc, storeBySlug map[string]*domain.Doc, storeEn map[string]*domain.Doc) *DocsHandler {
	return &DocsHandler{Store: store, StoreBySlug: storeBySlug, StoreEn: storeEn}
}

// requestLang 解析请求语言参数。
// 只识别 "en" 为英文请求，其余（缺省、zh、ja 等）一律按默认中文处理，向后兼容。
func requestLang(c *gin.Context) string {
	if c.Query("lang") == "en" {
		return "en"
	}
	return "zh"
}

// resolveDoc 按请求语言解析文档。
// 返回解析后的文档、实际语言与是否回退（未命中请求语言）。
// lang=="en" 时优先英文译文，无则回退中文；其余语言优先中文，缺失时兜底英文（防 404）。
func (h *DocsHandler) resolveDoc(slug, lang string) (doc *domain.Doc, resolvedLang string, isFallback bool) {
	if lang == "en" {
		if en, ok := h.StoreEn[slug]; ok {
			return en, "en", false
		}
		if zh, ok := h.StoreBySlug[slug]; ok {
			return zh, "zh", true
		}
		return nil, "", false
	}
	if zh, ok := h.StoreBySlug[slug]; ok {
		return zh, "zh", false
	}
	if en, ok := h.StoreEn[slug]; ok {
		return en, "en", true
	}
	return nil, "", false
}

// hasTranslation 判断该 slug 是否存在英文译文
func (h *DocsHandler) hasTranslation(slug string) bool {
	_, ok := h.StoreEn[slug]
	return ok
}

// buildSummary 按请求语言构建文档摘要（列表页与系列分组共用）
func (h *DocsHandler) buildSummary(doc *domain.Doc, lang string) domain.DocSummary {
	resolved, resolvedLang, isFallback := h.resolveDoc(doc.Slug, lang)
	if resolved == nil {
		resolved = doc
		resolvedLang = "zh"
	}
	return domain.DocSummary{
		Title:          resolved.Title,
		Description:    resolved.Description,
		Date:           resolved.Date,
		Permalink:      resolved.Permalink,
		Slug:           resolved.Slug,
		Level:          resolved.Level,
		Series:         resolved.Series,
		Tags:           resolved.Tags,
		Lang:           resolvedLang,
		IsFallback:     isFallback,
		HasTranslation: h.hasTranslation(doc.Slug),
	}
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
	lang := requestLang(c)                         // 请求语言：en | zh

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

	// 7. 筛选逻辑（title/description/tags 均按请求语言解析）
	filtered := make([]*domain.Doc, 0, len(docs))
	for _, doc := range docs {
		resolved, _, _ := h.resolveDoc(doc.Slug, lang)
		if resolved == nil {
			resolved = doc
		}

		// 7.1. 按 level 筛选
		if level != "" && resolved.Level != domain.Level(strings.ToUpper(level)) {
			continue
		}

		// 7.2. 按搜索关键词筛选（作用于请求语言的标题/摘要）
		if search != "" {
			keyword := strings.ToLower(search)
			matchTitle := strings.Contains(strings.ToLower(resolved.Title), keyword)
			matchDescription := strings.Contains(strings.ToLower(resolved.Description), keyword)

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
		if len(tags) > 0 && !resolved.ContainsAllTags(tags) {
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
		// 返回完整文档（包含 content），按请求语言解析并填语言字段（浅拷贝，不污染共享存储）
		fullDocs := make([]*domain.Doc, 0, len(pagedDocs))
		for _, doc := range pagedDocs {
			resolved, resolvedLang, isFallback := h.resolveDoc(doc.Slug, lang)
			if resolved == nil {
				resolved = doc
				resolvedLang = "zh"
			}
			cp := *resolved
			cp.Lang = resolvedLang
			cp.IsFallback = isFallback
			cp.HasTranslation = h.hasTranslation(doc.Slug)
			fullDocs = append(fullDocs, &cp)
		}
		result = fullDocs
	} else {
		// 返回摘要（不含 content）
		summaries := make([]domain.DocSummary, 0, len(pagedDocs))
		for _, doc := range pagedDocs {
			summaries = append(summaries, h.buildSummary(doc, lang))
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
// GET /api/docs?group=series&lang=en
func (h *DocsHandler) getSeriesGroup(c *gin.Context) {
	lang := requestLang(c)
	groups := make(map[string][]domain.DocSummary)

	for _, doc := range h.Store {
		if doc.Series == nil || *doc.Series == "" {
			continue
		}
		slug := *doc.Series

		groups[slug] = append(groups[slug], h.buildSummary(doc, lang))
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

// GetDoc 返回指定文章的详细内容，支持按请求语言解析（无译文时回退）
// GET /api/docs/:slug?lang=en
func (h *DocsHandler) GetDocBySlug(c *gin.Context) {
	slug := c.Param("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 slug 参数"})
		return
	}

	resolved, resolvedLang, isFallback := h.resolveDoc(slug, requestLang(c))
	if resolved == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在"})
		return
	}

	// 浅拷贝后填充语言字段，避免修改共享存储中的文档
	cp := *resolved
	cp.Lang = resolvedLang
	cp.IsFallback = isFallback
	cp.HasTranslation = h.hasTranslation(slug)

	c.JSON(http.StatusOK, cp)
}
