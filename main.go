package main

import (
	"log"
	"moongate-api/internal/api"
	"moongate-api/internal/loader"
	"os"

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
		log.Printf("⚠️ 加载内容失败: %v", err)
	} else {
		log.Printf("✅ 加载了 %d 篇技术文章", len(contentStore.DocsBySlug))
		log.Printf("✅ 加载了 %d 篇关于文章", len(contentStore.AboutBySlug))
	}

	// 3. 创建路由
	docsHandler := api.NewDocsHandler(contentStore.DocsBySlug, contentStore.DocsEn)
	aboutHandler := api.NewAboutHandler(contentStore.AboutBySlug)

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:  []string{"http://localhost:3000", "http://localhost:5173"},
		AllowMethods:  []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:  []string{"Origin", "Content-Type"},
		ExposeHeaders: []string{"Content-Length"},
	}))

	// 4. 内容 API 路由
	r.GET("/api/docs", docsHandler.GetDocs)
	r.GET("/api/docs/:slug", docsHandler.GetDocBySlug)
	r.GET("/api/about", aboutHandler.GetAboutList)
	r.GET("/api/about/:slug", aboutHandler.GetAbout)

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
