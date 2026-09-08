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
func expandDetailsCodeFences(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))

	i := 0
	for i < len(lines) {
		if !detailsOpenRe.MatchString(lines[i]) {
			out = append(out, lines[i])
			i++
			continue
		}

		// 收集整个 details 区域（含开/闭标签行）
		region := []string{lines[i]}
		j := i + 1
		for j < len(lines) && !detailsCloseRe.MatchString(lines[j]) {
			region = append(region, lines[j])
			j++
		}
		closed := j < len(lines)
		if closed {
			region = append(region, lines[j])
		}

		out = append(out, convertRegionFences(region)...)
		if closed {
			i = j + 1
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

// fenceCodeEscaper 用"命名实体"转义代码内容：与前端 shikiProcessor 的
// 反转义清单（&lt;/&gt;/&amp;/&quot;/&apos;）严格对应，避免 &#34; 之类
// 数字实体无法被前端还原。顺序必须先 & 后其余。
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
