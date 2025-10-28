package main

import (
	"log"
	"net/http"
	"runtime"
	"time"

	"duckex-server/internal/handlers"
	"duckex-server/internal/models"
	"duckex-server/internal/utils"

	"github.com/gin-gonic/gin"
)

// 获取系统内存(MB)，简单实现
func getSystemMemoryMB() int64 {
	// 在Windows上使用runtime.GOMAXPROCS的方式获取可能不太准确
	// 这里提供一个简单实现，实际项目可能需要使用系统特定的方法
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	// 返回一个估计值，实际系统可能需要更复杂的实现
	return 4096 // 默认假设4GB内存
}

func main() {
	// 初始化仓库
	itemRepo := models.NewInMemoryItemRepository()
	
	// 初始化内存监控器，默认设置为可用内存的80%
	// 设置最大内存为系统内存的80%，如果无法获取则设置为1GB
	maxMemoryMB := int64(1024) // 默认1GB
	if sysMem := getSystemMemoryMB(); sysMem > 0 {
		maxMemoryMB = int64(float64(sysMem) * 0.8)
	}
	log.Printf("Memory monitor initialized with max memory: %d MB", maxMemoryMB)
	memoryMonitor := utils.NewMemoryMonitor(maxMemoryMB)

	// 初始化处理器
	itemHandler := handlers.NewItemHandler(itemRepo, memoryMonitor)

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
		// 内存状态
		api.GET("/memory", func(c *gin.Context) {
			c.JSON(http.StatusOK, memoryMonitor.GetStatus())
		})
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
	
	// 启动内存监控goroutine
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				memoryMonitor.UpdateStatus()
				status := memoryMonitor.GetStatus()
				if status["share_disabled"].(bool) {
					log.Printf("WARNING: Memory usage high (%.1f%%), share functionality temporarily disabled", 
						status["usage_percentage"].(float64)*100)
				} else {
					log.Printf("Memory usage: %.1f%% of %d MB", 
						status["usage_percentage"].(float64)*100,
						status["max_memory_mb"].(int64))
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
	log.Printf("  GET  http://localhost%s/api/v1/memory - Check memory status", serverAddr)

	if err := r.Run(serverAddr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}