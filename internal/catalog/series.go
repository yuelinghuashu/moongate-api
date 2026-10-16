package catalog

import (
	"sort"

	"moongate-api/internal/domain"
)

// Series 返回按系列分组的文档摘要（裸数组，按系列 slug 升序）。
//
// 组内顺序由 orderSeriesDocs 决定，未被 order 覆盖的篇目按日期升序填充空位。
func (c *Catalog) Series(lang string) []domain.SeriesGroup {
	groups := make(map[string][]domain.DocSummary)

	for _, e := range c.candidates(lang) {
		if e.doc.Series == nil || *e.doc.Series == "" {
			continue
		}
		slug := *e.doc.Series
		groups[slug] = append(groups[slug], e.buildSummary())
	}

	result := make([]domain.SeriesGroup, 0, len(groups))
	for slug, docs := range groups {
		result = append(result, domain.SeriesGroup{
			Slug: slug,
			Docs: orderSeriesDocs(docs),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Slug < result[j].Slug
	})

	return result
}

// orderSeriesDocs 将同系列文档按阅读顺序排序。
//
// 规则：
//   - 显式声明 order 的文章占据其 1-based 序号位置（order 越大越靠后）；
//   - 未声明 order 的文章按日期升序（阅读顺序）填充剩余的空位。
//
// 这样既能让"混入的关联文"通过 order 被移到正确位置，又能让绝大多数
// 按日期升序即自然阅读顺序的系列无需任何改动。
func orderSeriesDocs(docs []domain.DocSummary) []domain.DocSummary {
	n := len(docs)
	if n <= 1 {
		return docs
	}

	// 找出显式声明的 order 取值集合，超出 1..n 范围的视为非法，忽略（用日期兜底）。
	validOrder := make(map[int]bool)
	for _, d := range docs {
		if d.Order != nil && *d.Order >= 1 && *d.Order <= n {
			validOrder[*d.Order] = true
		}
	}

	// 按日期升序排序未声明 order 的文章（作为默认阅读顺序）。
	unordered := make([]domain.DocSummary, 0, n)
	ordered := make([]domain.DocSummary, 0, n)
	for _, d := range docs {
		if d.Order != nil && validOrder[*d.Order] {
			ordered = append(ordered, d)
		} else {
			unordered = append(unordered, d)
		}
	}
	sort.Slice(unordered, func(i, j int) bool {
		return unordered[i].Date.Before(unordered[j].Date)
	})

	result := make([]domain.DocSummary, n)
	filled := make([]bool, n)
	// 1. 放置显式 order 的文章
	for _, d := range ordered {
		idx := *d.Order - 1
		result[idx] = d
		filled[idx] = true
	}
	// 2. 用未声明 order（按日期升序）填充空位
	ui := 0
	for i := 0; i < n; i++ {
		if !filled[i] {
			result[i] = unordered[ui]
			ui++
		}
	}
	return result
}
