package domain

import (
	"strings"
	"time"
)

// Doc 文档实体（包含所有字段）
type Doc struct {
	Title       string    `yaml:"title" json:"title"`             // 文章标题
	Description string    `yaml:"description" json:"description"` // 简短摘要
	Date        time.Time `yaml:"date" json:"date"`           // 发布日期 (YYYY-MM-DD HH:MM:SS)
	Slug        string    `yaml:"slug,omitempty" json:"slug"` // URL 友好标识符
	Series      *string   `yaml:"series" json:"series"`           // 所属系列，nil 表示无系列
	Order       *int      `yaml:"order,omitempty" json:"order"`   // 系列内阅读顺序（可选），nil 表示未指定
	Tags        []string  `yaml:"tags" json:"tags"`               // 标签列表，用于分类
	Content     string    `json:"content"`                        // 正文 HTML（来自 Markdown 转换）

	// 以下字段为 API 响应时按请求语言填充（不参与 YAML frontmatter 解析）
	Lang           string `json:"lang"`           // 实际返回内容的语言：zh | en
	IsFallback     bool   `json:"isFallback"`     // 请求语言无译文时回退到中文（或反向兜底）
	HasTranslation bool   `json:"hasTranslation"` // 该 slug 是否存在英文译文
}

func (d *Doc) SetSlug(slug string) {
	d.Slug = slug
}

func (d *Doc) SetContent(content string) {
	d.Content = content
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

// DocSummary 文档摘要（用于列表页）
type DocSummary struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Date        time.Time `json:"date"`
	Slug        string    `json:"slug"`
	Series      *string   `json:"series"`
	Order       *int      `json:"order"`   // 系列内阅读顺序（可选），nil 表示未指定
	Tags        []string  `json:"tags"`
	Lang        string    `json:"lang"`           // 实际返回内容的语言：zh | en
	IsFallback  bool      `json:"isFallback"`     // 请求语言无译文时回退到中文
	HasTranslation bool   `json:"hasTranslation"` // 该 slug 是否存在英文译文
}

// SeriesGroup 系列分组（用于 API 响应）
type SeriesGroup struct {
	Slug string       `json:"slug"`
	Docs []DocSummary `json:"docs"`
}
