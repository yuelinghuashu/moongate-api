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
---

About content.
`)

	store, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	// 验证 Docs 数量（按 slug 索引）
	if len(store.DocsBySlug) != 2 {
		t.Errorf("DocsBySlug count = %d, want 2", len(store.DocsBySlug))
	}

	// 验证 slug 索引
	if _, ok := store.DocsBySlug["article1"]; !ok {
		t.Error("DocsBySlug should contain slug article1")
	}
	if _, ok := store.DocsBySlug["article2"]; !ok {
		t.Error("DocsBySlug should contain slug article2")
	}

	// 验证 About 数量（按 slug 索引）
	if len(store.AboutBySlug) != 1 {
		t.Errorf("AboutBySlug count = %d, want 1", len(store.AboutBySlug))
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
level: P3
series: ""
---

Content.
`)

	store, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	doc := store.DocsBySlug["empty-series"]
	if doc == nil {
		t.Fatal("Doc should exist for slug empty-series")
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

	if len(store.DocsBySlug) != 1 {
		t.Errorf("DocsBySlug count = %d, want 1 (invalid files should be skipped)", len(store.DocsBySlug))
	}
}

func TestLoadAll_RecursiveSubdirectories(t *testing.T) {
	dir := createTestContentDir(t)

	// 创建嵌套子目录模拟按 series 分组
	seriesDir := filepath.Join(dir, "docs", "narrative-engine")
	if err := os.MkdirAll(seriesDir, 0755); err != nil {
		t.Fatalf("Failed to create series dir: %v", err)
	}

	// 顶层文档
	writeTestMarkdown(t, filepath.Join(dir, "docs"), "standalone.md", `---
title: "Standalone"
description: "No series"
date: 2025-01-01 00:00:00
level: P1
---

Standalone content.
`)

	// 子目录文档
	writeTestMarkdown(t, seriesDir, "narrative-part-1.md", `---
title: "Narrative Part 1"
description: "First part"
date: 2025-01-02 00:00:00
level: P2
series: narrative-engine
---

Part 1 content.
`)

	writeTestMarkdown(t, seriesDir, "narrative-part-2.md", `---
title: "Narrative Part 2"
description: "Second part"
date: 2025-01-03 00:00:00
level: P2
series: narrative-engine
---

Part 2 content.
`)

	store, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	// 顶层 + 子目录共 3 个文档
	if len(store.DocsBySlug) != 3 {
		t.Errorf("DocsBySlug count = %d, want 3", len(store.DocsBySlug))
	}

	// 验证子目录文档的 slug（应为文件名，而非路径）
	if _, ok := store.DocsBySlug["narrative-part-1"]; !ok {
		t.Error("DocsBySlug should contain slug narrative-part-1")
	}
	if _, ok := store.DocsBySlug["standalone"]; !ok {
		t.Error("DocsBySlug should contain slug standalone")
	}

	// 验证 series 正确关联
	doc := store.DocsBySlug["narrative-part-1"]
	if doc == nil {
		t.Fatal("Doc should exist for slug narrative-part-1")
	}
	if doc.Series == nil || *doc.Series != "narrative-engine" {
		t.Errorf("Series = %v, want narrative-engine", doc.Series)
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
	if len(store.DocsBySlug) != 0 {
		t.Errorf("DocsBySlug count = %d, want 0 for non-existent directory", len(store.DocsBySlug))
	}
}

func TestLoadAll_EnglishTranslationFiles(t *testing.T) {
	dir := createTestContentDir(t)

	// 中文文章
	writeTestMarkdown(t, filepath.Join(dir, "docs"), "article.md", `---
title: "Article"
description: "中文摘要"
date: 2025-01-01 00:00:00
level: P2
series: go-tutorial
tags:
  - Go
---

中文内容
`)

	// 英文译文（.en.md），slug 应归一化为 article
	writeTestMarkdown(t, filepath.Join(dir, "docs"), "article.en.md", `---
title: "Article (EN)"
description: "English description"
date: 2025-01-01 00:00:00
level: P2
series: go-tutorial
tags:
  - Go
---

English content
`)

	store, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	// 中文照常进入 DocsBySlug
	if len(store.DocsBySlug) != 1 {
		t.Errorf("DocsBySlug count = %d, want 1", len(store.DocsBySlug))
	}

	// 英文译文进入 DocsEn，slug 不带 .en 后缀
	if len(store.DocsEn) != 1 {
		t.Fatalf("DocsEn count = %d, want 1", len(store.DocsEn))
	}
	en := store.DocsEn["article"]
	if en == nil {
		t.Fatal("DocsEn should contain slug article")
	}
	if en.Title != "Article (EN)" {
		t.Errorf("EN title = %q, want %q", en.Title, "Article (EN)")
	}
	if _, ok := store.DocsBySlug["article.en"]; ok {
		t.Error("DocsBySlug should NOT contain slug article.en")
	}
}

func TestLoadAll_EnglishTranslationMissingCounterpart(t *testing.T) {
	dir := createTestContentDir(t)

	// 只有英文译文，没有中文配对
	writeTestMarkdown(t, filepath.Join(dir, "docs"), "en-only.en.md", `---
title: "EN Only"
description: "desc"
date: 2025-01-01 00:00:00
level: P1
tags:
  - English
---

English only
`)

	store, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	if len(store.DocsBySlug) != 0 {
		t.Errorf("DocsBySlug count = %d, want 0", len(store.DocsBySlug))
	}
	if len(store.DocsEn) != 1 {
		t.Errorf("DocsEn count = %d, want 1", len(store.DocsEn))
	}
	if _, ok := store.DocsEn["en-only"]; !ok {
		t.Error("DocsEn should contain slug en-only")
	}
}
