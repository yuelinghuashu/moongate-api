package loader

import (
	"fmt"
	"moongate-api/internal/domain"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

func ParseMarkdown[T any](filePath string) (T, error) {
	var result T

	// 1. 读取文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		return result, fmt.Errorf("读取文件失败: %w", err)
	}

	// 2. 分割 Frontmatter
	parts := strings.SplitN(string(data), "---", 3)
	if len(parts) != 3 {
		return result, fmt.Errorf("无效的 frontmatter 格式: %s", filePath)
	}
	frontmatter := parts[1]
	body := parts[2]

	// 3. 解析 YAML 到目标结构体
	err = yaml.Unmarshal([]byte(frontmatter), &result)
	if err != nil {
		return result, fmt.Errorf("解析 frontmatter 失败: %w", err)
	}

	// 4. 提取文件名作为 Slug（去掉扩展名）
	baseName := filepath.Base(filePath)
	slug := strings.TrimSuffix(baseName, filepath.Ext(baseName))

	// 5. 转换 Markdown 到 HTML
	htmlContent := mdToHTML(body)

	// 6. 通过接口设置 Slug、Content 字段
	if setter, ok := any(&result).(domain.ContentSetter); ok {
		setter.SetSlug(slug)
		setter.SetContent(htmlContent)
	}

	return result, nil
}
