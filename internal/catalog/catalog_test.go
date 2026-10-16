package catalog

import (
	"sync"
	"testing"
	"time"

	"moongate-api/internal/domain"
)

// doc 构造一个测试用文档（frontmatter + slug + 正文）。
func doc(fm domain.Frontmatter, slug, content string) *domain.Doc {
	return &domain.Doc{Frontmatter: fm, Slug: slug, Content: content}
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

var baseDate = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// newFixture 构造一个覆盖回退场景的 catalog：
//
//	zh: with-en(有英文译文)、zh-only(仅中文)
//	en: with-en(英文译文)、en-only(仅英文，无中文配对)
func newFixture() *Catalog {
	zh := map[string]*domain.Doc{
		"with-en": doc(domain.Frontmatter{
			Title: "中文标题", Description: "中文摘要", Date: baseDate,
			Series: strPtr("s1"), Order: intPtr(1), Tags: []string{"Go"},
		}, "with-en", "<p>中文正文</p>"),
		"zh-only": doc(domain.Frontmatter{
			Title: "仅中文", Description: "中文摘要", Date: baseDate.Add(time.Hour),
			Tags: []string{"Vue"},
		}, "zh-only", "<p>仅中文正文</p>"),
	}
	en := map[string]*domain.Doc{
		"with-en": doc(domain.Frontmatter{
			Title: "EN Title", Description: "EN description", Date: baseDate,
			Series: strPtr("s1"), Order: intPtr(1), Tags: []string{"Go"},
		}, "with-en", "<p>EN content</p>"),
		"en-only": doc(domain.Frontmatter{
			Title: "EN Only", Description: "EN only description", Date: baseDate.Add(2 * time.Hour),
		}, "en-only", "<p>EN only content</p>"),
	}
	return New(zh, en)
}

// TestResolve_FallbackMatrix 覆盖语言回退的六种落点。
func TestResolve_FallbackMatrix(t *testing.T) {
	c := newFixture()

	tests := []struct {
		name           string
		slug           string
		lang           string
		wantOK         bool
		wantTitle      string
		wantLang       string
		wantIsFallback bool
		wantHasTrans   bool
	}{
		{"中文请求-有译文", "with-en", LangZH, true, "中文标题", LangZH, false, true},
		{"中文请求-无译文", "zh-only", LangZH, true, "仅中文", LangZH, false, false},
		{"中文请求-仅英文兜底", "en-only", LangZH, true, "EN Only", LangEN, true, true},
		{"不存在的语言按默认处理", "with-en", "ja", true, "中文标题", LangZH, false, true},
		{"英文请求-有译文", "with-en", LangEN, true, "EN Title", LangEN, false, true},
		{"英文请求-无译文回退中文", "zh-only", LangEN, true, "仅中文", LangZH, true, false},
		{"英文请求-仅英文", "en-only", LangEN, true, "EN Only", LangEN, false, true},
		{"不存在", "nope", LangZH, false, "", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, ok := c.resolve(tt.slug, tt.lang)
			if ok != tt.wantOK {
				t.Fatalf("resolve(%q,%q) ok = %v, want %v", tt.slug, tt.lang, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if e.doc.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", e.doc.Title, tt.wantTitle)
			}
			if e.lang != tt.wantLang {
				t.Errorf("lang = %q, want %q", e.lang, tt.wantLang)
			}
			if e.isFallback != tt.wantIsFallback {
				t.Errorf("isFallback = %v, want %v", e.isFallback, tt.wantIsFallback)
			}
			if e.hasTranslation != tt.wantHasTrans {
				t.Errorf("hasTranslation = %v, want %v", e.hasTranslation, tt.wantHasTrans)
			}
		})
	}
}

// TestDetail 验证详情响应：无译文时回退中文，正文与语言字段同时切换。
func TestDetail(t *testing.T) {
	c := newFixture()

	en, ok := c.Detail("with-en", LangEN)
	if !ok {
		t.Fatal("Detail(with-en, en) should exist")
	}
	if en.Content != "<p>EN content</p>" || en.Lang != LangEN || en.Title != "EN Title" {
		t.Errorf("英文详情 = {title:%q lang:%q content:%q}", en.Title, en.Lang, en.Content)
	}

	zh, ok := c.Detail("with-en", LangZH)
	if !ok {
		t.Fatal("Detail(with-en, zh) should exist")
	}
	if zh.Content != "<p>中文正文</p>" || zh.Lang != LangZH {
		t.Errorf("中文详情 = {lang:%q content:%q}", zh.Lang, zh.Content)
	}

	fallback, ok := c.Detail("zh-only", LangEN)
	if !ok {
		t.Fatal("Detail(zh-only, en) 应回退而非 404")
	}
	if !fallback.IsFallback || fallback.Lang != LangZH || fallback.Title != "仅中文" {
		t.Errorf("回退详情 = {title:%q lang:%q fallback:%v}", fallback.Title, fallback.Lang, fallback.IsFallback)
	}

	if _, ok := c.Detail("nope", LangZH); ok {
		t.Error("Detail(nope) 不应存在")
	}
}

// TestList_EnglishUsesEnglishContent 回归测试：lang=en 时列表必须返回英文
// 标题与正文，而不是"标题英文、正文中文"这类混合结果。
func TestList_EnglishUsesEnglishContent(t *testing.T) {
	c := newFixture()

	page := c.List(LangEN, ListOptions{Page: 1, Limit: 10, SearchMode: "all", WithContent: true})

	details, ok := page.Data.([]domain.DocDetail)
	if !ok {
		t.Fatalf("Data 类型 = %T, want []domain.DocDetail", page.Data)
	}
	// 列表语料只含中文文档（with-en、zh-only），仅英文的 en-only 不进入列表
	if len(details) != 2 {
		t.Fatalf("列表长度 = %d, want 2（仅英文文档不进入列表）", len(details))
	}

	byTitle := make(map[string]domain.DocDetail, len(details))
	for _, d := range details {
		byTitle[d.Slug] = d
	}

	withEn := byTitle["with-en"]
	if withEn.Title != "EN Title" || withEn.Content != "<p>EN content</p>" || withEn.Lang != LangEN {
		t.Errorf("with-en = {title:%q lang:%q content:%q}, want 英文标题+英文正文",
			withEn.Title, withEn.Lang, withEn.Content)
	}

	zhOnly := byTitle["zh-only"]
	if zhOnly.Title != "仅中文" || !zhOnly.IsFallback || zhOnly.Lang != LangZH {
		t.Errorf("zh-only = {title:%q lang:%q fallback:%v}, want 中文回退",
			zhOnly.Title, zhOnly.Lang, zhOnly.IsFallback)
	}
}

// TestList_PaginationAndSorting 验证排序（日期降序）与分页越界保护。
func TestList_PaginationAndSorting(t *testing.T) {
	c := New(map[string]*domain.Doc{
		"a": doc(domain.Frontmatter{Title: "A", Date: baseDate}, "a", ""),
		"b": doc(domain.Frontmatter{Title: "B", Date: baseDate.Add(2 * time.Hour)}, "b", ""),
		"c": doc(domain.Frontmatter{Title: "C", Date: baseDate.Add(time.Hour)}, "c", ""),
	}, nil)

	page := c.List(LangZH, ListOptions{Page: 1, Limit: 10, SearchMode: "all"})
	if page.Total != 3 || page.TotalPages != 1 {
		t.Errorf("Total/TotalPages = %d/%d, want 3/1", page.Total, page.TotalPages)
	}
	summaries := page.Data.([]domain.DocSummary)
	got := []string{summaries[0].Slug, summaries[1].Slug, summaries[2].Slug}
	want := []string{"b", "c", "a"} // 日期降序
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("顺序 = %v, want %v", got, want)
		}
	}

	// 页码越界：不 panic，data 为空但 total 反映全量
	overflow := c.List(LangZH, ListOptions{Page: 99, Limit: 10, SearchMode: "all"})
	if len(overflow.Data.([]domain.DocSummary)) != 0 || overflow.Total != 3 {
		t.Errorf("越界分页 = {len:%d total:%d}, want {0 3}",
			len(overflow.Data.([]domain.DocSummary)), overflow.Total)
	}
	if overflow.Page != 99 || overflow.Limit != 10 {
		t.Errorf("回显分页参数 = %d/%d, want 99/10", overflow.Page, overflow.Limit)
	}
}

