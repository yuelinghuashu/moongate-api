package api

import (
	"encoding/json"
	"moongate-api/internal/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// setupDocsTestRouter 创建一个带有测试数据的 DocsHandler 路由
func setupDocsTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	doc1 := &domain.Doc{
		Title:       "Go Tutorial",
		Description: "Learn Go programming",
		Date:        time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC),
		Slug:        "go-tutorial",
		Series:      stringPtr("go-series"),
		Tags:        []string{"Go", "Tutorial"},
		Content:     "<p>Go tutorial content</p>",
	}

	doc2 := &domain.Doc{
		Title:       "Vue Guide",
		Description: "Vue.js framework guide",
		Date:        time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC),
		Slug:        "vue-guide",
		Series:      stringPtr("vue-series"),
		Tags:        []string{"Vue", "Frontend"},
		Content:     "<p>Vue guide content</p>",
	}

	doc3 := &domain.Doc{
		Title:       "Advanced Go",
		Description: "Deep dive into Go concurrency",
		Date:        time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC),
		Slug:        "advanced-go",
		Series:      stringPtr("go-series"),
		Tags:        []string{"Go", "Advanced"},
		Content:     "<p>Advanced Go content</p>",
	}

	// 英文译文：go-tutorial 有英文版，vue-guide / advanced-go 无英文版
	doc1En := &domain.Doc{
		Title:       "Go Tutorial (EN)",
		Description: "Learn Go programming (EN)",
		Date:        time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC),
		Slug:        "go-tutorial",
		Series:      stringPtr("go-series"),
		Tags:        []string{"Go", "Tutorial"},
		Content:     "<p>Go tutorial content (EN)</p>",
	}

	// 仅英文存在的文档（无中文配对）：测试兜底逻辑
	docEnOnly := &domain.Doc{
		Title:       "English Only Doc",
		Description: "No Chinese version",
		Date:        time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC),
		Slug:        "en-only",
		Tags:        []string{"English"},
		Content:     "<p>English only content</p>",
	}

	storeBySlug := map[string]*domain.Doc{
		doc1.Slug: doc1,
		doc2.Slug: doc2,
		doc3.Slug: doc3,
	}
	storeEn := map[string]*domain.Doc{
		doc1En.Slug:   doc1En,
		docEnOnly.Slug: docEnOnly,
	}

	handler := NewDocsHandler(storeBySlug, storeEn)
	r.GET("/api/docs", handler.GetDocs)
	r.GET("/api/docs/:slug", handler.GetDocBySlug)

	return r
}

func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}

func performRequest(r http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGetDocs_ListAndPagination(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs?page=1&limit=2")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data       []domain.DocSummary `json:"data"`
		Total      int                 `json:"total"`
		Page       int                 `json:"page"`
		Limit      int                 `json:"limit"`
		TotalPages int                 `json:"totalPages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Total != 3 {
		t.Errorf("Total = %d, want 3", resp.Total)
	}
	if len(resp.Data) != 2 {
		t.Errorf("Data length = %d, want 2", len(resp.Data))
	}
	if resp.Page != 1 {
		t.Errorf("Page = %d, want 1", resp.Page)
	}
	if resp.Limit != 2 {
		t.Errorf("Limit = %d, want 2", resp.Limit)
	}
	if resp.TotalPages != 2 {
		t.Errorf("TotalPages = %d, want 2", resp.TotalPages)
	}

	if resp.Data[0].Title != "Advanced Go" {
		t.Errorf("First doc title = %q, want %q", resp.Data[0].Title, "Advanced Go")
	}
}

func TestGetDocs_SortedByDateDesc(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data []domain.DocSummary `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Data) != 3 {
		t.Fatalf("Data length = %d, want 3", len(resp.Data))
	}

	expectedOrder := []string{"Advanced Go", "Vue Guide", "Go Tutorial"}
	for i, title := range expectedOrder {
		if resp.Data[i].Title != title {
			t.Errorf("Data[%d].Title = %q, want %q", i, resp.Data[i].Title, title)
		}
	}
}

