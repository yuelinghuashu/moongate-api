package loader

import (
	"fmt"
	"io/fs"
	"moongate-api/internal/domain"
	"path/filepath"
	"strings"
)

// Store 内存数据存储，存放所有解析后的内容。
// DocsBySlug 和 AboutBySlug 以 slug（文件名）为 key 存储。
// DocsEn 以 slug 为 key 存储英文译文（文件名形如 <slug>.en.md）。
type Store struct {
	DocsBySlug  map[string]*domain.Doc   // key = slug（中文/默认）
	DocsEn      map[string]*domain.Doc   // key = slug（英文译文）
	AboutBySlug map[string]*domain.About // key = slug
}

// LoadAll 扫描 content 目录，加载所有 Markdown 文件。
// 遍历 content/docs/ 和 content/about/ 两个子目录，
// 分别加载技术文章和独立页面。
func LoadAll(contentDir string) (*Store, error) {
	store := &Store{
		DocsBySlug:  make(map[string]*domain.Doc),
		DocsEn:      make(map[string]*domain.Doc),
		AboutBySlug: make(map[string]*domain.About),
	}

	docsPath := filepath.Join(contentDir, "docs")
	if err := loadDocs(docsPath, store); err != nil {
		return nil, fmt.Errorf("加载 docs 失败: %w", err)
	}

	aboutPath := filepath.Join(contentDir, "about")
	if err := loadAbout(aboutPath, store); err != nil {
		return nil, fmt.Errorf("加载 about 失败: %w", err)
	}

	// 健壮性提示：英文译文没有对应的中文文章
	for slug := range store.DocsEn {
		if _, ok := store.DocsBySlug[slug]; !ok {
			fmt.Printf("⚠️ 英文译文 %s.en.md 没有对应的中文文章\n", slug)
		}
	}

	return store, nil
}

// loadDocs 递归加载 content/docs/ 目录下的所有技术文章。
// 支持按 series 分组的子目录结构（如 content/docs/narrative-engine/xxx.md）。
// 支持英文译文文件（文件名以 .en.md 结尾），其 slug 去掉 .en 后缀后与中文文章配对。
// 逐个解析 Markdown 文件，若 slug 为空则从文件名生成，
// 解析失败时跳过该文件并继续处理其余文件。
func loadDocs(dir string, store *Store) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// 跳过目录
		if d.IsDir() {
			return nil
		}
		// 只处理 .md 文件
		if filepath.Ext(path) != ".md" {
			return nil
		}

		doc, err := ParseMarkdown[domain.Doc](path)
		if err != nil {
			fmt.Printf("⚠️ 跳过 %s: %v\n", path, err)
			return nil
		}

		// 后处理：空字符串的 series 视为 nil
		// YAML 中 series: （值为空）会被解析为 *string 指向 "" 而非 nil
		if doc.Series != nil && *doc.Series == "" {
			doc.Series = nil
		}

		// 英文译文：文件名形如 <slug>.en.md，ParseMarkdown 已将 slug 归一化为去掉 .en 后缀
		if strings.HasSuffix(path, ".en.md") {
			store.DocsEn[doc.Slug] = &doc // 存储 slug 索引
			return nil
		}

		store.DocsBySlug[doc.Slug] = &doc // 存储 slug 索引
		return nil
	})
}

// loadAbout 加载 content/about/ 目录下的所有关于页面。
// 逐个解析 Markdown 文件，解析失败时跳过该文件并继续处理其余文件。
func loadAbout(dir string, store *Store) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return err
	}

	for _, file := range files {
		about, err := ParseMarkdown[domain.About](file)
		if err != nil {
			fmt.Printf("⚠️ 跳过 %s: %v\n", file, err)
			continue
		}

		store.AboutBySlug[about.Slug] = &about // 存储 slug 索引
	}

	return nil
}
