package loader

import (
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

// mdToHTML 将 Markdown 正文转换为 HTML 字符串。
//
// 转换流程：
//  1. 使用 parser 将 Markdown 解析为 AST（抽象语法树）
//  2. 使用 renderer 将 AST 渲染为 HTML
//
// 启用的扩展：
//   - CommonExtensions：表格、任务列表、删除线、脚注等
//   - AutoHeadingIDs：自动为标题生成 id 属性
//   - CommonFlags：默认 HTML 标式化选项
//   - HrefTargetBlank：链接在新窗口打开
func mdToHTML(body string) string {
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs
	p := parser.NewWithExtensions(extensions)

	doc := p.Parse([]byte(body))

	htmlFlags := html.CommonFlags | html.HrefTargetBlank
	opts := html.RendererOptions{Flags: htmlFlags}
	renderer := html.NewRenderer(opts)

	return string(markdown.Render(doc, renderer))
}
