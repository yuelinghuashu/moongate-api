package loader

import (
	"os"
	"path/filepath"
	"testing"
)

// createTestContentDir 创建包含 docs 和 about 子目录的测试内容目录
func createTestContentDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	docsDir := filepath.Join(dir, "docs")
	aboutDir := filepath.Join(dir, "about")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs dir: %v", err)
	}
	if err := os.MkdirAll(aboutDir, 0755); err != nil {
		t.Fatalf("Failed to create about dir: %v", err)
	}

	return dir
}

// writeTestMarkdown 写入测试 Markdown 文件
func writeTestMarkdown(t *testing.T, dir, filename, content string) {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write %s: %v", filename, err)
	}
}

func TestLoadAll_ValidContent(t *testing.T) {
	dir := createTestContentDir(t)

	// 写入 docs
	writeTestMarkdown(t, filepath.Join(dir, "docs"), "article1.md", `---
title: "Article One"
description: "First article"
date: 2025-01-01 00:00:00
permalink: /docs/article-one
level: P1
tags:
  - Go
---

Content one.
`)

	writeTestMarkdown(t, filepath.Join(dir, "docs"), "article2.md", `---
title: "Article Two"
description: "Second article with series"
date: 2025-01-02 00:00:00
permalink: /docs/article-two
level: P2
series: "go-tutorial"
tags:
  - Go
  - Tutorial
---

Content two.
`)

	// 写入 about
	writeTestMarkdown(t, filepath.Join(dir, "about"), "me.md", `---
title: "About Me"
description: "Bio"
date: 2025-01-01 00:00:00
permalink: /about/me
---

About content.
`)

	store, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	// 验证 Docs 数量
	if len(store.Docs) != 2 {
		t.Errorf("Docs count = %d, want 2", len(store.Docs))
	}
	if len(store.DocsBySlug) != 2 {
		t.Errorf("DocsBySlug count = %d, want 2", len(store.DocsBySlug))
	}

	// 验证 permalink 索引
	if _, ok := store.Docs["/docs/article-one"]; !ok {
		t.Error("Docs should contain permalink /docs/article-one")
	}
	if _, ok := store.Docs["/docs/article-two"]; !ok {
		t.Error("Docs should contain permalink /docs/article-two")
	}

	// 验证 slug 索引
	if _, ok := store.DocsBySlug["article1"]; !ok {
		t.Error("DocsBySlug should contain slug article1")
	}
	if _, ok := store.DocsBySlug["article2"]; !ok {
		t.Error("DocsBySlug should contain slug article2")
	}

	// 验证 About 数量
	if len(store.About) != 1 {
		t.Errorf("About count = %d, want 1", len(store.About))
	}
	if len(store.AboutBySlug) != 1 {
		t.Errorf("AboutBySlug count = %d, want 1", len(store.AboutBySlug))
	}
	if _, ok := store.About["/about/me"]; !ok {
		t.Error("About should contain permalink /about/me")
	}
	if _, ok := store.AboutBySlug["me"]; !ok {
		t.Error("AboutBySlug should contain slug me")
	}
}

func TestLoadAll_EmptySeriesConvertedToNil(t *testing.T) {
	dir := createTestContentDir(t)

	writeTestMarkdown(t, filepath.Join(dir, "docs"), "empty-series.md", `---
title: "Empty Series"
description: "desc"
date: 2025-01-01 00:00:00
permalink: /docs/empty-series
level: P3
series: ""
---

Content.
`)

	store, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	doc := store.Docs["/docs/empty-series"]
	if doc == nil {
		t.Fatal("Doc should exist for /docs/empty-series")
	}

	// 空字符串 series 应被转换为 nil
	if doc.Series != nil {
		t.Errorf("Series should be nil after loading, got %q", *doc.Series)
	}
}

func TestLoadAll_SkipsInvalidFiles(t *testing.T) {
	dir := createTestContentDir(t)

	// 有效文件
	writeTestMarkdown(t, filepath.Join(dir, "docs"), "valid.md", `---
title: "Valid"
description: "desc"
date: 2025-01-01 00:00:00
permalink: /docs/valid
level: P1
---

Content.
`)

	// 无效文件 - 没有 frontmatter
	writeTestMarkdown(t, filepath.Join(dir, "docs"), "invalid.md", `just plain text`)

	// 非 .md 文件应该被忽略
	writeTestMarkdown(t, filepath.Join(dir, "docs"), "notes.txt", `not a markdown file`)

	store, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	if len(store.Docs) != 1 {
		t.Errorf("Docs count = %d, want 1 (invalid files should be skipped)", len(store.Docs))
	}
}

func TestLoadAll_NonExistentDir(t *testing.T) {
	// filepath.Glob 对不存在的目录不会返回错误，而是返回空列表。
	// LoadAll 对空目录的处理是：正常返回空 store（无错误）。
	store, err := LoadAll("/nonexistent/directory")
	if err != nil {
		t.Logf("LoadAll() returned error (acceptable behavior): %v", err)
		return
	}

	// 如果没返回错误，store 应该为空但非 nil
	if store == nil {
		t.Error("LoadAll() should return non-nil store even for non-existent directory")
	}
	if len(store.Docs) != 0 {
		t.Errorf("Docs count = %d, want 0 for non-existent directory", len(store.Docs))
	}
}