func TestGetDocs_SummaryExcludesContent(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data []domain.DocSummary `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Data) > 0 {
		body := w.Body.String()
		if strings.Contains(body, "\"content\"") {
			t.Error("Summary response should not contain content field")
		}
	}
}

func TestGetDocs_IncludeContent(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs?content=true")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data []domain.Doc `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Data) != 3 {
		t.Fatalf("Data length = %d, want 3", len(resp.Data))
	}
	if resp.Data[0].Content == "" {
		t.Error("Content should be included when content=true")
	}
}

func TestGetDocs_SearchAll(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs?search=go")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data  []domain.DocSummary `json:"data"`
		Total int                 `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Total != 2 {
		t.Errorf("Total = %d, want 2", resp.Total)
	}
}

func TestGetDocs_SearchTitleOnly(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs?search=tutorial&searchMode=title")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data  []domain.DocSummary `json:"data"`
		Total int                 `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("Total = %d, want 1", resp.Total)
	}
	if len(resp.Data) != 1 || resp.Data[0].Title != "Go Tutorial" {
		t.Errorf("Data = %+v, want [Go Tutorial]", resp.Data)
	}
}

func TestGetDocs_InvalidSearchMode(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs?searchMode=invalid")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestGetDocs_FilterByTags(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs?tag=Go")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data  []domain.DocSummary `json:"data"`
		Total int                 `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Total != 2 {
		t.Errorf("Total = %d, want 2", resp.Total)
	}
}

func TestGetDocs_FilterByMultipleTags(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs?tag=Go&tag=Advanced")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data  []domain.DocSummary `json:"data"`
		Total int                 `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("Total = %d, want 1", resp.Total)
	}
	if len(resp.Data) != 1 || resp.Data[0].Title != "Advanced Go" {
		t.Errorf("Data = %+v, want [Advanced Go]", resp.Data)
	}
}

func TestGetDocs_SeriesGroup(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs?group=series")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var groups []domain.SeriesGroup
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(groups) != 2 {
		t.Fatalf("Groups length = %d, want 2", len(groups))
	}

	if groups[0].Slug != "go-series" {
		t.Errorf("Groups[0].Slug = %q, want %q", groups[0].Slug, "go-series")
	}
	if len(groups[0].Docs) != 2 {
		t.Errorf("Groups[0].Docs length = %d, want 2", len(groups[0].Docs))
	}
	if groups[1].Slug != "vue-series" {
		t.Errorf("Groups[1].Slug = %q, want %q", groups[1].Slug, "vue-series")
	}
	if len(groups[1].Docs) != 1 {
		t.Errorf("Groups[1].Docs length = %d, want 1", len(groups[1].Docs))
	}
}

// TestGetDocs_SeriesGroup_Order 验证系列内排序：
// 1) 显式 order 升序优先于日期
// 2) order 未设置时按日期升序（阅读顺序）
func TestGetDocs_SeriesGroup_Order(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// 系列：order-series。
	// a：order=2 但日期更早 —— 应排第 2（order 优先）
	// b：order=1 —— 应排第 1
	// c：无 order、日期早于 d —— 应排第 3
	// d：无 order、日期晚于 c —— 应排第 4
	a := &domain.Doc{
		Slug:   "a",
		Title:  "A",
		Date:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Series: stringPtr("order-series"),
		Order:  intPtr(2),
	}
	b := &domain.Doc{
		Slug:   "b",
		Title:  "B",
		Date:   time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		Series: stringPtr("order-series"),
		Order:  intPtr(1),
	}
	c := &domain.Doc{
		Slug:   "c",
		Title:  "C",
		Date:   time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
		Series: stringPtr("order-series"),
	}
	d := &domain.Doc{
		Slug:   "d",
		Title:  "D",
		Date:   time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC),
		Series: stringPtr("order-series"),
	}

	store := map[string]*domain.Doc{
		"a": a, "b": b, "c": c, "d": d,
	}
	h := NewDocsHandler(store, nil)
	r.GET("/api/docs", h.GetDocs)

	w := performRequest(r, "GET", "/api/docs?group=series")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var groups []domain.SeriesGroup
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	got := make([]string, 0, len(groups[0].Docs))
	for _, doc := range groups[0].Docs {
		got = append(got, doc.Slug)
	}
	want := []string{"b", "a", "c", "d"} // order 优先，其次日期升序
	if len(got) != len(want) {
		t.Fatalf("Series docs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Series docs[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
			break
		}
	}
}

