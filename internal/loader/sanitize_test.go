package loader

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// TestSanitizePolicyIsNoOpOnRealContent 是清洗策略的硬门槛与永久守卫。
//
// 它把 content/docs 下每一篇文章正文跑两遍——一遍只渲染（renderMarkdown），
// 一遍渲染后清洗（mdToHTML）——断言两者**语义相同**。
//
// 为什么比字节而不是比语义：
//   - bluemonday 会把 HTML 解析后重新序列化，实体写法会变（&quot; ↔ &#34;），
//     字节比较会把这种等价改写误报成"削掉了东西"；
//   - 真正要守的是"没有元素、属性或文本被移除/改写"，DOM 投影恰好只表达这件事。
//
// 两个作用：
//  1. 上线前的等价性证明：白名单一旦削掉任何现有标记（details/summary、标题 id、
//     code 的 language-* class、表格、任务列表、脚注……），这里立刻红，
//     而不是等页面上代码高亮或目录悄悄失效；
//  2. 之后的守卫：新文章若用到策略放行范围之外的原始 HTML，这里会指出文件与
//     具体节点——由作者决定是放宽策略还是改文章，不做静默削减。
func TestSanitizePolicyIsNoOpOnRealContent(t *testing.T) {
	docsDir := filepath.Join("..", "..", "content", "docs")
	if _, err := os.Stat(docsDir); err != nil {
		t.Skipf("跳过：未找到真实内容目录 %s（%v）", docsDir, err)
	}

	count := 0
	err := filepath.WalkDir(docsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body := bodyAfterFrontmatter(t, path, string(data))

		count++
		want := htmlProjection(t, path, renderMarkdown(body))
		got := htmlProjection(t, path, mdToHTML(body))
		if diff := firstProjectionDiff(want, got); diff != "" {
			t.Errorf("%s: 清洗改变了渲染结果\n%s", path, diff)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历内容目录失败: %v", err)
	}

	if count == 0 {
		t.Fatal("未遍历到任何文章，守卫形同虚设")
	}
	t.Logf("已核对 %d 篇文章：清洗对现有内容无元素/属性/文本改动", count)
}

// htmlProjection 把一段 HTML 摊平成可比对的事件序列：
// 每个元素记 "<tag attr=val …>"（属性名排序，避免属性顺序差异），
// 每个非空文本节点记 "TEXT:<原文>"。忽略注释与纯空白文本。
//
// 不比较树形嵌套：清洗删掉一个未知标签时子内容会被提升一层，
// 这属于"标签没了"而非"内容没了"，用事件序列能精确表达这两者的区别。
func htmlProjection(t *testing.T, path, doc string) []string {
	t.Helper()

	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("%s: 解析 HTML 失败: %v", path, err)
	}

	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.ElementNode:
			attrs := make([]string, 0, len(n.Attr))
			for _, a := range n.Attr {
				attrs = append(attrs, a.Key+"="+a.Val)
			}
			sort.Strings(attrs)
			line := "<" + n.Data
			if len(attrs) > 0 {
				line += " " + strings.Join(attrs, " ")
			}
			out = append(out, line+">")
		case html.TextNode:
			if text := strings.TrimSpace(n.Data); text != "" {
				out = append(out, "TEXT:"+text)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)

	return out
}

// firstProjectionDiff 返回首个差异的可读描述；无差异返回空串。
func firstProjectionDiff(want, got []string) string {
	limit := len(want)
	if len(got) < limit {
		limit = len(got)
	}

	i := 0
	for i < limit && want[i] == got[i] {
		i++
	}
	if i == limit && len(want) == len(got) {
		return ""
	}

	clip := func(s []string) string {
		end := i + 6
		if end > len(s) {
			end = len(s)
		}
		start := i - 2
		if start < 0 {
			start = 0
		}
		return strings.Join(s[start:end], "\n      ")
	}

	return "  第 " + itoa(i) + " 个事件起不同（渲染 %d 个事件 / 清洗 %d 个事件）\n" +
		"    渲染:\n      " + clip(want) + "\n    清洗:\n      " + clip(got)
}

// bodyAfterFrontmatter 取出 "---" 包围的 frontmatter 之后的正文。
func bodyAfterFrontmatter(t *testing.T, path, raw string) string {
	t.Helper()

	parts := strings.SplitN(raw, "---", 3)
	if len(parts) != 3 {
		t.Fatalf("%s: 无效的 frontmatter 格式", path)
	}
	return parts[2]
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
