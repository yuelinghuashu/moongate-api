package loader

import (
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

func TestParseDoc_ValidDoc(t *testing.T) {
	content := `---
title: "Go 测试"
description: "测试解析功能的文章"
date: 2025-01-15 10:00:00
tags:
  - Go
  - Testing
---

# Hello World

This is a **test** body.
`

	path := createTempFile(t, content)
	fm, slug, _, err := parseDoc(path)
	if err != nil {
		t.Fatalf("parseDoc() error = %v", err)
	}

	if slug != "test-article" {
		t.Errorf("Slug = %q, want %q", slug, "test-article")
	}
	if fm.Title != "Go 测试" {
		t.Errorf("Title = %q, want %q", fm.Title, "Go 测试")
	}
	if fm.Description != "测试解析功能的文章" {
		t.Errorf("Description = %q, want %q", fm.Description, "测试解析功能的文章")
	}
	if len(fm.Tags) != 2 || fm.Tags[0] != "Go" || fm.Tags[1] != "Testing" {
		t.Errorf("Tags = %v, want [Go Testing]", fm.Tags)
	}

	// 验证日期解析
	expectedDate := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	if !fm.Date.Equal(expectedDate) {
		t.Errorf("Date = %v, want %v", fm.Date, expectedDate)
	}
}

func TestParseDoc_SlugFromFilename(t *testing.T) {
	content := `---
title: "Test"
description: "desc"
date: 2025-01-01 00:00:00
---

Body text here.
`

	path := createTempFile(t, content)
	_, slug, _, err := parseDoc(path)
	if err != nil {
		t.Fatalf("parseDoc() error = %v", err)
	}

	if slug != "test-article" {
		t.Errorf("Slug = %q, want %q", slug, "test-article")
	}
}

func TestParseDoc_HTMLContent(t *testing.T) {
	content := `---
title: "Test HTML"
description: "desc"
date: 2025-01-01 00:00:00
---

# Heading One

Paragraph with **bold** text.
`

	path := createTempFile(t, content)
	_, _, content, err := parseDoc(path)
	if err != nil {
		t.Fatalf("parseDoc() error = %v", err)
	}

	// 验证 Markdown 转 HTML
	if content == "" {
		t.Error("Content should not be empty after Markdown conversion")
	}
	if !strings.Contains(content, "<h1") {
		t.Errorf("Content should contain heading, got: %s", content)
	}
	if !strings.Contains(content, "<strong>") {
		t.Errorf("Content should contain <strong> tag, got: %s", content)
	}
}

func TestParseDoc_InvalidFrontmatter(t *testing.T) {
	// 没有 frontmatter 分隔符
	content := `just plain text without frontmatter`
	path := createTempFile(t, content)

	_, _, _, err := parseDoc(path)
	if err == nil {
		t.Error("parseDoc() should error for invalid frontmatter format")
	}
}

func TestParseDoc_InvalidYAML(t *testing.T) {
	content := `---
title: [unclosed
---
body here
`
	path := createTempFile(t, content)

	_, _, _, err := parseDoc(path)
	if err == nil {
		t.Error("parseDoc() should error for invalid YAML")
	}
}

func TestParseDoc_FileNotFound(t *testing.T) {
	_, _, _, err := parseDoc("/nonexistent/file.md")
	if err == nil {
		t.Error("parseDoc() should error when file does not exist")
	}
}

func TestParseDoc_EmptySeries(t *testing.T) {
	content := `---
title: "Series Test"
description: "desc"
date: 2025-01-01 00:00:00
series: 
---

Body text.
`

	path := createTempFile(t, content)
	fm, _, _, err := parseDoc(path)
	if err != nil {
		t.Fatalf("parseDoc() error = %v", err)
	}

	// 解析后 series 可能是空字符串指针
	if fm.Series != nil && *fm.Series == "" {
		t.Log("Series is empty string pointer (handled in loadDocs)")
	}
}
