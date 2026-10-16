package domain

import (
	"strings"
	"time"
)

// Frontmatter 是 Markdown 文件 frontmatter 的解析结果。
//
// 它只回答"文件里写了什么"，不关心"如何返回给客户端"：
// 语言回退、是否命中译文等请求期状态属于响应视图（见 dto.go），
// 解析产物因此可以在多个并发请求之间安全共享。
type Frontmatter struct {
	Title       string    `yaml:"title" json:"title"`             // 文章标题
	Description string    `yaml:"description" json:"description"` // 简短摘要
	Date        time.Time `yaml:"date" json:"date"`               // 发布日期 (YYYY-MM-DD HH:MM:SS)
	Series      *string   `yaml:"series" json:"series"`           // 所属系列，nil 表示无系列
	Order       *int      `yaml:"order,omitempty" json:"order"`   // 系列内阅读顺序（可选），nil 表示未指定
	Tags        []string  `yaml:"tags" json:"tags"`               // 标签列表，用于分类
}

// Doc 一篇已解析的文档：embedding 让 frontmatter 字段直接可用
// （doc.Title、domain.Doc{Title: ...} 的写法保持不变），
// 另外补上由文件名推导的 Slug 与 Markdown 转换后的正文 HTML。
type Doc struct {
	Frontmatter
	Slug    string `json:"slug"`    // URL 友好标识符（由文件名生成）
	Content string `json:"content"` // 正文 HTML
}

// ContainsAllTags 检查文档是否包含所有目标标签（不区分大小写）
func (d *Doc) ContainsAllTags(targetTags []string) bool {
	// 如果目标标签为空，直接返回 true
	if len(targetTags) == 0 {
		return true
	}

	// 如果目标标签数量大于文档标签数量，直接返回 false
	if len(targetTags) > len(d.Tags) {
		return false
	}

	// 创建一个标签集合，用于快速查找
	tagSet := make(map[string]bool, len(d.Tags))

	// 遍历文档标签，将所有标签转换为小写并添加到集合中
	for _, tag := range d.Tags {
		tagSet[strings.ToLower(tag)] = true
	}

	// 遍历目标标签，检查是否在文档标签集合中
	for _, tag := range targetTags {
		if !tagSet[strings.ToLower(tag)] {
			return false
		}
	}

	// 如果所有目标标签都在文档标签集合中，返回 true
	return true
}