func TestGetDocBySlug_Existing(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs/go-tutorial")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var doc domain.Doc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if doc.Title != "Go Tutorial" {
		t.Errorf("Title = %q, want %q", doc.Title, "Go Tutorial")
	}
	if doc.Content == "" {
		t.Error("Content should be included in single doc response")
	}
}

func TestGetDocBySlug_NotFound(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs/nonexistent")
	if w.Code != http.StatusNotFound {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ==================== 语言（lang）相关测试 ====================

func TestGetDocBySlug_LangEnglish(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs/go-tutorial?lang=en")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var doc domain.Doc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if doc.Title != "Go Tutorial (EN)" {
		t.Errorf("Title = %q, want %q", doc.Title, "Go Tutorial (EN)")
	}
	if doc.Lang != "en" {
		t.Errorf("Lang = %q, want %q", doc.Lang, "en")
	}
	if doc.IsFallback {
		t.Error("IsFallback = true, want false (English version exists)")
	}
	if !doc.HasTranslation {
		t.Error("HasTranslation = false, want true")
	}
}

func TestGetDocBySlug_LangEnglishFallbackToChinese(t *testing.T) {
	r := setupDocsTestRouter()

	// vue-guide 无英文译文，lang=en 应回退中文并标记
	w := performRequest(r, "GET", "/api/docs/vue-guide?lang=en")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var doc domain.Doc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if doc.Title != "Vue Guide" {
		t.Errorf("Title = %q, want %q", doc.Title, "Vue Guide")
	}
	if doc.Lang != "zh" {
		t.Errorf("Lang = %q, want %q", doc.Lang, "zh")
	}
	if !doc.IsFallback {
		t.Error("IsFallback = false, want true (no English version)")
	}
	if doc.HasTranslation {
		t.Error("HasTranslation = true, want false")
	}
}

func TestGetDocBySlug_DefaultLangHasTranslationFlag(t *testing.T) {
	r := setupDocsTestRouter()

	// 默认（无 lang 参数）行为不变，但应标记存在英文译文
	w := performRequest(r, "GET", "/api/docs/go-tutorial")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var doc domain.Doc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if doc.Title != "Go Tutorial" {
		t.Errorf("Title = %q, want %q", doc.Title, "Go Tutorial")
	}
	if doc.Lang != "zh" {
		t.Errorf("Lang = %q, want %q", doc.Lang, "zh")
	}
	if doc.IsFallback {
		t.Error("IsFallback = true, want false (default language)")
	}
	if !doc.HasTranslation {
		t.Error("HasTranslation = false, want true")
	}
}

func TestGetDocBySlug_EnglishOnlyFallback(t *testing.T) {
	r := setupDocsTestRouter()

	// en-only 无中文配对：默认语言下应兜底返回英文而非 404
	w := performRequest(r, "GET", "/api/docs/en-only")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var doc domain.Doc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if doc.Title != "English Only Doc" {
		t.Errorf("Title = %q, want %q", doc.Title, "English Only Doc")
	}
	if doc.Lang != "en" {
		t.Errorf("Lang = %q, want %q", doc.Lang, "en")
	}
	if !doc.IsFallback {
		t.Error("IsFallback = false, want true (no Chinese version)")
	}
}

func TestGetDocsList_LangEnglish(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs?lang=en")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data  []domain.DocSummary `json:"data"`
		Total int                 `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Total != 3 {
		t.Errorf("Total = %d, want 3", resp.Total)
	}

	// 按日期倒序：advanced-go, vue-guide, go-tutorial
	en := resp.Data[2] // go-tutorial 有英文译文
	if en.Title != "Go Tutorial (EN)" {
		t.Errorf("go-tutorial Title = %q, want %q", en.Title, "Go Tutorial (EN)")
	}
	if en.Lang != "en" || en.IsFallback || !en.HasTranslation {
		t.Errorf("go-tutorial lang flags = {lang:%s fallback:%v hasTrans:%v}, want {en false true}",
			en.Lang, en.IsFallback, en.HasTranslation)
	}

	zh := resp.Data[1] // vue-guide 无英文译文
	if zh.Title != "Vue Guide" {
		t.Errorf("vue-guide Title = %q, want %q", zh.Title, "Vue Guide")
	}
	if zh.Lang != "zh" || !zh.IsFallback || zh.HasTranslation {
		t.Errorf("vue-guide lang flags = {lang:%s fallback:%v hasTrans:%v}, want {zh true false}",
			zh.Lang, zh.IsFallback, zh.HasTranslation)
	}
}

func TestGetDocsList_IncludeContentLangEnglish(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs?content=true&lang=en")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data []domain.Doc `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Data) != 3 {
		t.Fatalf("Data length = %d, want 3", len(resp.Data))
	}
	en := resp.Data[2] // go-tutorial
	if !strings.Contains(en.Content, "(EN)") {
		t.Errorf("go-tutorial Content should be English, got %q", en.Content)
	}
	if en.Lang != "en" {
		t.Errorf("go-tutorial Lang = %q, want %q", en.Lang, "en")
	}
}

