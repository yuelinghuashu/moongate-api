package loader

import (
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

// mdToHTML 将 Markdown 正文转换为 HTML 字符串（渲染 + 白名单清洗）。
//
// 这是内容进入内存的唯一入口：完成后 HTML 即定型，SSR 与浏览器拿到同一份内容，
// 前端因此不需要任何运行期清洗（清洗策略见 sanitize.go）。
//
// 转换流程：
//  0. 预处理：把 <details>…</details> 内的围栏代码块改写为 <pre><code>
//     （CommonMark/gomarkdown 会把 details 区域整体按原始 HTML 透传，
//     不解析其中的 Markdown 围栏，详见 details.go）
//  1. 使用 parser 将 Markdown 解析为 AST（抽象语法树）
//  2. 使用 renderer 将 AST 渲染为 HTML
//  3. 按白名单清洗渲染结果（只做减法）
//
// 启用的扩展：
//   - CommonExtensions：表格、任务列表、删除线、脚注等
//   - AutoHeadingIDs：自动为标题生成 id 属性
//   - CommonFlags：默认 HTML 标式化选项
//   - HrefTargetBlank：链接在新窗口打开
func mdToHTML(body string) string {
	return sanitizeHTML(renderMarkdown(body))
}

// renderMarkdown 只做 Markdown → HTML 的渲染，不清洗。
//
// 单独拆出来是为了让"清洗是不是纯白名单守卫"可以被直接验证：
// sanitize_test.go 对真实内容逐篇比较 renderMarkdown 与 mdToHTML，断言字节一致
// （即现有文章一条标记都没被削掉）。生产路径只用 mdToHTML。
func renderMarkdown(body string) string {
	body = expandDetailsCodeFences(body)

	extensions := parser.CommonExtensions | parser.AutoHeadingIDs
	p := parser.NewWithExtensions(extensions)

	doc := p.Parse([]byte(body))

	htmlFlags := html.CommonFlags | html.HrefTargetBlank
	opts := html.RendererOptions{Flags: htmlFlags}
	renderer := html.NewRenderer(opts)

	return string(markdown.Render(doc, renderer))
}
