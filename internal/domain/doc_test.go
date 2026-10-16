package domain

import (
	"testing"
	"time"
)

func TestContainsAllTags(t *testing.T) {
	doc := &Doc{
		Frontmatter: Frontmatter{Title: "test doc", Tags: []string{"Go", "Vue", "Docker"}},
	}

	emptyDoc := &Doc{Frontmatter: Frontmatter{Title: "empty doc"}}

	tests := []struct {
		name       string
		targetTags []string
		want       bool
	}{
		{
			name:       "空标签列表返回 true",
			targetTags: []string{},
			want:       true,
		},
		{
			name:       "单个匹配标签",
			targetTags: []string{"Go"},
			want:       true,
		},
		{
			name:       "大小写不敏感匹配",
			targetTags: []string{"go", "vue"},
			want:       true,
		},
		{
			name:       "多个匹配标签",
			targetTags: []string{"Go", "Vue", "Docker"},
			want:       true,
		},
		{
			name:       "目标标签超过文档标签数量",
			targetTags: []string{"Go", "Vue", "Docker", "React"},
			want:       false,
		},
		{
			name:       "不存在的标签",
			targetTags: []string{"React"},
			want:       false,
		},
		{
			name:       "部分匹配（非全部）",
			targetTags: []string{"Go", "React"},
			want:       false,
		},
		{
			name:       "空文档标签",
			targetTags: []string{"Go"},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDoc := doc
			if tt.name == "空文档标签" {
				testDoc = emptyDoc
			}
			got := testDoc.ContainsAllTags(tt.targetTags)
			if got != tt.want {
				t.Errorf("ContainsAllTags(%v) = %v, want %v", tt.targetTags, got, tt.want)
			}
		})
	}
}

// TestDocEmbeddedFrontmatter 验证内嵌 Frontmatter 的字段提升：
// frontmatter 解析结果（Title/Tags/...）可通过 Doc 直接访问，
// 同时 Doc 自己持有 Slug / Content（语言字段只存在于响应 DTO 上）。
func TestDocEmbeddedFrontmatter(t *testing.T) {
	doc := &Doc{
		Frontmatter: Frontmatter{Title: "内嵌标题", Tags: []string{"Go"}},
		Slug:        "my-slug",
		Content:     "<p>hello</p>",
	}

	if doc.Title != "内嵌标题" {
		t.Errorf("提升字段 Title = %q, want %q", doc.Title, "内嵌标题")
	}
	if doc.Frontmatter.Title != doc.Title {
		t.Errorf("Frontmatter.Title = %q, 与提升字段不一致", doc.Frontmatter.Title)
	}
	if doc.Slug != "my-slug" {
		t.Errorf("Slug = %q, want %q", doc.Slug, "my-slug")
	}
	if doc.Content != "<p>hello</p>" {
		t.Errorf("Content = %q, want %q", doc.Content, "<p>hello</p>")
	}
}

func TestSearchModeIsValid(t *testing.T) {
	tests := []struct {
		mode SearchMode
		want bool
	}{
		{SearchModeAll, true},
		{SearchModeTitle, true},
		{SearchModeDescription, true},
		{SearchMode("invalid"), false},
		{SearchMode(""), false},
		{SearchMode("ALL"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDocSortingByDate(t *testing.T) {
	docs := []Doc{
		{Frontmatter: Frontmatter{Title: "older", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}},
		{Frontmatter: Frontmatter{Title: "newer", Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}},
		{Frontmatter: Frontmatter{Title: "middle", Date: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)}},
	}

	// 验证 Date.After 方法的行为（用于排序逻辑）
	if !docs[1].Date.After(docs[0].Date) {
		t.Error("newer date should be After older date")
	}
	if !docs[1].Date.After(docs[2].Date) {
		t.Error("newer date should be After middle date")
	}
	if !docs[2].Date.After(docs[0].Date) {
		t.Error("middle date should be After older date")
	}
	if docs[0].Date.After(docs[1].Date) {
		t.Error("older date should NOT be After newer date")
	}
}
