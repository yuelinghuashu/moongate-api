package loader

import (
	"regexp"
	"strings"
	"testing"

	"golang.org/x/net/html"
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
	// 代码内容做了 HTML 转义（防止 <、&、引号破坏结构）。
	//
	// 这里断言"解码后的源码"，而不是实体的具体写法：&quot; 与 &#34; 语义等价，
	// 前端 app/utils/htmlEntities.ts 两种都能还原（渲染器/清洗器换写法不再
	// 影响高亮），因此转义怎么写不是跨仓契约，能不能还原才是。
	code := firstCodeText(t, htmlOut)
	wantCode := "package main\n\nfunc main() { // 含 <尖括号> & 与 \"引号\"\n}"
	if code != wantCode {
		t.Fatalf("代码块文本还原后不一致\n got: %q\nwant: %q\nHTML:\n%s", code, wantCode, htmlOut)
	}
}

// firstCodeText 取出 HTML 中第一个 <code> 元素的纯文本（实体已解码）。
func firstCodeText(t *testing.T, doc string) string {
	t.Helper()

	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("解析 HTML 失败: %v", err)
	}

	var code *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if code != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "code" {
			code = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(root)

	if code == nil {
		t.Fatalf("未找到 <code> 元素:\n%s", doc)
	}
	return codeText(code)
}

// codeText 递归拼接节点及其后代的文本。
func codeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
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

// TestOpeningFence 覆盖围栏开行解析的边界：过短的行、非围栏字符、
// 只有围栏没有语言标识、以及带额外 info 串（只取第一个词）。
func TestOpeningFence(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantOK   bool
		wantChar byte
		wantLen  int
		wantLang string
	}{
		{"空行", "", false, 0, 0, ""},
		{"不足三个字符", "``", false, 0, 0, ""},
		{"非围栏字符", "abc", false, 0, 0, ""},
		{"单字符", "`", false, 0, 0, ""},
		{"仅反引号无语言", "```", true, '`', 3, ""},
		{"带语言", "```go", true, '`', 3, "go"},
		{"波浪号围栏", "~~~python", true, '~', 3, "python"},
		{"长围栏带多余 info", "`````js {1,3}", true, '`', 5, "js"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			char, length, lang, ok := openingFence(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if char != tt.wantChar || length != tt.wantLen || lang != tt.wantLang {
				t.Errorf("got (%q, %d, %q), want (%q, %d, %q)",
					char, length, lang, tt.wantChar, tt.wantLen, tt.wantLang)
			}
		})
	}
}
