package domain

import "time"

// 本文件只描述"对外响应长什么样"（HTTP JSON 契约）。
//
// 与 Doc 的区别：
//   - Doc 是解析产物 + 内存存储实体，不含请求期状态，可被并发请求共享；
//   - DocDetail / DocSummary 是每次请求新构造的值对象（不是共享指针的浅拷贝），
//     语言回退结果显式写在字段里，因此不存在"回填共享结构体"的隐患。
//
// 两个视图各自显式列出字段（不内嵌 Frontmatter），好处有三：
//  1. 字段顺序与历史 JSON 契约逐字节一致；
//  2. 任一视图将来增删字段都不会意外影响另一个；
//  3. go.mod 的语言版本为 1.26，内嵌结构体的提升字段无法用于复合字面量，
//     显式字段让构造代码更直白。

// DocDetail 单篇文档响应（GET /api/docs/:slug）。
type DocDetail struct {
	Title          string    `json:"title"`          // 文章标题
	Description    string    `json:"description"`    // 简短摘要
	Date           time.Time `json:"date"`           // 发布日期
	Slug           string    `json:"slug"`           // URL 友好标识符
	Series         *string   `json:"series"`         // 所属系列，nil 表示无系列
	Order          *int      `json:"order"`          // 系列内阅读顺序（可选）
	Tags           []string  `json:"tags"`           // 标签列表
	Content        string    `json:"content"`        // 正文 HTML
	Lang           string    `json:"lang"`           // 实际返回内容的语言：zh | en
	IsFallback     bool      `json:"isFallback"`     // 请求语言无译文时回退到了另一语言
	HasTranslation bool      `json:"hasTranslation"` // 该 slug 是否存在英文译文
}

// DocSummary 列表 / 系列分组用的摘要响应（不含正文）。
type DocSummary struct {
	Title          string    `json:"title"`
	Description    string    `json:"description"`
	Date           time.Time `json:"date"`
	Slug           string    `json:"slug"`
	Series         *string   `json:"series"`
	Order          *int      `json:"order"`
	Tags           []string  `json:"tags"`
	Lang           string    `json:"lang"`
	IsFallback     bool      `json:"isFallback"`
	HasTranslation bool      `json:"hasTranslation"`
}

// DocsPage 列表接口的响应信封。
//
// Data 按请求参数不同可承载 []DocSummary（默认）或 []DocDetail（?content=true）。
// 键顺序与历史输出一致（旧实现使用 gin.H，由 encoding/json 按字典序输出）。
type DocsPage struct {
	Data       any `json:"data"`
	Limit      int `json:"limit"`
	Page       int `json:"page"`
	Total      int `json:"total"`
	TotalPages int `json:"totalPages"`
}

// SeriesGroup 系列分组响应（GET /api/docs?group=series，裸数组）。
type SeriesGroup struct {
	Slug string       `json:"slug"`
	Docs []DocSummary `json:"docs"`
}
