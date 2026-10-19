package main

import (
	"testing"

	"github.com/gin-contrib/cors"
)

// TestDevOriginPattern 锁定"本地开发来源"的匹配形态。
//
// 这张表就是线上行为的最小规格：改正则之前先看这里哪条会红。
func TestDevOriginPattern(t *testing.T) {
	cases := []struct {
		origin string
		want   bool
		note   string
	}{
		{"http://localhost:3000", true, "默认开发端口"},
		{"http://localhost:3001", true, "端口被占用时 Nuxt 顺延"},
		{"http://127.0.0.1:3000", true, "localhost 与 127.0.0.1 是两个 Origin"},
		{"http://127.0.0.1:5173", true, "其它本地端口"},
		{"http://[::1]:3000", true, "IPv6 回环"},
		{"http://evil.example.com", false, "外部域名"},
		{"null", false, "沙箱 iframe / file:// 页面的 Origin"},
		{"http://localhost:3000.evil.com", false, "后缀混淆"},
		{"https://localhost:3000", false, "本地开发走 http，https 不在此列"},
		{"http://localhost", false, "缺少端口"},
	}

	for _, tc := range cases {
		if got := devOriginPattern.MatchString(tc.origin); got != tc.want {
			t.Errorf("devOriginPattern(%q) = %v, want %v（%s）", tc.origin, got, tc.want, tc.note)
		}
	}
}

// TestCorsConfig_DevEnvAllowsLoopback 本地开发环境：放行回环来源。
func TestCorsConfig_DevEnvAllowsLoopback(t *testing.T) {
	t.Setenv("ENV", "development")
	t.Setenv("CORS_ORIGINS", "")

	cfg := corsConfig()
	if cfg.AllowOriginFunc == nil {
		t.Fatal("开发环境应设置 AllowOriginFunc（否则跨源全拒，本地前端会拿到 403）")
	}
	for _, origin := range []string{"http://localhost:3000", "http://127.0.0.1:3001", "http://[::1]:3000"} {
		if !cfg.AllowOriginFunc(origin) {
			t.Errorf("开发环境应放行 %q", origin)
		}
	}
	for _, origin := range []string{"http://evil.example.com", "null"} {
		if cfg.AllowOriginFunc(origin) {
			t.Errorf("开发环境不应放行 %q", origin)
		}
	}
}

// TestCorsConfig_NonDevEnvDeniesLoopback 是这次修复的核心护栏：
// 生产（或任何没有显式声明 ENV=development 的环境）一律不放行本地来源。
//
// 之前的实现只看 CORS_ORIGINS 是否为空，导致生产也会放行任意
// http://localhost:<port>、http://127.0.0.1、http://[::1] —— 与
// "生产一点不放宽"的说法矛盾，也是将来引入鉴权后的后门。
func TestCorsConfig_NonDevEnvDeniesLoopback(t *testing.T) {
	for _, env := range []string{"production", ""} {
		t.Run("ENV="+env, func(t *testing.T) {
			t.Setenv("ENV", env)
			t.Setenv("CORS_ORIGINS", "")

			cfg := corsConfig()
			if cfg.AllowOriginFunc == nil {
				t.Fatal("非开发环境也必须设置 AllowOriginFunc：留 nil 会让 cors.New 直接 panic（all origins disabled）")
			}
			for _, origin := range []string{"http://localhost:3000", "http://127.0.0.1:3000", "http://[::1]:3000"} {
				if cfg.AllowOriginFunc(origin) {
					t.Errorf("非开发环境（ENV=%q）不应放行 %q", env, origin)
				}
			}
		})
	}
}

// TestCorsConfig_ExplicitOriginsWin 显式 CORS_ORIGINS 始终优先，与环境无关。
func TestCorsConfig_ExplicitOriginsWin(t *testing.T) {
	t.Setenv("ENV", "production")
	t.Setenv("CORS_ORIGINS", " http://a.example , http://b.example ,")

	cfg := corsConfig()
	if cfg.AllowOriginFunc != nil {
		t.Error("显式白名单下不应再设置 AllowOriginFunc（两者互斥）")
	}
	want := []string{"http://a.example", "http://b.example"}
	if len(cfg.AllowOrigins) != len(want) {
		t.Fatalf("AllowOrigins = %#v, want %#v", cfg.AllowOrigins, want)
	}
	for i := range want {
		if cfg.AllowOrigins[i] != want[i] {
			t.Errorf("AllowOrigins[%d] = %q, want %q", i, cfg.AllowOrigins[i], want[i])
		}
	}
}

// TestCorsNewNeverPanics 覆盖 gin-contrib/cors 最要命的失败模式：
// 三个 origin 配置全空时 Config.Validate() 报错、newCors 直接 panic。
// 任何"关掉跨源"的写法都必须先过这一关。
func TestCorsNewNeverPanics(t *testing.T) {
	cases := []struct{ name, env, origins string }{
		{"开发环境", "development", ""},
		{"生产环境", "production", ""},
		{"未声明环境", "", ""},
		{"显式白名单", "production", "http://a.example"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ENV", tc.env)
			t.Setenv("CORS_ORIGINS", tc.origins)

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("cors.New(corsConfig()) panic: %v", r)
				}
			}()
			if handler := cors.New(corsConfig()); handler == nil {
				t.Fatal("cors.New 返回了 nil handler")
			}
		})
	}
}
