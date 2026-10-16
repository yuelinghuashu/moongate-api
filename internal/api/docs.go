package api

import (
	"moongate-api/internal/catalog"
	"moongate-api/internal/domain"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// 列表分页默认值与上限。
//
// maxLimit 是真实存在的服务端不变量：?content=true 时单个响应会带上整篇正文，
// 不设上限等于允许任何人用一次请求把全部内容拉走。超限时钳制到 maxLimit，
// 并在响应的 limit 字段回显真实生效值——客户端应按信封里的 limit/totalPages
// 翻页，而不是按自己请求的值（前端 server/utils/docs.ts 就是这么做的）。
const (
	defaultPage  = 1
	defaultLimit = 10
	maxLimit     = 100
)

// DocsHandler 处理技术文章相关的 HTTP 请求。
//
// 它只负责三件事：解析请求参数 → 调用 catalog 查询 → 序列化响应；
// 语言回退、排序、过滤、分页、系列分组都在 catalog 层完成。
type DocsHandler struct {
	catalog *catalog.Catalog
}

// NewDocsHandler 创建一个 DocsHandler 实例。
func NewDocsHandler(c *catalog.Catalog) *DocsHandler {
	return &DocsHandler{catalog: c}
}

// requestLang 解析请求语言参数。
// 只识别 "en" 为英文请求，其余（缺省、zh、ja 等）一律按默认中文处理，向后兼容。
func requestLang(c *gin.Context) string {
	if c.Query("lang") == "en" {
		return catalog.LangEN
	}
	return catalog.LangZH
}

// GetDocs 路由入口：group=series 时返回系列分组，否则返回分页列表。
// GET /api/docs
func (h *DocsHandler) GetDocs(c *gin.Context) {
	if c.Query("group") == "series" {
		c.JSON(http.StatusOK, h.catalog.Series(requestLang(c)))
		return
	}

	// 参数解析与边界保护
	pageNumber, _ := strconv.Atoi(c.DefaultQuery("page", strconv.Itoa(defaultPage)))
	limitNumber, _ := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(defaultLimit)))
	if pageNumber < 1 {
		pageNumber = defaultPage
	}
	if limitNumber < 1 {
		limitNumber = defaultLimit
	}
	if limitNumber > maxLimit {
		limitNumber = maxLimit
	}

	opts := catalog.ListOptions{
		Page:        pageNumber,
		Limit:       limitNumber,
		Search:      c.Query("search"),
		SearchMode:  c.DefaultQuery("searchMode", string(domain.SearchModeAll)),
		Tags:        c.QueryArray("tag"),
		WithContent: c.Query("content") == "true",
	}

	// 参数校验属于 HTTP 层职责：非法 searchMode 直接 400，不必进入查询层
	if !domain.SearchMode(opts.SearchMode).IsValid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "searchMode 参数只能是 all、title 或 description"})
		return
	}

	c.JSON(http.StatusOK, h.catalog.List(requestLang(c), opts))
}

// GetDocBySlug 返回指定文章的详细内容，支持按请求语言解析（无译文时回退）。
// GET /api/docs/:slug
//
// 注意：gin 的 RedirectTrailingSlash 会把 /api/docs/ 重定向到 /api/docs，
// 因此 :slug 不会为空，这里无需再判空。
func (h *DocsHandler) GetDocBySlug(c *gin.Context) {
	detail, ok := h.catalog.Detail(c.Param("slug"), requestLang(c))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在"})
		return
	}

	c.JSON(http.StatusOK, detail)
}
