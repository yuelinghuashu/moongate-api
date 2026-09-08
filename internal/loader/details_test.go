package loader

import (
	"regexp"
	"strings"
	"testing"
)

// go-agent 等文章的真实写法：<details><summary>…</summary> 后跟空行再放 ```go 围栏。
const detailsGoSample = `<details>
<summary>demo/plain-chat/main.go 全文（点击展开）</summary>

` + "```go\npackage main\n\nfunc main() { // 含 <尖括号> & 与 \"引号\"\n}\n```\n" + `
</details>
`

func TestMdToHTML_DetailsFenceBecomesCodeBlock(t *testing.T) {
	htmlOut := mdToHTML(detailsGoSample)

	// details 结构仍在
	if !strings.Contains(htmlOut, "<details>") || !strings.Contains(htmlOut, "</details>") {
		t.Fatalf("details 标签缺失:\n%s", htmlOut)
	}
	// 围栏被改写成真实代码块
	if !strings.Contains(htmlOut, `<pre><code class="language-go">`) {
		t.Fatalf("details 内没有生成 <pre><code class=\"language-go\">:\n%s", htmlOut)
	}
	// 反引号围栏不应再原样出现
	if strings.Contains(htmlOut, "```go") {
		t.Fatalf("```go 围栏仍以纯文本出现:\n%s", htmlOut)
	}
	// 代码内容做了 HTML 转义（防止 <、&、引号破坏结构）
	if !strings.Contains(htmlOut, "含 &lt;尖括号&gt; &amp; 与 &quot;引号&quot;") {
		t.Fatalf("代码内容未正确转义:\n%s", htmlOut)
	}
}

func TestMdToHTML_DetailsFenceMatchesShikiPattern(t *testing.T) {
	// 前端 shikiProcessor 用 /<pre>\s*<code([^>]*)>([\s\S]*?)<\/code>\s*<\/pre>/g 匹配，
	// 这里直接验证注入的代码块能被该正则命中。
	re := regexp.MustCompile(`<pre>\s*<code([^>]*)>([\s\S]*?)</code>\s*</pre>`)
	htmlOut := mdToHTML(detailsGoSample)
	if !re.MatchString(htmlOut) {
		t.Fatalf("注入的代码块不符合前端高亮正则:\n%s", htmlOut)
	}
}

func TestMdToHTML_FenceOutsideDetailsUnchanged(t *testing.T) {
	body := "```go\npackage main\n```\n"
	htmlOut := mdToHTML(body)
	if strings.Contains(htmlOut, "```go") {
		t.Fatalf("details 外部的围栏不应被改写:\n%s", htmlOut)
	}
	if !strings.Contains(htmlOut, `<pre><code class="language-go">`) {
		t.Fatalf("普通围栏应照常生成代码块:\n%s", htmlOut)
	}
}

func TestExpandDetailsCodeFences_UnclosedFenceKept(t *testing.T) {
	// 开行 ```go 后是同字符 ```` （长度 4 > 3，可算闭合），因此用不同字符 ~~~ 保证未闭合：
	body := "<details>\n<summary>s</summary>\n\n```go\npackage main\n~~~\n</details>\n"
	out := expandDetailsCodeFences(body)
	// ~~~ 与 ``` 不同字符，不构成闭合 → 围栏整段应原样保留
	if !strings.Contains(out, "```go") {
		t.Fatalf("未闭合围栏不应被改写:\n%s", out)
	}
	if strings.Contains(out, "<pre><code") {
		t.Fatalf("未闭合围栏不应生成代码块:\n%s", out)
	}
}

func TestExpandDetailsCodeFences_RegionWithoutCloseTag(t *testing.T) {
	body := "<details>\n<summary>s</summary>\n\n```bash\necho hi\n```\n" // 无 </details>
	out := expandDetailsCodeFences(body)
	if !strings.Contains(out, `<pre><code class="language-bash">`) {
		t.Fatalf("区域未闭合到文档末尾也应转换:\n%s", out)
	}
}

func TestExpandDetailsCodeFences_CaseInsensitiveTag(t *testing.T) {
	body := "<DETAILS>\n<summary>s</summary>\n\n```js\nvar x = 1\n```\n</DETAILS>\n"
	out := expandDetailsCodeFences(body)
	if !strings.Contains(out, `<pre><code class="language-js">`) {
		t.Fatalf("大小写标签未处理:\n%s", out)
	}
}

func TestExpandDetailsCodeFences_MultipleBlocksInOneDetails(t *testing.T) {
	body := "<details>\n<summary>s</summary>\n\n```go\npackage main\n```\n\n```bash\necho ok\n```\n\n</details>\n"
	out := expandDetailsCodeFences(body)
	if strings.Count(out, "<pre><code") != 2 {
		t.Fatalf("应转换区域内两个围栏，实际:\n%s", out)
	}
}
