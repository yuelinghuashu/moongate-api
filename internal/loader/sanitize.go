package loader

import (
	"regexp"

	"github.com/microcosm-cc/bluemonday"
)

// 本文件在"内容进入内存"这一刻对渲染出的 HTML 做一次白名单清洗。
//
// 为什么放在这里（而不是前端）：
//   - 清洗是内容的属性，不是渲染环境的属性。放在 loader 意味着 SSR 与浏览器
//     拿到的是同一份 HTML，前端无需再按 `import.meta.client` 分叉——那种分叉
//     会让服务端首帧与客户端首渲不一致（hydration 隐患）。
//   - 代价是"清洗后即定型"：前端 Shiki 之后注入的 <span style="--shiki-*"> 不在
//     本策略视野内（那是渲染产物，不是内容）。Shiki 的 codeToHtml 自行转义代码文本，
//     因此这条路径不引入注入面。
//
// 策略以"现有 108 篇文章逐字节不变"为准绳（见 sanitize_test.go），
// 因此这里只放行 gomarkdown 实际会产出、以及文章确实在用的标记。

// codeLangClassRe 限定 <code> 上允许的 class：仅 language-xxx。
//
// 这是跨仓不变量：前端 app/utils/shikiProcessor.ts 用
// /class="[^"]*language-(\w+)"/ 取语言，class 一旦被削掉，所有代码块都会
// 退化成纯文本（text）。同理 loader/details.go 注入的 <pre><code> 也依赖它。
var codeLangClassRe = regexp.MustCompile(`^language-[\w+-]+$`)

// htmlPolicy 是包级复用的清洗策略（bluemonday.Policy 构造后只读，可并发使用）。
var htmlPolicy = newHTMLPolicy()

// newHTMLPolicy 构造内容 HTML 的白名单策略。
//
// 基线用 UGCPolicy（覆盖 Markdown 常见标记：标题、列表、表格、引用、图片、链接、
// 删除线、脚注等），再按"gomarkdown 选项 + 文章实际用法"补齐少量放行项。
func newHTMLPolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()

	// 1. 折叠块：gomarkdown 把 <details>…</details> 整体按原始 HTML 透传
	//    （详见 details.go），因此这里必须放行，且保留展开状态。
	p.AllowElements("details", "summary")
	p.AllowAttrs("open").OnElements("details")

	// 2. 标题锚点：parser.AutoHeadingIDs 生成的 id 是大纲（useOutline）与
	//    #hash 跳转的依据，削掉就等于目录失效。
	p.AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6")

	// 3. 代码块语言标记（见 codeLangClassRe 注释）。
	p.AllowAttrs("class").Matching(codeLangClassRe).OnElements("code")

	// 4. html.HrefTargetBlank 给链接加 target="_blank"，属渲染约定的一部分；
	//    文章里手写的 rel="noopener noreferrer" 也要原样保留（策略只做减法，
	//    不替作者重写 rel）。
	p.AllowAttrs("target", "rel").OnElements("a")

	// 5. 任务列表：gomarkdown 的 CommonExtensions 产出禁用的复选框。
	p.AllowAttrs("type", "checked", "disabled").OnElements("input")
	p.AllowElements("input")

	// 6. 脚注锚点。
	p.AllowAttrs("id").OnElements("sup", "li", "div")
	p.AllowAttrs("href").OnElements("sup", "a")

	// 7. 图片排版：文章用行内 style 控制演示图宽度。bluemonday 会用内置的
	//    CSS 解析器清洗声明（url(javascript:)、expression 等会被剔除），
	//    因此这里放宽 style 不等于放宽脚本执行。
	p.AllowAttrs("style").OnElements("img")

	// 8. 允许"无协议"链接：文章结尾的 href="#"（返回顶部）与站内相对路径。
	//    注意这里**不能**用 RequireParseableURLs(false)——那条分支会无条件放行
	//    任意 URL（含 javascript:/data:）。AllowRelativeURLs 只在
	//    url.Parse 成功、且 u.Scheme == "" 时才放行，
	//    因此协议白名单（http/https/mailto/tel/ftp）与 javascript: 拦截依旧生效。
	p.AllowRelativeURLs(true)

	// 9. 不要新增属性：清洗只做减法。
	//    默认策略会给链接补 rel="nofollow"，那属于"改写内容"，会让
	//    "清洗前后一致"的守卫误报，也会改变现有页面行为。
	p.RequireNoFollowOnLinks(false)
	p.RequireNoReferrerOnLinks(false)
	p.AddTargetBlankToFullyQualifiedLinks(false)

	return p
}

// sanitizeHTML 按 htmlPolicy 清洗一段渲染出的 HTML。
func sanitizeHTML(html string) string {
	return htmlPolicy.Sanitize(html)
}
