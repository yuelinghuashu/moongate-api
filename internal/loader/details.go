package loader

import (
	"regexp"
	"strings"
)

// 背景：CommonMark/gomarkdown 把 <details>…</details> 整段当作"原始 HTML"透传，
// 不会解析其中的 Markdown（GitHub 能渲染是因为其渲染器专门支持 details 内的 Markdown，
// 标准实现没有）。于是 details 里的 ```go 围栏会原样变成纯文本、失去代码块与高亮。
//
// 对策：在 mdToHTML 之前做一次预处理——把 details 区域内的围栏代码块改写成真实的
// <pre><code class="language-…">…</code></pre>（内容做 HTML 转义）。details 区域
// 在解析时仍然原样透传，注入的 <pre><code> 因此能完整存活，前端 Shiki 即可正常高亮。

var (
	detailsOpenRe  = regexp.MustCompile(`(?i)^\s*<details(?:\s[^>]*)?>`)
	detailsCloseRe = regexp.MustCompile(`(?i)^\s*</details\s*>`)
)

// expandDetailsCodeFences 返回改写后的正文：仅处理 <details>…</details> 内的围栏，
// 其余内容保持逐字节不变。
//
// 扫描时需要同时跟踪两种状态，否则会把代码文本当结构：
//   - 顶层围栏：文章里作为示例出现的 <details> 可能落在围栏代码块内，那不是真区域；
//   - 区域内的围栏：details 内嵌的代码示例本身可能包含 <details>/</details>
//     （例如 Vue 代码示例里的折叠组件），把代码里的 </details> 当成区域结束，
//     会让围栏被截断、整段代码原样透传成 HTML——页面会显示乱码而不是代码块。
func expandDetailsCodeFences(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))

	i := 0
	var topFenceChar byte
	var topFenceLen int

	for i < len(lines) {
		trim := strings.TrimLeft(lines[i], " \t")

		// 顶层围栏内部：原样复制，绝不在这里识别 details。
		if topFenceChar != 0 {
			out = append(out, lines[i])
			if closingFence(trim, topFenceChar, topFenceLen) {
				topFenceChar = 0
			}
			i++
			continue
		}
		if c, n, _, ok := openingFence(trim); ok {
			topFenceChar, topFenceLen = c, n
			out = append(out, lines[i])
			i++
			continue
		}

		if !detailsOpenRe.MatchString(lines[i]) {
			out = append(out, lines[i])
			i++
			continue
		}

		// 收集整个 details 区域（含开/闭标签行）：只有不在围栏内、
		// 且嵌套深度归零的那个 </details> 才算区域结束。
		region := []string{lines[i]}
		depth := 1
		var fenceChar byte
		var fenceLen int
		closed := false

		j := i + 1
		for ; j < len(lines); j++ {
			lineTrim := strings.TrimLeft(lines[j], " \t")

			if fenceChar != 0 {
				region = append(region, lines[j])
				if closingFence(lineTrim, fenceChar, fenceLen) {
					fenceChar = 0
				}
				continue
			}
			if c, n, _, ok := openingFence(lineTrim); ok {
				fenceChar, fenceLen = c, n
				region = append(region, lines[j])
				continue
			}
			if detailsOpenRe.MatchString(lines[j]) {
				depth++
			} else if detailsCloseRe.MatchString(lines[j]) {
				depth--
				if depth == 0 {
					region = append(region, lines[j])
					closed = true
					j++
					break
				}
			}
			region = append(region, lines[j])
		}

		out = append(out, convertRegionFences(region)...)
		if closed {
			i = j
		} else {
			i = len(lines) // 未闭合：区域延伸到文档末尾
		}
	}
	return strings.Join(out, "\n")
}

// convertRegionFences 把 details 区域内"成对闭合"的围栏代码块转为 <pre><code>；
// 未闭合的围栏原样保留，避免破坏内容。
func convertRegionFences(region []string) []string {
	out := make([]string, 0, len(region))
	buf := make([]string, 0, 8) // 从围栏开行开始缓冲，闭合时整体替换
	inFence := false
	var fenceChar byte
	var fenceLen int
	var fenceLang string

	for _, ln := range region {
		trim := strings.TrimLeft(ln, " \t")

		if !inFence {
			if c, n, lang, ok := openingFence(trim); ok {
				inFence = true
				fenceChar, fenceLen, fenceLang = c, n, lang
				buf = []string{ln}
				continue
			}
			out = append(out, ln)
			continue
		}

		buf = append(buf, ln)
		if closingFence(trim, fenceChar, fenceLen) {
			out = append(out, fenceBlock(buf[1:len(buf)-1], fenceLang))
			inFence = false
		}
	}

	if inFence {
		// 区域结束仍未闭合：按原文还原，不冒险改写
		out = append(out, buf...)
	}
	return out
}

// openingFence 判断行首是否为合法的围栏开行（``` 或 ~~~，长度 >= 3），
// 返回围栏字符、长度与语言标识（info 串的第一个词）。
func openingFence(trim string) (char byte, length int, lang string, ok bool) {
	if len(trim) < 3 {
		return 0, 0, "", false
	}
	c := trim[0]
	if c != '`' && c != '~' {
		return 0, 0, "", false
	}
	n := 0
	for n < len(trim) && trim[n] == c {
		n++
	}
	if n < 3 {
		return 0, 0, "", false
	}
	info := strings.TrimSpace(trim[n:])
	if i := strings.IndexAny(info, " \t"); i >= 0 {
		info = info[:i]
	}
	return c, n, info, true
}

// closingFence 判断行是否为当前围栏的闭合行：同字符、长度 >= 开行、其余仅空白。
func closingFence(trim string, char byte, minLen int) bool {
	if trim == "" || trim[0] != char {
		return false
	}
	n := 0
	for n < len(trim) && trim[n] == char {
		n++
	}
	return n >= minLen && strings.TrimSpace(trim[n:]) == ""
}

// fenceCodeEscaper 用"命名实体"转义代码内容。
//
// 转义本身是必须的：代码里的 <、& 若原样进入 HTML 会破坏结构。
// 但"用哪种实体写法"不是跨仓契约——前端 app/utils/htmlEntities.ts 会按实体语法
// 统一解码（命名 + 数字），因此渲染器或清洗器把 &quot; 规整成 &#34; 也不会
// 让代码块显示成实体字面量。顺序必须先 & 后其余。
var fenceCodeEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&apos;",
)

// fenceBlock 生成 <pre><code class="language-…">…</code></pre>（内容 HTML 转义，
// 前端高亮会先反转义再交给 Shiki）。
func fenceBlock(bodyLines []string, lang string) string {
	attr := ""
	if lang != "" {
		attr = ` class="language-` + lang + `"`
	}
	code := fenceCodeEscaper.Replace(strings.Join(bodyLines, "\n"))
	return "<pre><code" + attr + ">" + code + "</code></pre>"
}
