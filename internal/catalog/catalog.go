// Package catalog 是内容查询层：在内存中的已解析文档之上提供
// "语言回退 + 排序 + 过滤 + 分页 + 系列分组"这一整套只读查询能力。
//
// 分层约定（自下而上）：
//
//	loader   → 读文件、解析 frontmatter、Markdown 转 HTML
//	domain   → 解析模型（Doc/Frontmatter）与响应 DTO（DocDetail/DocSummary）
//	catalog  → 查询与视图组装（本包），只读、并发安全
//	api      → 解析 HTTP 参数、调用 catalog、序列化响应
//
// 本包不做 IO、不依赖 Gin，因此可以直接用单元测试覆盖回退与排序规则。
package catalog

import (
	"sort"

	"moongate-api/internal/domain"
)

// 内容语言常量。
const (
	LangZH = "zh"
	LangEN = "en"
)

// Catalog 持有内存中的全部文档，并按语言分桶。
//
// zh 以 slug 为 key 存放中文/默认内容，en 存放英文译文（文件名形如 <slug>.en.md）。
// 字段为私有，外部只能通过 List / Series / Detail 读取组装好的视图，
// 因此不存在"调用方拿到 *Doc 自行改写"的路径。
type Catalog struct {
	zh map[string]*domain.Doc
	en map[string]*domain.Doc
}

// New 基于 loader 的解析结果创建 catalog。
func New(zh, en map[string]*domain.Doc) *Catalog {
	return &Catalog{zh: zh, en: en}
}

// entry 是"一篇文档 + 它在本次请求语言下的落点"。
//
// 关键设计：语言回退（要中文还是英文、是否发生了回退）在排序之前一次性判定，
// 之后排序、过滤、分页、组装响应全部复用同一份 entry，
// 避免同一 slug 在过滤 / 摘要 / 分页三处各解析一次。
type entry struct {
	doc            *domain.Doc // 命中的文档（中文或英文）
	lang           string      // 实际语言
	isFallback     bool        // 是否为请求语言的回退结果
	hasTranslation bool        // 该 slug 是否存在英文译文
}

// resolve 判定某个 slug 在请求语言下的落点；未命中返回 ok=false。
//
// 回退规则（与历史行为一致）：
//   - 请求英文：优先英文译文，缺失时回退中文；
//   - 请求其他语言（含缺省）：优先中文，缺失时兜底英文（防 404）。
func (c *Catalog) resolve(slug, lang string) (entry, bool) {
	if lang == LangEN {
		if en, ok := c.en[slug]; ok {
			return entry{doc: en, lang: LangEN, hasTranslation: true}, true
		}
		if zh, ok := c.zh[slug]; ok {
			return entry{doc: zh, lang: LangZH, isFallback: true}, true
		}
		return entry{}, false
	}

	if zh, ok := c.zh[slug]; ok {
		_, hasTranslation := c.en[slug]
		return entry{doc: zh, lang: LangZH, hasTranslation: hasTranslation}, true
	}
	if en, ok := c.en[slug]; ok {
		return entry{doc: en, lang: LangEN, isFallback: true, hasTranslation: true}, true
	}
	return entry{}, false
}

// candidates 按请求语言解析列表语料，并按列表页规则排序。
//
// 语料范围与历史行为一致：以中文/默认文档为准（slug 取自 c.zh），
// 每篇再按请求语言选正文（英文请求取译文，缺失时回退中文）。
// 仅英文存在的文档不单独出现在列表里，只能通过详情接口兜底访问。
//
// 排序：日期降序（最新在前）。日期并列时使用确定性次级规则，否则受 map 迭代
// 随机序影响，每次请求顺序不同，会引发 SSR 与客户端水合不一致：
//   - 同系列：列表页遵循"最新在前"惯例 → order 降序（大的在上；无 order 视为 0，排在最后）；
//   - 不同系列 / 无系列：按 slug 升序。
func (c *Catalog) candidates(lang string) []entry {
	list := make([]entry, 0, len(c.zh))
	for slug := range c.zh {
		// 语料来自 c.zh，resolve 对同样的 slug 必然命中，无需再判 ok
		e, _ := c.resolve(slug, lang)
		list = append(list, e)
	}

	sort.Slice(list, func(i, j int) bool {
		di, dj := list[i].doc, list[j].doc
		if !di.Date.Equal(dj.Date) {
			return di.Date.After(dj.Date)
		}
		if di.Series != nil && dj.Series != nil && *di.Series == *dj.Series {
			oi, oj := 0, 0
			if di.Order != nil {
				oi = *di.Order
			}
			if dj.Order != nil {
				oj = *dj.Order
			}
			if oi != oj {
				return oi > oj
			}
		}
		return di.Slug < dj.Slug
	})

	return list
}
