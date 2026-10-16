package api

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"moongate-api/internal/catalog"
	"moongate-api/internal/loader"

	"github.com/gin-gonic/gin"
)

// contractSnapshot 是一份冻结的响应快照。
//
// 文件很小：正文（content）与摘要（description）这类"随文章编辑而变"的文本
// 在快照里被替换为占位符 <text>，只保留结构与所有稳定字段——
// 契约测试要守的就是这些，文章正文改几个字不该让护栏报警。
// 快照文件本身也不参与生产：Dockerfile 只 COPY 二进制与 content/，
// .dockerignore 另外排除了 testdata，因此不会进镜像。
type contractSnapshot struct {
	Code int `json:"code"`
	Body any `json:"body"`
}

// TestDocsAPIContract 是 HTTP 契约护栏：用真实 content/ 目录跑完整请求，
// 与 testdata/contract/*.json 中冻结的响应逐字段比对。
//
// 这些快照是分层重构（domain 解析模型 / catalog 查询层 / 薄 api 层）之前
// 从原实现抓取的真实响应，因此它们全部通过就意味着对外契约没被改动：
// 字段名、字段顺序、空值语义（series:null、order 省略）、信封结构、状态码、
// 以及每篇文档的 slug 与语言标记。
//
// 如果确实要有意修改响应结构，请先确认这是有意的契约变更，再重新生成快照
// （抓一份真实响应 → 替换 content/description 为 <text> → 覆盖对应快照文件）。
func TestDocsAPIContract(t *testing.T) {
	router := newRealContentRouter(t)

	cases := []struct {
		name string // testdata/contract 下的快照名
		path string // 请求路径（与快照对应的 query）
	}{
		{"01-list-default", "/api/docs?page=1&limit=3"},
		{"02-list-content", "/api/docs?page=1&limit=2&content=true"},
		{"03-list-lang-en", "/api/docs?page=1&limit=2&lang=en"},
		{"04-list-search-title", "/api/docs?search=nuxt&searchMode=title&limit=5"},
		{"05-list-search-all", "/api/docs?search=gorm&limit=5"},
		{"06-list-search-desc", "/api/docs?search=postgres&searchMode=description&limit=5"},
		{"07-list-tags", "/api/docs?tag=Go&tag=Engineering&limit=3"},
		{"08-list-tag-single", "/api/docs?tag=PostgreSQL&limit=3"},
		{"09-list-invalid-searchmode", "/api/docs?searchMode=bogus"},
		{"10-list-page-overflow", "/api/docs?page=99&limit=10"},
		{"11-series-group", "/api/docs?group=series&lang=en"},
		{"12-detail-zh", "/api/docs/nuxt-url-state-guide"},
		{"13-detail-fallback", "/api/docs/nuxt-url-state-guide?lang=en"},
		{"14-detail-en-only", "/api/docs/create-vscode-theme-basics?lang=en"},
		{"15-detail-notfound", "/api/docs/does-not-exist"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := readContractSnapshot(t, tc.name)

			w := performRequest(router, "GET", tc.path)
			if w.Code != want.Code {
				t.Fatalf("status = %d, want %d", w.Code, want.Code)
			}

			got := canonicalContract(t, w.Body.Bytes())
			wantBody := canonicalValue(t, want.Body)
			if !bytes.Equal(got, wantBody) {
				t.Errorf("响应结构偏离冻结契约\n got: %s\nwant: %s", got, wantBody)
			}
		})
	}
}

func newRealContentRouter(t *testing.T) *gin.Engine {
	t.Helper()

	contentDir := filepath.Join("..", "..", "content")
	if _, err := os.Stat(contentDir); err != nil {
		t.Skipf("跳过：未找到真实内容目录 %s（%v）", contentDir, err)
	}

	store, err := loader.LoadAll(contentDir)
	if err != nil {
		t.Fatalf("LoadAll(%s) error = %v", contentDir, err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	handler := NewDocsHandler(catalog.New(store.DocsBySlug, store.DocsEn))
	r.GET("/api/docs", handler.GetDocs)
	r.GET("/api/docs/:slug", handler.GetDocBySlug)

	return r
}

// canonicalContract 把响应体规范化为可比对的字节：
// 剔除随文章编辑而变的正文/摘要文本，但保留文档顺序与所有结构信息。
func canonicalContract(t *testing.T, raw []byte) []byte {
	t.Helper()

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, raw)
	}
	return canonicalValue(t, value)
}

func canonicalValue(t *testing.T, value any) []byte {
	t.Helper()

	out, err := json.Marshal(blankVolatileText(value))
	if err != nil {
		t.Fatalf("规范化失败: %v", err)
	}
	return out
}

// volatileDocFields 是"文章自身内容"字段：改了文章正文不应影响契约测试。
var volatileDocFields = map[string]bool{
	"content":     true,
	"description": true,
}

// blankVolatileText 递归把易变文本字段替换为占位符（快照与实测响应用同一套规则）。
func blankVolatileText(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, child := range v {
			if volatileDocFields[k] {
				out[k] = "<text>"
				continue
			}
			out[k] = blankVolatileText(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = blankVolatileText(child)
		}
		return out
	default:
		return value
	}
}

func readContractSnapshot(t *testing.T, name string) contractSnapshot {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "contract", name+".json"))
	if err != nil {
		t.Fatalf("读取契约快照失败: %v", err)
	}

	var snap contractSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("解析契约快照失败 (%s): %v", name, err)
	}
	return snap
}
