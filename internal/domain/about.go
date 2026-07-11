package domain

import "time"

// About 文档实体（包含所有字段）
type About struct {
	Title       string    `yaml:"title" json:"title"`             // 文章标题
	Description string    `yaml:"description" json:"description"` // 简短摘要
	Date        time.Time `yaml:"date" json:"date"`               // 发布日期 (YYYY-MM-DD HH:MM:SS)
	Permalink   string    `yaml:"permalink" json:"permalink"`     // 唯一标识符，用于构建链接
	Slug        string    `yaml:"slug" json:"slug"`               // URL 友好标识符
	Content     string    `json:"content"`                        // 正文 HTML（来自 Markdown 转换）
}

func (d *About) SetSlug(slug string) {
	d.Slug = slug
}

func (d *About) SetContent(content string) {
	d.Content = content
}

// AboutSummary 文档摘要（用于列表页）
type AboutSummary struct {
	Title       string    `yaml:"title" json:"title"`
	Description string    `yaml:"description" json:"description"`
	Date        time.Time `yaml:"date" json:"date"`
	Permalink   string    `yaml:"permalink" json:"permalink"`
	Slug        string    `yaml:"slug" json:"slug"`
}
