package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"duckex-server/internal/database"
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

const (
	// 默认PID文件路径
	defaultPIDFile = "./duckex-server.pid"
)

// 保存PID到文件
func savePID(pidFile string) error {
	pid := os.Getpid()
	file, err := os.Create(pidFile)
	if err != nil {
		return fmt.Errorf("failed to create PID file: %w", err)
	}
	defer file.Close()

	_, err = fmt.Fprintf(file, "%d", pid)
	if err != nil {
		return fmt.Errorf("failed to write PID to file: %w", err)
	}

	log.Printf("PID %d saved to %s", pid, pidFile)
	return nil
}

// 删除PID文件
func removePID(pidFile string) {
	if err := os.Remove(pidFile); err != nil {
		log.Printf("Warning: failed to remove PID file %s: %v", pidFile, err)
	} else {
		log.Printf("PID file %s removed", pidFile)
	}
}

func main() {
	// 初始化SQLite数据库
	dbPath := "./duckex.db"
	if err := database.InitSQLite(dbPath); err != nil {
		log.Fatalf("Failed to initialize SQLite database: %v", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.Printf("Error closing database: %v", err)
		}
	}()

	// 初始化仓库（使用SQLite实现）
	itemRepo := models.NewSQLiteItemRepository()
	
	// 从JSON备份文件迁移数据到SQLite
	jsonBackupPath := "./items_backup.json"
	log.Printf("Attempting to migrate data from JSON backup at %s", jsonBackupPath)
	if err := itemRepo.MigrateFromJSON(jsonBackupPath); err != nil {
		log.Printf("Warning: JSON migration failed: %v, continuing without migration", err)
	} else {
		log.Printf("JSON migration completed successfully")
	}

	// 保存PID文件
	pidFile := defaultPIDFile
	if err := savePID(pidFile); err != nil {
		log.Fatalf("Failed to save PID file: %v", err)
	}

	// 确保程序退出时保存数据并删除PID文件
	defer func() {
		// 先删除PID文件
		removePID(pidFile)

		// 再关闭仓库
		if err := itemRepo.Shutdown(); err != nil {
			log.Printf("Error during repository shutdown: %v", err)
		} else {
			log.Println("Repository shutdown completed successfully")
		}
	}()

	// 后台运行模式 - 关闭标准输入输出
	// 重定向标准输出和标准错误到日志文件
	logFile, err := os.OpenFile("./duckex-server.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Warning: failed to open log file: %v, continuing with default logging", err)
	} else {
		defer logFile.Close()
		log.SetOutput(logFile)

		// 关闭标准输入
		os.Stdin.Close()
		// 重定向标准输出和标准错误到日志文件
		os.Stdout = logFile
		os.Stderr = logFile
	}

	log.Println("DuckEx Server started in background mode")

	// 初始化内存监控器，默认设置为可用内存的80%
	// 设置最大内存为系统内存的80%，如果无法获取则设置为1GB
	maxMemoryMB := int64(1024) // 默认1GB
	if sysMem := getSystemMemoryMB(); sysMem > 0 {
		maxMemoryMB = int64(float64(sysMem) * 0.8)
	}
	log.Printf("Memory monitor initialized with max memory: %d MB", maxMemoryMB)
	memoryMonitor := utils.NewMemoryMonitor(maxMemoryMB)

	// 初始化审计服务（使用SQLite实现）
	auditService := utils.NewSQLiteAuditService()
	log.Println("Audit service initialized with log file: ./audit_log.json")

	// 初始化处理器
	itemHandler := handlers.NewItemHandler(itemRepo, memoryMonitor, auditService)

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

	// 审计查看器页面
	r.GET("/audit", func(c *gin.Context) {
		c.File("static/audit_viewer.html")
	})

	// 健康检查端点
	r.GET("/health", func(c *gin.Context) {
		// 获取当前时间和1小时前的时间
		now := models.GetCurrentTime()
		hourAgo := now.Add(-1 * time.Hour)

		// 获取物品总数（包括已过期和已领取的）
		totalItemsCount := itemRepo.GetTotalCount()

		// 获取1小时内处理的物品数量（分享和领取）
		hourlyProcessedCount := itemRepo.GetProcessedCountInTimeRange(hourAgo, now)

		// 获取内存状态
		memoryStatus := memoryMonitor.GetStatus()
		memoryUsageMB := memoryStatus["current_usage_mb"].(int64)
		memoryUsagePercent := memoryStatus["usage_percentage"].(float64) * 100

		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"message":   "DuckEx Server is quacking!",
			"timestamp": now.Format(time.RFC3339),
			"statistics": gin.H{
				"total_items":            totalItemsCount,
				"hourly_processed_items": hourlyProcessedCount,
				"memory_usage": gin.H{
					"current_mb":     memoryUsageMB,
					"percentage":     fmt.Sprintf("%.1f%%", memoryUsagePercent),
					"max_allowed_mb": memoryStatus["max_memory_mb"].(int64),
				},
			},
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

		// 获取审计日志数据（支持分页和过滤）
		api.GET("/audit/logs", func(c *gin.Context) {
			// 从请求参数获取分页信息
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

			// 从请求参数获取过滤信息
			filters := make(map[string]string)
			filters["action"] = c.Query("action")
			filters["level"] = c.Query("level")
			filters["user_id"] = c.Query("user_id")
			filters["pickup_code"] = c.Query("pickup_code")
			filters["time_range"] = c.Query("time_range")

			// 调用分页查询方法（带过滤条件）
			paginatedLogs := auditService.GetLogsWithPagination(page, pageSize, filters)

			// 返回分页响应
			c.JSON(http.StatusOK, gin.H{
				"status":      "success",
				"total":       paginatedLogs.Total,
				"page":        paginatedLogs.Page,
				"page_size":   paginatedLogs.PageSize,
				"total_pages": paginatedLogs.TotalPages,
				"logs":        paginatedLogs.Logs,
			})
		})

		// 获取物品数量统计数据（用于折线图）
		api.GET("/statistics/items", func(c *gin.Context) {
			// 从数据库获取统计数据
			rows, err := database.DB.Query(
				`SELECT timestamp, item_count, claimed_count, unclaimed_count 
				 FROM item_statistics 
				 ORDER BY timestamp DESC 
				 LIMIT 100`,
			)
			if err != nil {
				log.Printf("Error querying statistics from database: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"status":  "error",
					"message": "Failed to query statistics data",
				})
				return
			}
			defer rows.Close()

			// 解析数据
			var timestamps []string
			var counts []int
			var claimedCounts []int
			var unclaimedCounts []int

			// 读取所有行到临时切片，以便后续反转顺序
			type statRecord struct {
				timestamp    string
				count        int
				claimedCount int
				unclaimedCount int
			}
			var records []statRecord

			for rows.Next() {
				var record statRecord
				err := rows.Scan(&record.timestamp, &record.count, &record.claimedCount, &record.unclaimedCount)
				if err != nil {
					log.Printf("Error scanning statistics record: %v", err)
					continue
				}
				records = append(records, record)
			}

			if err = rows.Err(); err != nil {
				log.Printf("Error iterating statistics rows: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"status":  "error",
					"message": "Failed to process statistics data",
				})
				return
			}

			// 反转记录顺序，使时间戳按升序排列
			for i := len(records) - 1; i >= 0; i-- {
				timestamps = append(timestamps, records[i].timestamp)
				counts = append(counts, records[i].count)
				claimedCounts = append(claimedCounts, records[i].claimedCount)
				unclaimedCounts = append(unclaimedCounts, records[i].unclaimedCount)
			}

			c.JSON(http.StatusOK, gin.H{
				"status":         "ok",
				"timestamps":     timestamps,
				"counts":         counts,
				"claimed_counts": claimedCounts,
				"unclaimed_counts": unclaimedCounts,
			})
		})
	}

	// 启动定期清理任务（作为额外保障，主要清理仍可能存在的过期物品）
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				log.Printf("Running scheduled cleanup task")
				if err := itemRepo.DeleteExpired(); err != nil {
					log.Printf("Error during scheduled cleanup: %v", err)
				}
			}
		}
	}()

	// 启动每5分钟统计物品数量并写入数据库的任务
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		log.Printf("Started item statistics collection, will save to database every 5 minutes")

		for {
			select {
			case <-ticker.C:
				// 获取当前时间和物品统计信息
				now := models.GetCurrentTime()
				totalItemsCount := itemRepo.GetTotalCount()
				claimedCount := itemRepo.GetClaimedCount()
				unclaimedCount := totalItemsCount - claimedCount

				// 保存统计数据到数据库
				_, err := database.DB.Exec(
					`INSERT INTO item_statistics (timestamp, item_count, claimed_count, unclaimed_count) 
					 VALUES (?, ?, ?, ?)`,
					now.Format(time.RFC3339), totalItemsCount, claimedCount, unclaimedCount,
				)

				if err != nil {
					log.Printf("Error saving statistics to database: %v", err)
				} else {
					log.Printf("Saved item statistics to database: timestamp=%s, total=%d, claimed=%d, unclaimed=%d", 
						now.Format(time.RFC3339), totalItemsCount, claimedCount, unclaimedCount)
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

	// 配置静态文件服务
	r.Static("/static", "./static")

	// 添加根路径路由，直接返回统计图表HTML内容
	r.GET("/", func(c *gin.Context) {
		// 获取健康检查数据
		now := models.GetCurrentTime()
		hourAgo := now.Add(-1 * time.Hour)
		totalItemsCount := itemRepo.GetTotalCount()
		hourlyProcessedCount := itemRepo.GetProcessedCountInTimeRange(hourAgo, now)
		memoryStatus := memoryMonitor.GetStatus()
		memoryUsageMB := memoryStatus["current_usage_mb"].(int64)
		memoryUsagePercent := memoryStatus["usage_percentage"].(float64) * 100
		maxMemoryMB := memoryStatus["max_memory_mb"].(int64)

		// 读取HTML文件内容
		htmlContent, err := os.ReadFile("./static/statistics_chart.html")
		if err != nil {
			log.Printf("Error reading HTML file: %v", err)
			c.String(http.StatusInternalServerError, "Error loading page")
			return
		}

		// 在HTML头部添加健康数据脚本
		healthDataScript := fmt.Sprintf(`
		<script>
			// 健康检查数据
			window.healthData = {
				status: "ok",
				message: "DuckEx Server is quacking!",
				timestamp: "%s",
				statistics: {
					total_items: %d,
					hourly_processed_items: %d,
					memory_usage: {
						current_mb: %d,
						percentage: "%.1f%%",
						max_allowed_mb: %d
					}
				}
			};
		</script>
		`, now.Format(time.RFC3339), totalItemsCount, hourlyProcessedCount, memoryUsageMB, memoryUsagePercent, maxMemoryMB)

		// 在head标签后插入健康数据脚本
		htmlStr := string(htmlContent)
		headEndIndex := strings.Index(htmlStr, "</head>")
		if headEndIndex > 0 {
			htmlStr = htmlStr[:headEndIndex] + healthDataScript + htmlStr[headEndIndex:]
		}

		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, htmlStr)
	})

	// 保留原有统计图表页面路由，重定向到根路径
	r.GET("/statistics/chart", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/")
	})

	// 启动服务器
	serverAddr := ":8080"
	log.Printf("DuckEx Server starting on %s", serverAddr)
	log.Printf("Health check: http://localhost%s/health", serverAddr)
	log.Printf("Statistics chart: http://localhost%s/statistics/chart", serverAddr)
	log.Printf("API endpoints:")
	log.Printf("  POST http://localhost%s/api/v1/items/share - Share an item", serverAddr)
	log.Printf("  POST http://localhost%s/api/v1/items/claim - Claim an item", serverAddr)
	log.Printf("  GET  http://localhost%s/api/v1/memory - Check memory status", serverAddr)
	log.Printf("  GET  http://localhost%s/api/v1/statistics/items - Get item statistics data", serverAddr)
	log.Printf("Data will be saved every 5 minutes and on graceful shutdown")

	// 创建HTTP服务器
	srv := &http.Server{
		Addr:    serverAddr,
		Handler: r,
	}

	// 启动服务器（非阻塞）
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// 等待中断信号以优雅地关闭服务器
	quit := make(chan os.Signal, 1)

	// 支持Windows和Unix/Linux的终止信号
	// kill (no param) default send syscall.SIGTERM
	// kill -2 is syscall.SIGINT
	signalsToHandle := []os.Signal{syscall.SIGINT}

	// 在不同操作系统上添加适当的终止信号
	// Windows上可能不支持SIGTERM，但Go的signal包会尝试处理
	signalsToHandle = append(signalsToHandle, syscall.SIGTERM)

	// 在Windows上，添加Windows特定的信号处理
	if runtime.GOOS == "windows" {
		log.Println("Windows detected, registering appropriate signal handlers")
		// 在Windows上，kill命令通常通过任务管理器或taskkill发送终止信号
		// Go的signal包会将这些信号映射到SIGINT或SIGTERM
	} else {
		log.Println("Unix/Linux detected, registering standard signal handlers")
	}

	// 注册信号处理
	signal.Notify(quit, signalsToHandle...)
	log.Println("Signal handlers registered for graceful shutdown")
	log.Println("Note: On Windows, use taskkill /pid <pid> /f to force kill or omit /f for graceful shutdown")
	<-quit
	log.Println("Shutting down server...")

	// 设置5秒的超时时间来关闭服务器
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exiting")
}