// TestList_SameDateDeterministic 同日期文档顺序必须确定（避免 SSR 水合不一致）。
func TestList_SameDateDeterministic(t *testing.T) {
	zh := make(map[string]*domain.Doc)
	slugs := []string{"zeta", "alpha", "mu", "beta"}
	for _, s := range slugs {
		zh[s] = doc(domain.Frontmatter{Title: s, Date: baseDate}, s, "")
	}
	c := New(zh, nil)

	want := []string{"alpha", "beta", "mu", "zeta"} // 同日期 → slug 升序
	for attempt := 0; attempt < 100; attempt++ {
		page := c.List(LangZH, ListOptions{Page: 1, Limit: 10, SearchMode: "all"})
		for i, s := range page.Data.([]domain.DocSummary) {
			if s.Slug != want[i] {
				t.Fatalf("attempt %d: data[%d] = %q, want %q", attempt, i, s.Slug, want[i])
			}
		}
	}
}

// TestList_SameDateSameSeries_OrderDesc 同日期同系列按 order 降序（列表页"最新在前"）。
func TestList_SameDateSameSeries_OrderDesc(t *testing.T) {
	zh := map[string]*domain.Doc{
		"first":    doc(domain.Frontmatter{Title: "1", Date: baseDate, Series: strPtr("s"), Order: intPtr(1)}, "first", ""),
		"second":   doc(domain.Frontmatter{Title: "2", Date: baseDate, Series: strPtr("s"), Order: intPtr(2)}, "second", ""),
		"no-order": doc(domain.Frontmatter{Title: "x", Date: baseDate, Series: strPtr("s")}, "no-order", ""),
	}
	c := New(zh, nil)

	page := c.List(LangZH, ListOptions{Page: 1, Limit: 10, SearchMode: "all"})
	got := page.Data.([]domain.DocSummary)
	want := []string{"second", "first", "no-order"} // order 2 → 1 → 无 order
	for i := range want {
		if got[i].Slug != want[i] {
			t.Fatalf("顺序[%d] = %q, want %q（全序：%v）", i, got[i].Slug, want[i], slugsOf(got))
		}
	}
}

