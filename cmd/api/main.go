package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
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
	// 初始化仓库
	itemRepo := models.NewInMemoryItemRepository()

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

	// 初始化审计服务
	auditService := utils.NewAuditService("./audit_log.json")
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

		// 获取审计日志数据（支持分页）
		api.GET("/audit/logs", func(c *gin.Context) {
			// 从请求参数获取分页信息
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

			// 调用分页查询方法
			paginatedLogs := auditService.GetLogsWithPagination(page, pageSize)

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
			csvFile := "./item_statistics.csv"

			// 检查文件是否存在
			if _, err := os.Stat(csvFile); os.IsNotExist(err) {
				c.JSON(http.StatusNotFound, gin.H{
					"status":  "error",
					"message": "Statistics file not found",
				})
				return
			}

			// 打开CSV文件
			file, err := os.Open(csvFile)
			if err != nil {
				log.Printf("Error opening statistics file: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"status":  "error",
					"message": "Failed to open statistics file",
				})
				return
			}
			defer file.Close()

			// 读取CSV数据
			reader := csv.NewReader(file)

			// 跳过表头
			_, err = reader.Read()
			if err != nil {
				log.Printf("Error reading CSV header: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"status":  "error",
					"message": "Failed to read statistics data",
				})
				return
			}

			// 解析数据
			var timestamps []string
			var counts []int

			for {
				record, err := reader.Read()
				if err == io.EOF {
					break
				}
				if err != nil {
					log.Printf("Error reading CSV record: %v", err)
					continue
				}

				if len(record) >= 2 {
					timestamps = append(timestamps, record[0])
					count, err := strconv.Atoi(record[1])
					if err != nil {
						log.Printf("Error parsing count value: %v", err)
						count = 0
					}
					counts = append(counts, count)
				}
			}

			// 只返回最后1000条数据
			if len(timestamps) > 100 {
				timestamps = timestamps[len(timestamps)-100:]
				counts = counts[len(counts)-100:]
			}

			c.JSON(http.StatusOK, gin.H{
				"status":     "ok",
				"timestamps": timestamps,
				"counts":     counts,
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

	// 启动每5分钟统计物品数量并写入CSV的任务
	go func() {
		csvFile := "./item_statistics.csv"
		// 检查文件是否存在，如果不存在则创建并写入表头
		if _, err := os.Stat(csvFile); os.IsNotExist(err) {
			file, err := os.Create(csvFile)
			if err != nil {
				log.Printf("Error creating CSV file: %v", err)
				return
			}
			// 写入CSV表头
			_, err = file.WriteString("timestamp,item_count\n")
			if err != nil {
				log.Printf("Error writing CSV header: %v", err)
			}
			file.Close()
			log.Printf("Created new statistics CSV file: %s", csvFile)
		}

		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		log.Printf("Started item statistics collection, will save to %s every 5 minutes", csvFile)

		for {
			select {
			case <-ticker.C:
				// 获取当前时间和物品总数
				now := models.GetCurrentTime()
				totalItemsCount := itemRepo.GetTotalCount()

				// 打开文件以追加模式
				file, err := os.OpenFile(csvFile, os.O_APPEND|os.O_WRONLY, 0644)
				if err != nil {
					log.Printf("Error opening CSV file for appending: %v", err)
					continue
				}

				// 写入统计数据
				csvLine := fmt.Sprintf("%s,%d\n", now.Format(time.RFC3339), totalItemsCount)
				_, err = file.WriteString(csvLine)
				file.Close()

				if err != nil {
					log.Printf("Error writing to CSV file: %v", err)
				} else {
					log.Printf("Saved item statistics to CSV: timestamp=%s, count=%d", now.Format(time.RFC3339), totalItemsCount)
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

	// 在服务器退出前将所有物品写入CSV
	exportItemsToCSV(itemRepo, "./items_export.csv")
	log.Println("Server exiting")
}

// exportItemsToCSV 将所有物品数据导出到CSV文件
func exportItemsToCSV(itemRepo models.ItemRepository, filePath string) {
	log.Println("Exporting all items to CSV file...")

	// 获取所有物品
	items := itemRepo.GetAll()

	// 创建CSV文件
	file, err := os.Create(filePath)
	if err != nil {
		log.Printf("Error creating CSV file: %v", err)
		return
	}
	defer file.Close()

	// 创建CSV写入器
	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 写入CSV表头
	headers := []string{
		"ID", "Name", "Description", "TypeID", "Num",
		"Durability", "DurabilityLoss", "SharerID", "PickupCode",
		"CreatedAt", "ExpiresAt", "IsClaimed", "ClaimerID",
	}
	if err := writer.Write(headers); err != nil {
		log.Printf("Error writing CSV headers: %v", err)
		return
	}

	// 写入物品数据
	for _, item := range items {
		// 处理可空字段
		var durability, durabilityLoss string
		if item.Durability != nil {
			durability = fmt.Sprintf("%.2f", *item.Durability)
		} else {
			durability = ""
		}
		if item.DurabilityLoss != nil {
			durabilityLoss = fmt.Sprintf("%.2f", *item.DurabilityLoss)
		} else {
			durabilityLoss = ""
		}

		row := []string{
			item.ID,
			item.Name,
			item.Description,
			fmt.Sprintf("%d", item.TypeID),
			fmt.Sprintf("%d", item.Num),
			durability,
			durabilityLoss,
			item.SharerID,
			item.PickupCode,
			item.CreatedAt.Format(time.RFC3339),
			item.ExpiresAt.Format(time.RFC3339),
			fmt.Sprintf("%t", item.IsClaimed),
			item.ClaimerID,
		}

		if err := writer.Write(row); err != nil {
			log.Printf("Error writing item %s to CSV: %v", item.ID, err)
			continue
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		log.Printf("Error flushing CSV writer: %v", err)
		return
	}

	log.Printf("Successfully exported %d items to CSV file: %s", len(items), filePath)
}
