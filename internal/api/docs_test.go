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
		Permalink:   "/docs/go-tutorial",
		Slug:        "go-tutorial",
		Level:       domain.LevelP1,
		Series:      stringPtr("go-series"),
		Tags:        []string{"Go", "Tutorial"},
		Content:     "<p>Go tutorial content</p>",
	}

	doc2 := &domain.Doc{
		Title:       "Vue Guide",
		Description: "Vue.js framework guide",
		Date:        time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC),
		Permalink:   "/docs/vue-guide",
		Slug:        "vue-guide",
		Level:       domain.LevelP2,
		Series:      stringPtr("vue-series"),
		Tags:        []string{"Vue", "Frontend"},
		Content:     "<p>Vue guide content</p>",
	}

	doc3 := &domain.Doc{
		Title:       "Advanced Go",
		Description: "Deep dive into Go concurrency",
		Date:        time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC),
		Permalink:   "/docs/advanced-go",
		Slug:        "advanced-go",
		Level:       domain.LevelP3,
		Series:      stringPtr("go-series"),
		Tags:        []string{"Go", "Advanced"},
		Content:     "<p>Advanced Go content</p>",
	}

	store := map[string]*domain.Doc{
		doc1.Permalink: doc1,
		doc2.Permalink: doc2,
		doc3.Permalink: doc3,
	}
	storeBySlug := map[string]*domain.Doc{
		doc1.Slug: doc1,
		doc2.Slug: doc2,
		doc3.Slug: doc3,
	}

	handler := NewDocsHandler(store, storeBySlug)
	r.GET("/api/docs", handler.GetDocs)
	r.GET("/api/docs/:slug", handler.GetDocBySlug)

	return r
}

func stringPtr(s string) *string {
	return &s
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

func TestGetDocs_FilterByLevel(t *testing.T) {
	r := setupDocsTestRouter()

	w := performRequest(r, "GET", "/api/docs?level=P3")
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