func slugsOf(docs []domain.DocSummary) []string {
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.Slug)
	}
	return out
}

// TestList_SearchAndTags 验证搜索模式与标签过滤（作用于请求语言的标题/摘要）。
func TestList_SearchAndTags(t *testing.T) {
	c := newFixture()

	tests := []struct {
		name string
		lang string
		opts ListOptions
		want []string
	}{
		{"中文搜索标题", LangZH, ListOptions{Search: "仅中文", SearchMode: "title"}, []string{"zh-only"}},
		{"中文搜索未命中", LangZH, ListOptions{Search: "EN Title", SearchMode: "title"}, nil},
		{"英文搜索标题", LangEN, ListOptions{Search: "EN Title", SearchMode: "title"}, []string{"with-en"}},
		{"英文搜索摘要", LangEN, ListOptions{Search: "EN description", SearchMode: "description"}, []string{"with-en"}},
		{"all 覆盖摘要", LangZH, ListOptions{Search: "中文摘要", SearchMode: "all"}, []string{"zh-only", "with-en"}}, // 日期降序
		{"标签命中", LangZH, ListOptions{Tags: []string{"go"}}, []string{"with-en"}},
		{"标签全不命中", LangZH, ListOptions{Tags: []string{"Rust"}}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts
			opts.Page, opts.Limit = 1, 10
			if opts.SearchMode == "" {
				opts.SearchMode = "all"
			}
			page := c.List(tt.lang, opts)
			got := slugsOf(page.Data.([]domain.DocSummary))
			if len(got) != len(tt.want) {
				t.Fatalf("命中 = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("命中 = %v, want %v", got, tt.want)
				}
			}
			if page.Total != len(tt.want) {
				t.Errorf("Total = %d, want %d", page.Total, len(tt.want))
			}
		})
	}
}