func TestGetDocsList_DefaultLangBackwardCompatible(t *testing.T) {
	r := setupDocsTestRouter()

	// 不带 lang 参数时，标题/内容与改造前一致
	w := performRequest(r, "GET", "/api/docs")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data  []domain.DocSummary `json:"data"`
		Total int                 `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Total != 3 {
		t.Errorf("Total = %d, want 3", resp.Total)
	}
	if resp.Data[2].Title != "Go Tutorial" {
		t.Errorf("go-tutorial Title = %q, want %q", resp.Data[2].Title, "Go Tutorial")
	}
	if resp.Data[2].Lang != "zh" {
		t.Errorf("go-tutorial Lang = %q, want %q", resp.Data[2].Lang, "zh")
	}
}

func TestGetDocs_SeriesGroupLangEnglish(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs?group=series&lang=en")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var groups []domain.SeriesGroup
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(groups) != 2 {
		t.Fatalf("Groups length = %d, want 2", len(groups))
	}
	// go-series 组内 go-tutorial 应显示英文标题
	found := false
	for _, g := range groups {
		if g.Slug != "go-series" {
			continue
		}
		for _, doc := range g.Docs {
			if doc.Slug == "go-tutorial" && doc.Title == "Go Tutorial (EN)" && doc.Lang == "en" {
				found = true
			}
		}
	}
	if !found {
		t.Error("go-series 组内 go-tutorial 应为英文标题且 Lang=en")
	}
}

func TestGetDocs_SearchInEnglish(t *testing.T) {
	r := setupDocsTestRouter()

	// lang=en 时搜索作用于英文标题/摘要
	w := performRequest(r, "GET", "/api/docs?lang=en&search=learn&searchMode=description")
	if w.Code != http.StatusOK {
		t.Fatalf("Status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data  []domain.DocSummary `json:"data"`
		Total int                 `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("Total = %d, want 1", resp.Total)
	}
	if len(resp.Data) != 1 || resp.Data[0].Title != "Go Tutorial (EN)" {
		t.Errorf("Data = %+v, want [Go Tutorial (EN)]", resp.Data)
	}
}
