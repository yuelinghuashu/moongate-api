package catalog

import (
	"moongate-api/internal/domain"
)

// ListOptions 是列表接口的查询参数（已由 api 层解析成类型化值）。
type ListOptions struct {
	Page        int
	Limit       int
	Search      string
	SearchMode  string // all | title | description
	Tags        []string
	WithContent bool // true 时 data 为 []DocDetail（含正文），否则为 []DocSummary
}

// List 返回分页后的文档列表。
//
// 流程：解析语言 → 排序 → 过滤 → 分页 → 组装响应。
// 每个 slug 只解析一次语言落点，过滤与组装共用同一份 entry。
//
// 参数合法性（如 searchMode）由调用方在进入本层前校验，
// 因此这里不返回 error，也不存在"查询层错误"这类分支。
func (c *Catalog) List(lang string, opts ListOptions) domain.DocsPage {
	// 1. 过滤（关键词 + 标签），保留已经解析好语言的 entry
	filtered := make([]entry, 0, len(c.zh))
	for _, e := range c.candidates(lang) {
		if !e.matchSearch(opts.Search, opts.SearchMode) {
			continue
		}
		if len(opts.Tags) > 0 && !e.matchTags(opts.Tags) {
			continue
		}
		filtered = append(filtered, e)
	}

	// 2. 分页（起止越界保护）
	total := len(filtered)
	start := (opts.Page - 1) * opts.Limit
	end := start + opts.Limit
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	paged := filtered[start:end]

	// 3. 组装响应
	var data any
	if opts.WithContent {
		details := make([]domain.DocDetail, 0, len(paged))
		for _, e := range paged {
			details = append(details, e.buildDetail())
		}
		data = details
	} else {
		summaries := make([]domain.DocSummary, 0, len(paged))
		for _, e := range paged {
			summaries = append(summaries, e.buildSummary())
		}
		data = summaries
	}

	totalPages := 0
	if opts.Limit > 0 {
		totalPages = (total + opts.Limit - 1) / opts.Limit
	}

	return domain.DocsPage{
		Data:       data,
		Total:      total,
		Page:       opts.Page,
		Limit:      opts.Limit,
		TotalPages: totalPages,
	}
}

// Detail 返回单篇文档详情；第二个返回值为 false 表示 slug 不存在。
func (c *Catalog) Detail(slug, lang string) (domain.DocDetail, bool) {
	e, ok := c.resolve(slug, lang)
	if !ok {
		return domain.DocDetail{}, false
	}
	return e.buildDetail(), true
}
