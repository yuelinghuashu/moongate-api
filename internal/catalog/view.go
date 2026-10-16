package catalog

import (
	"moongate-api/internal/domain"
	"strings"
)

// buildSummary 把 entry 组装成列表 / 系列分组的摘要响应。
//
// 这里只做"字段搬运"，所有语言判定已在 entry 里完成，
// 且返回的是新构造的值对象（而非共享 *Doc 的浅拷贝），
// 因此多个并发请求之间不会互相污染。
func (e entry) buildSummary() domain.DocSummary {
	fm := e.doc.Frontmatter
	return domain.DocSummary{
		Title:          fm.Title,
		Description:    fm.Description,
		Date:           fm.Date,
		Slug:           e.doc.Slug,
		Series:         fm.Series,
		Order:          fm.Order,
		Tags:           fm.Tags,
		Lang:           e.lang,
		IsFallback:     e.isFallback,
		HasTranslation: e.hasTranslation,
	}
}

// buildDetail 把 entry 组装成单篇详情响应（含正文 HTML）。
func (e entry) buildDetail() domain.DocDetail {
	fm := e.doc.Frontmatter
	return domain.DocDetail{
		Title:          fm.Title,
		Description:    fm.Description,
		Date:           fm.Date,
		Slug:           e.doc.Slug,
		Series:         fm.Series,
		Order:          fm.Order,
		Tags:           fm.Tags,
		Content:        e.doc.Content,
		Lang:           e.lang,
		IsFallback:     e.isFallback,
		HasTranslation: e.hasTranslation,
	}
}

// matchSearch 判断 entry 是否命中搜索关键词（作用于请求语言的标题 / 摘要）。
func (e entry) matchSearch(keyword, mode string) bool {
	if keyword == "" {
		return true
	}

	kw := strings.ToLower(keyword)
	matchTitle := strings.Contains(strings.ToLower(e.doc.Title), kw)
	matchDescription := strings.Contains(strings.ToLower(e.doc.Description), kw)

	switch mode {
	case string(domain.SearchModeTitle):
		return matchTitle
	case string(domain.SearchModeDescription):
		return matchDescription
	default: // all
		return matchTitle || matchDescription
	}
}

// matchTags 判断 entry 是否包含全部目标标签（不区分大小写）。
func (e entry) matchTags(tags []string) bool {
	return e.doc.ContainsAllTags(tags)
}
