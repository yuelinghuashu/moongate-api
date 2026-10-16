package main

import (
	"log"
	"moongate-api/internal/api"
	"moongate-api/internal/catalog"
	"moongate-api/internal/loader"
	"os"
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

	// CORS 只为本地开发存在：线上前端与 API 同源（NUXT_PUBLIC_API_URL=https://moongate.top，
	// Caddy 把 /api 反代到本服务），浏览器请求不跨源，因此生产无需放行任何 Origin。
	// 需要窄放行时用 CORS_ORIGINS 指定（逗号分隔），默认只给本地开发端口。
	r.Use(cors.New(cors.Config{
		AllowOrigins:  corsOrigins(),
		AllowMethods:  []string{"GET", "OPTIONS"}, // 内容 API 只读
		AllowHeaders:  []string{"Origin", "Content-Type"},
		ExposeHeaders: []string{"Content-Length"},
	}))

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

// corsOrigins 解析允许的跨源来源：CORS_ORIGINS="http://a,http://b"（逗号分隔，忽略空白）。
// 未设置时回落到本地开发默认值——生产同源，不需要放行。
func corsOrigins() []string {
	const devDefaults = "http://localhost:3000"

	raw := os.Getenv("CORS_ORIGINS")
	if raw == "" {
		raw = devDefaults
	}

	origins := make([]string, 0, 2)
	for _, part := range strings.Split(raw, ",") {
		if origin := strings.TrimSpace(part); origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}