// TestSeries_GroupingAndOrder 系列分组：按 slug 升序，组内 order 优先、
// 未声明 order 的按日期升序填充空位。
func TestSeries_GroupingAndOrder(t *testing.T) {
	zh := map[string]*domain.Doc{
		// series "b"：显式 order 的占位，其余按日期升序填空
		"b2": doc(domain.Frontmatter{Title: "B2", Date: baseDate.Add(9 * time.Hour), Series: strPtr("b"), Order: intPtr(2)}, "b2", ""),
		"b1": doc(domain.Frontmatter{Title: "B1", Date: baseDate}, "b1", ""),
		"b3": doc(domain.Frontmatter{Title: "B3", Date: baseDate.Add(time.Hour), Series: strPtr("b")}, "b3", ""),
		// series "a"：仅一篇
		"a1": doc(domain.Frontmatter{Title: "A1", Date: baseDate, Series: strPtr("a")}, "a1", ""),
		// 无系列：不出现在分组里
		"solo": doc(domain.Frontmatter{Title: "Solo", Date: baseDate}, "solo", ""),
	}
	c := New(zh, nil)

	groups := c.Series(LangZH)
	if len(groups) != 2 {
		t.Fatalf("分组数 = %d, want 2（无系列文档不入组）", len(groups))
	}
	if groups[0].Slug != "a" || groups[1].Slug != "b" {
		t.Fatalf("分组顺序 = [%s %s], want [a b]", groups[0].Slug, groups[1].Slug)
	}
	for _, g := range groups {
		for _, d := range g.Docs {
			if d.Slug == "solo" {
				t.Error("无系列文档不应出现在系列分组里")
			}
		}
	}

	got := slugsOf(groups[1].Docs)
	want := []string{"b3", "b2"} // order=2 占第 2 位，未声明 order 的 b3 填第 1 位
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("b 组顺序 = %v, want %v", got, want)
		}
	}
}

// TestSeries_OrderOutOfRangeIgnored order 超出 1..n 时忽略（用日期兜底），且不 panic。
func TestSeries_OrderOutOfRangeIgnored(t *testing.T) {
	zh := map[string]*domain.Doc{
		"x": doc(domain.Frontmatter{Title: "X", Date: baseDate, Series: strPtr("s"), Order: intPtr(99)}, "x", ""),
		"y": doc(domain.Frontmatter{Title: "Y", Date: baseDate.Add(time.Hour), Series: strPtr("s")}, "y", ""),
	}
	c := New(zh, nil)

	got := slugsOf(c.Series(LangZH)[0].Docs)
	want := []string{"x", "y"} // 非法 order 被忽略 → 按日期升序
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("顺序 = %v, want %v", got, want)
		}
	}
}

// TestSeries_EnglishFallback 系列分组同样遵循语言回退。
func TestSeries_EnglishFallback(t *testing.T) {
	c := newFixture()

	groups := c.Series(LangEN)
	if len(groups) != 1 {
		t.Fatalf("分组数 = %d, want 1", len(groups))
	}
	d := groups[0].Docs[0]
	if d.Title != "EN Title" || d.Lang != LangEN || d.IsFallback {
		t.Errorf("系列内文档 = {title:%q lang:%q fallback:%v}, want 英文原文",
			d.Title, d.Lang, d.IsFallback)
	}
}

// TestConcurrentReads 并发读取（配合 -race）验证 catalog 只读、不污染共享存储。
func TestConcurrentReads(t *testing.T) {
	c := newFixture()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lang := LangZH
			if i%2 == 0 {
				lang = LangEN
			}
			for j := 0; j < 50; j++ {
				c.List(lang, ListOptions{Page: 1, Limit: 10, SearchMode: "all", WithContent: true})
				if _, ok := c.Detail("with-en", lang); !ok {
					t.Error("Detail(with-en) 应该存在")
					return
				}
				c.Series(lang)
			}
		}(i)
	}
	wg.Wait()

	// 共享存储必须保持原样：语言字段从未被回填到 *Doc 上
	for slug, d := range c.zh {
		if d.Slug != slug {
			t.Errorf("共享文档 %s 的 Slug 被改写为 %q", slug, d.Slug)
		}
	}
}
