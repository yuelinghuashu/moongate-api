package main

import (
	"log"
	"moongate-api/db"
	"moongate-api/internal/api"
	"moongate-api/internal/loader"
	"moongate-api/models"
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

	// 2. 检查环境变量
	githubKey := os.Getenv("GITHUB_KEY")
	if githubKey == "" {
		log.Fatal("❌ GITHUB_KEY 环境变量未设置")
	}
	githubSecret := os.Getenv("GITHUB_SECRET")
	if githubSecret == "" {
		log.Fatal("❌ GITHUB_SECRET 环境变量未设置")
	}
	githubCallback := os.Getenv("GITHUB_CALLBACK_URL")
	if githubCallback == "" {
		log.Fatal("❌ GITHUB_CALLBACK_URL 环境变量未设置")
	}

	log.Printf("🔍 GITHUB_KEY: %s", githubKey[:8]+"...")
	log.Printf("🔍 GITHUB_CALLBACK_URL: %s", githubCallback)

	// 3. 初始化数据库
	db.InitDB()

	// 4. 自动迁移
	if err := db.DB.AutoMigrate(&models.Users{}); err != nil {
		log.Fatal("❌ 数据库迁移失败:", err)
	}
	log.Println("✅ 数据库迁移完成")

	// 5. 加载内容
	contentStore, err := loader.LoadAll("./content")
	if err != nil {
		log.Printf("⚠️ 加载内容失败: %v", err)
	} else {
		log.Printf("✅ 加载了 %d 篇技术文章", len(contentStore.Docs))
		log.Printf("✅ 加载了 %d 篇关于文章", len(contentStore.About))
	}

	// 6. 创建路由
	docsHandler := api.NewDocsHandler(contentStore.Docs, contentStore.DocsBySlug)
	aboutHandler := api.NewAboutHandler(contentStore.About, contentStore.AboutBySlug)

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000", "http://localhost:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	// 7. 公开路由（内容 API）
	r.GET("/api/docs", docsHandler.GetDocs)
	r.GET("/api/docs/:slug", docsHandler.GetDocBySlug)
	r.GET("/api/about", aboutHandler.GetAboutList)
	r.GET("/api/about/:slug", aboutHandler.GetAbout)

	// 8. GitHub OAuth 路由
	r.GET("/auth/github", api.GitHubLogin)
	r.GET("/auth/github/callback", api.GitHubCallback)

	// 9. 用户路由
	user := r.Group("/api/users")
	{
		// 受保护：需要登录
		protected := user.Group("/")
		protected.Use(api.AuthMiddleware())
		{
			protected.GET("/me", api.GetCurrentUser)
		}
	}

	// 10. 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// 11. 启动
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("🚀 服务器启动在 http://localhost:%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal("❌ 服务器启动失败:", err)
	}
}
