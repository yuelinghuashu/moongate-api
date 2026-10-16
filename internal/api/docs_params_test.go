package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestGetDocs_QueryParamBoundaries 覆盖分页参数的解析与边界保护：
// 非法/越界输入必须回落到默认值，而不是返回 500 或空页。
func TestGetDocs_QueryParamBoundaries(t *testing.T) {
	r := setupDocsTestRouter()

	tests := []struct {
		name      string
		path      string
		wantPage  int
		wantLimit int
	}{
		{"缺省", "/api/docs", 1, 10},
		{"显式合法值", "/api/docs?page=2&limit=1", 2, 1},
		{"page=0 回落默认", "/api/docs?page=0", 1, 10},
		{"page 负数回落默认", "/api/docs?page=-3", 1, 10},
		{"limit=0 回落默认", "/api/docs?limit=0", 1, 10},
		{"limit 非数字回落默认", "/api/docs?limit=abc", 1, 10},
		{"page 非数字回落默认", "/api/docs?page=x", 1, 10},
		{"越界页码返回空但保留总数", "/api/docs?page=99&limit=2", 99, 2},
		// 上限保护：limit 超过 maxLimit 时钳制并回显真实生效值。
		// 这是服务端不变量（?content=true 时单个响应会带整篇正文），
		// 客户端按信封里的 limit/totalPages 翻页即可，不需要自己猜上限。
		{"limit 恰好等于上限", "/api/docs?limit=100", 1, 100},
		{"limit 超过上限被钳制", "/api/docs?limit=101", 1, 100},
		{"limit 远超上限被钳制", "/api/docs?limit=100000", 1, 100},
		{"limit 超大且带正文仍被钳制", "/api/docs?limit=99999&content=true", 1, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := performRequest(r, "GET", tt.path)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}

			var resp struct {
				Data       []map[string]any `json:"data"`
				Total      int              `json:"total"`
				Page       int              `json:"page"`
				Limit      int              `json:"limit"`
				TotalPages int              `json:"totalPages"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("反序列化响应失败: %v", err)
			}

			if resp.Page != tt.wantPage {
				t.Errorf("page = %d, want %d", resp.Page, tt.wantPage)
			}
			if resp.Limit != tt.wantLimit {
				t.Errorf("limit = %d, want %d", resp.Limit, tt.wantLimit)
			}
			if resp.Total != 3 {
				t.Errorf("total = %d, want 3（总数不受分页参数影响）", resp.Total)
			}
			if resp.Data == nil {
				t.Error("data 应为数组而非 null（前端依赖 .data.length）")
			}
		})
	}
}
