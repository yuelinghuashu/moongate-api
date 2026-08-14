package domain

import (
	"testing"
	"time"
)

func TestContainsAllTags(t *testing.T) {
	doc := &Doc{
		Title: "test doc",
		Tags:  []string{"Go", "Vue", "Docker"},
	}

	emptyDoc := &Doc{Title: "empty doc"}

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

func TestDocSetSlugAndContent(t *testing.T) {
	doc := &Doc{}
	doc.SetSlug("my-slug")
	doc.SetContent("<p>hello</p>")

	if doc.Slug != "my-slug" {
		t.Errorf("SetSlug() Slug = %q, want %q", doc.Slug, "my-slug")
	}
	if doc.Content != "<p>hello</p>" {
		t.Errorf("SetContent() Content = %q, want %q", doc.Content, "<p>hello</p>")
	}
}

func TestAboutSetSlugAndContent(t *testing.T) {
	about := &About{}
	about.SetSlug("about-slug")
	about.SetContent("<p>about content</p>")

	if about.Slug != "about-slug" {
		t.Errorf("SetSlug() Slug = %q, want %q", about.Slug, "about-slug")
	}
	if about.Content != "<p>about content</p>" {
		t.Errorf("SetContent() Content = %q, want %q", about.Content, "<p>about content</p>")
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
		{Title: "older", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Title: "newer", Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Title: "middle", Date: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)},
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