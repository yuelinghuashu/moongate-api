package loader

import (
	"moongate-api/internal/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// helper: 创建临时测试文件
func createTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test-article.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	return path
}

func TestParseMarkdown_ValidDoc(t *testing.T) {
	content := `---
title: "Go 测试"
description: "测试解析功能的文章"
date: 2025-01-15 10:00:00
permalink: /docs/go-testing
level: P2
tags:
  - Go
  - Testing
---

# Hello World

This is a **test** body.
`

	path := createTempFile(t, content)
	doc, err := ParseMarkdown[domain.Doc](path)
	if err != nil {
		t.Fatalf("ParseMarkdown() error = %v", err)
	}

	if doc.Title != "Go 测试" {
		t.Errorf("Title = %q, want %q", doc.Title, "Go 测试")
	}
	if doc.Description != "测试解析功能的文章" {
		t.Errorf("Description = %q, want %q", doc.Description, "测试解析功能的文章")
	}
	if doc.Permalink != "/docs/go-testing" {
		t.Errorf("Permalink = %q, want %q", doc.Permalink, "/docs/go-testing")
	}
	if doc.Level != domain.LevelP2 {
		t.Errorf("Level = %q, want %q", doc.Level, domain.LevelP2)
	}
	if len(doc.Tags) != 2 || doc.Tags[0] != "Go" || doc.Tags[1] != "Testing" {
		t.Errorf("Tags = %v, want [Go Testing]", doc.Tags)
	}

	// 验证日期解析
	expectedDate := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	if !doc.Date.Equal(expectedDate) {
		t.Errorf("Date = %v, want %v", doc.Date, expectedDate)
	}
}

func TestParseMarkdown_SlugFromFilename(t *testing.T) {
	content := `---
title: "Test"
description: "desc"
date: 2025-01-01 00:00:00
permalink: /docs/test
---

Body text here.
`

	path := createTempFile(t, content)
	doc, err := ParseMarkdown[domain.Doc](path)
	if err != nil {
		t.Fatalf("ParseMarkdown() error = %v", err)
	}

	if doc.Slug != "test-article" {
		t.Errorf("Slug = %q, want %q", doc.Slug, "test-article")
	}
}

func TestParseMarkdown_HTMLContent(t *testing.T) {
	content := `---
title: "Test HTML"
description: "desc"
date: 2025-01-01 00:00:00
permalink: /docs/test-html
---

# Heading One

Paragraph with **bold** text.
`

	path := createTempFile(t, content)
	doc, err := ParseMarkdown[domain.Doc](path)
	if err != nil {
		t.Fatalf("ParseMarkdown() error = %v", err)
	}

	// 验证 Markdown 转 HTML
	if doc.Content == "" {
		t.Error("Content should not be empty after Markdown conversion")
	}
	if !strings.Contains(doc.Content, "<h1") {
		t.Errorf("Content should contain heading, got: %s", doc.Content)
	}
	if !strings.Contains(doc.Content, "<strong>") {
		t.Errorf("Content should contain <strong> tag, got: %s", doc.Content)
	}
}

func TestParseMarkdown_InvalidFrontmatter(t *testing.T) {
	// 没有 frontmatter 分隔符
	content := `just plain text without frontmatter`
	path := createTempFile(t, content)

	_, err := ParseMarkdown[domain.Doc](path)
	if err == nil {
		t.Error("ParseMarkdown() should error for invalid frontmatter format")
	}
}

func TestParseMarkdown_InvalidYAML(t *testing.T) {
	content := `---
title: [unclosed
---
body here
`
	path := createTempFile(t, content)

	_, err := ParseMarkdown[domain.Doc](path)
	if err == nil {
		t.Error("ParseMarkdown() should error for invalid YAML")
	}
}

func TestParseMarkdown_FileNotFound(t *testing.T) {
	_, err := ParseMarkdown[domain.Doc]("/nonexistent/file.md")
	if err == nil {
		t.Error("ParseMarkdown() should error when file does not exist")
	}
}

func TestParseMarkdown_EmptySeries(t *testing.T) {
	content := `---
title: "Series Test"
description: "desc"
date: 2025-01-01 00:00:00
permalink: /docs/series-test
series: 
---

Body text.
`

	path := createTempFile(t, content)
	doc, err := ParseMarkdown[domain.Doc](path)
	if err != nil {
		t.Fatalf("ParseMarkdown() error = %v", err)
	}

	// 解析后 series 可能是空字符串指针
	if doc.Series != nil && *doc.Series == "" {
		t.Log("Series is empty string pointer (handled in loadDocs)")
	}
}

func TestParseMarkdown_About(t *testing.T) {
	content := `---
title: "About Me"
description: "My bio"
date: 2025-01-01 00:00:00
permalink: /about/me
---

I am a developer.
`

	path := createTempFile(t, content)
	about, err := ParseMarkdown[domain.About](path)
	if err != nil {
		t.Fatalf("ParseMarkdown() error = %v", err)
	}

	if about.Title != "About Me" {
		t.Errorf("Title = %q, want %q", about.Title, "About Me")
	}
	if about.Slug != "test-article" {
		t.Errorf("Slug = %q, want %q", about.Slug, "test-article")
	}
	if about.Content == "" {
		t.Error("Content should not be empty")
	}
}

