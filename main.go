package main

import (
	"log"
	"moongate-api/internal/api"
	"moongate-api/internal/catalog"
	"moongate-api/internal/loader"
	"os"
	"regexp"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// 1. 加载 .env
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️ 未找到 .env 文件，使用系统环境变量")
	}

	// 2. 加载内容
	contentStore, err := loader.LoadAll("./content")
	if err != nil {
		log.Fatalf("❌ 加载内容失败: %v", err)
	}
	log.Printf("✅ 加载了 %d 篇技术文章", len(contentStore.DocsBySlug))

	// 3. 创建路由
	contentCatalog := catalog.New(contentStore.DocsBySlug, contentStore.DocsEn)
	docsHandler := api.NewDocsHandler(contentCatalog)

	r := gin.Default()

	r.Use(cors.New(corsConfig()))

	// CORS 一旦配错，症状是"客户端跳转没数据、刷新才有"（刷新走 SSR，不经过 CORS），
	// 极难发现。所以在"跨源全拒"的配置下启动时明确喊一声。
	if len(splitList(os.Getenv("CORS_ORIGINS"))) == 0 && !isDevEnv() {
		log.Println("⚠️ CORS: 未配置 CORS_ORIGINS 且 ENV≠development —— 所有跨源请求都会被拒绝（同源部署无影响）")
	}

	// 4. 内容 API 路由
	r.GET("/api/docs", docsHandler.GetDocs)
	r.GET("/api/docs/:slug", docsHandler.GetDocBySlug)

	// 5. 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// 6. 启动
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("🚀 服务器启动在 http://localhost:%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal("❌ 服务器启动失败:", err)
	}
}

// devOriginPattern 匹配本地开发前端的全部常见打开方式：
// localhost / 127.0.0.1 / [::1] 的任意端口。
//
// 不能只钉死某个 origin：Nuxt dev 在 3000 被占用时会顺延到 3001、3002…，
// 而浏览器用 127.0.0.1 打开与用 localhost 打开是两个不同的 Origin。
// 放行不全会让浏览器侧请求被 403 挡掉，但刷新走的是 SSR（服务端取数，不带 Origin），
// 现象就是"点链接看不到内容、刷新一次才有"。
var devOriginPattern = regexp.MustCompile(`^http://(localhost|127\.0\.0\.1|\[::1\]):[0-9]{1,5}$`)

// isDevEnv 判断是否本地开发环境（由 .env / 环境变量 ENV=development 声明）。
//
// 刻意不用 gin.Mode()：gin.Default() 默认就是 debug 模式，生产若没有显式设置
// GIN_MODE=release，就会被误判成开发环境 —— 等于把"放行本地来源"的规则带到线上。
func isDevEnv() bool {
	return os.Getenv("ENV") == "development"
}

// corsConfig 构造 CORS 配置。
//
// CORS 只为本地开发存在：线上前端与 API 同源（NUXT_PUBLIC_API_URL=https://moongate.top，
// Caddy 把 /api 反代到本服务），浏览器请求不跨源，因此生产无需放行任何 Origin。
// 需要窄放行时用 CORS_ORIGINS 显式指定（逗号分隔），例如手机真机调试要放行局域网 IP。
func corsConfig() cors.Config {
	cfg := cors.Config{
		AllowMethods: []string{"GET", "OPTIONS"}, // 内容 API 只读
		AllowHeaders: []string{"Content-Type"},
	}

	// 显式白名单优先。AllowOrigins 与 AllowOriginFunc 互斥（见 cors.Config.Validate）。
	if origins := splitList(os.Getenv("CORS_ORIGINS")); len(origins) > 0 {
		cfg.AllowOrigins = origins
		return cfg
	}

	// 关键：AllowOriginFunc 永远不能留 nil。
	// gin-contrib/cors 在「没有 AllowOrigins 且没有 AllowOriginFunc」时会 Validate 失败，
	// 并在 newCors 里 panic（"conflict settings: all origins disabled"），
	// 进程直接起不来。所以"关闭跨源"要写成"恒返回 false 的函数"，而不是"不设置"。
	//
	// 同时这里也是环境隔离点：只有 ENV=development 才启用本地规则，
	// 否则生产（或任何没有显式声明开发环境的地方）一律拒绝 —— 不依赖 GIN_MODE，
	// 也不依赖"忘了配 CORS_ORIGINS"这类隐含前提。
	cfg.AllowOriginFunc = func(origin string) bool {
		if !isDevEnv() {
			return false
		}
		return devOriginPattern.MatchString(origin)
	}
	return cfg
}

// splitList 解析逗号分隔的列表，去掉空白与空项。
func splitList(raw string) []string {
	parts := strings.Split(raw, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			items = append(items, item)
		}
	}
	return items
}
