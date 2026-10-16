package loader

import (
	"fmt"
	"moongate-api/internal/domain"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// parseDoc 解析一个 Markdown 文件，返回 frontmatter、由文件名推导的 slug 与正文 HTML。
//
// 返回三个值而不是"直接返回填好的 Doc"，是为了让"文件叫什么"（slug）与
// "文件里写了什么"（frontmatter）由调用方显式组装，避免解析函数反过来改写领域对象。
func parseDoc(filePath string) (domain.Frontmatter, string, string, error) {
	var fm domain.Frontmatter

	// 1. 读取文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fm, "", "", fmt.Errorf("读取文件失败: %w", err)
	}

	// 2. 分割 Frontmatter
	parts := strings.SplitN(string(data), "---", 3)
	if len(parts) != 3 {
		return fm, "", "", fmt.Errorf("无效的 frontmatter 格式: %s", filePath)
	}
	frontmatter := parts[1]
	body := parts[2]

	// 3. 解析 YAML 到领域结构体
	if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
		return fm, "", "", fmt.Errorf("解析 frontmatter 失败: %w", err)
	}

	// 4. 提取文件名作为 Slug（去掉扩展名）
	// 英文译文文件名形如 <slug>.en.md，去掉 .en 后缀以便与中文文章共享同一 slug
	baseName := filepath.Base(filePath)
	slug := strings.TrimSuffix(baseName, filepath.Ext(baseName))
	slug = strings.TrimSuffix(slug, ".en")

	// 5. 转换 Markdown 到 HTML
	return fm, slug, mdToHTML(body), nil
}
