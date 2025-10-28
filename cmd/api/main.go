package main

import (
	"log"
	"net/http"
	"time"

	"duckex-server/internal/handlers"
	"duckex-server/internal/models"

	"github.com/gin-gonic/gin"
)

func main() {
	// 初始化仓库
	itemRepo := models.NewInMemoryItemRepository()

	// 初始化处理器
	itemHandler := handlers.NewItemHandler(itemRepo)

	// 设置Gin模式
	gin.SetMode(gin.ReleaseMode)

	// 创建Gin引擎
	r := gin.Default()

	// 添加CORS中间件
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// 健康检查端点
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"message":   "DuckEx Server is quacking!",
			"timestamp": models.GetCurrentTime().Format(time.RFC3339),
		})
	})

	// API路由组
	api := r.Group("/api/v1")
	{
		// 分享物品
		api.POST("/items/share", itemHandler.ShareItem)
		// 领取物品
		api.POST("/items/claim", itemHandler.ClaimItem)
	}

	// 启动定期清理过期物品的goroutine
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := itemRepo.DeleteExpired(); err != nil {
					log.Printf("Error deleting expired items: %v", err)
				}
			}
		}
	}()

	// 启动服务器
	serverAddr := ":8080"
	log.Printf("DuckEx Server starting on %s", serverAddr)
	log.Printf("Health check: http://localhost%s/health", serverAddr)
	log.Printf("API endpoints:")
	log.Printf("  POST http://localhost%s/api/v1/items/share - Share an item", serverAddr)
	log.Printf("  POST http://localhost%s/api/v1/items/claim - Claim an item", serverAddr)

	if err := r.Run(serverAddr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}