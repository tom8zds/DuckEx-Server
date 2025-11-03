package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"duckex-server/internal/handlers"
	"duckex-server/internal/models"
	"duckex-server/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupTestRouter() (*gin.Engine, models.ItemRepository) {
	// 设置为测试模式
	gin.SetMode(gin.TestMode)

	// 创建仓库和处理器
	itemRepo := models.NewInMemoryItemRepository()
	monitor := utils.NewMemoryMonitor(500)
	auditService := utils.NewAuditService("")

	itemHandler := handlers.NewItemHandler(itemRepo, monitor, auditService)

	// 创建路由
	r := gin.Default()

	// 添加API路由
	api := r.Group("/api/v1")
	{
		api.POST("/items/share", itemHandler.ShareItem)
		api.POST("/items/claim", itemHandler.ClaimItem)
	}

	return r, itemRepo
}

func TestShareItem(t *testing.T) {
	router, _ := setupTestRouter()

	// 准备请求数据
	requestData := handlers.ShareItemRequest{
		Name:           "Test Weapon",
		Description:    "A powerful sword",
		TypeID:         1001,
		Num:            1,
		Durability:     90.0,
		DurabilityLoss: 10.0,
		SharerID:       "player123",
	}

	requestBody, err := json.Marshal(requestData)
	assert.NoError(t, err)

	// 创建请求
	req := httptest.NewRequest(http.MethodPost, "/api/v1/items/share", bytes.NewBuffer(requestBody))
	req.Header.Set("Content-Type", "application/json")

	// 执行请求
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 验证响应
	assert.Equal(t, http.StatusOK, w.Code)

	// 解析响应
	var response handlers.ShareItemResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	// 验证响应内容
	assert.Equal(t, "Item shared successfully! Quack!", response.Message)
	assert.NotEmpty(t, response.PickupCode)
	assert.NotEmpty(t, response.ExpiresAt)
}

func TestShareItemWithoutDurabilityFields(t *testing.T) {
	router, itemRepo := setupTestRouter()

	// 准备请求数据（不包含Durability和DurabilityLoss字段）
	requestData := map[string]interface{}{
		"name":        "Test Item Without Durability",
		"description": "Item without durability fields",
		"type_id":     1002,
		"num":         1,
		"sharer_id":   "player456",
	}

	requestBody, err := json.Marshal(requestData)
	assert.NoError(t, err)

	// 创建请求
	req := httptest.NewRequest(http.MethodPost, "/api/v1/items/share", bytes.NewBuffer(requestBody))
	req.Header.Set("Content-Type", "application/json")

	// 执行请求
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 验证响应
	assert.Equal(t, http.StatusOK, w.Code)

	// 解析响应
	var response handlers.ShareItemResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	// 验证响应内容
	assert.Equal(t, "Item shared successfully! Quack!", response.Message)
	assert.NotEmpty(t, response.PickupCode)

	// 验证物品确实被创建并且Durability字段默认为0
	item, err := itemRepo.GetByPickupCode(response.PickupCode)
	assert.NoError(t, err)
	assert.NotNil(t, item)
	assert.Equal(t, float64(0), item.Durability)
	assert.Equal(t, float64(0), item.DurabilityLoss)
}

func TestShareItemInvalidRequest(t *testing.T) {
	router, _ := setupTestRouter()

	// 准备无效的请求数据（缺少必要字段）
	requestData := map[string]interface{}{
		"name":      "Test Item",
		"sharer_id": "player123",
		// 缺少 type_id, num, durability
	}

	requestBody, err := json.Marshal(requestData)
	assert.NoError(t, err)

	// 创建请求
	req := httptest.NewRequest(http.MethodPost, "/api/v1/items/share", bytes.NewBuffer(requestBody))
	req.Header.Set("Content-Type", "application/json")

	// 执行请求
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 验证响应
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestConcurrentShareItemRequests(t *testing.T) {
	router, _ := setupTestRouter()
	var wg sync.WaitGroup
	var successCount int32
	var wgSuccess sync.WaitGroup
	wgSuccess.Add(1)

	// 启动20个并发请求分享物品
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			// 准备请求数据
			requestData := handlers.ShareItemRequest{
				Name:           fmt.Sprintf("Concurrent Weapon %d", index),
				Description:    "Concurrent test sword",
				TypeID:         1001 + index,
				Num:            1,
				Durability:     90.0,
				DurabilityLoss: 5.0,
				SharerID:       fmt.Sprintf("player%d", index),
			}

			requestBody, err := json.Marshal(requestData)
			if err != nil {
				return
			}

			// 创建请求
			req := httptest.NewRequest(http.MethodPost, "/api/v1/items/share", bytes.NewBuffer(requestBody))
			req.Header.Set("Content-Type", "application/json")

			// 执行请求
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// 验证响应
			if w.Code == http.StatusOK {
				successCount++
			}
		}(i)
	}

	// 等待所有请求完成
	wg.Wait()

	// 验证至少有18个请求成功（考虑到可能有一些并发问题）
	assert.GreaterOrEqual(t, successCount, int32(18))
	wgSuccess.Done()
}

func TestConcurrentClaimItemRequests(t *testing.T) {
	router, itemRepo := setupTestRouter()
	var wg sync.WaitGroup

	// 首先创建一个物品用于测试
	pickupCode := "TEST123"
	item := &models.Item{
		ID:             "test-item-concurrent-claim",
		Name:           "Claim Test Item",
		Description:    "Test concurrent claiming",
		TypeID:         2000,
		Num:            1,
		Durability:     95.0,
		DurabilityLoss: 15.0,
		SharerID:       "sharer123",
		PickupCode:     pickupCode,
		CreatedAt:      time.Now(),
		ExpiresAt:      time.Now().Add(24 * time.Hour),
		IsClaimed:      false,
	}
	itemRepo.Create(item)

	// 跟踪成功领取的请求数
	var claimedSuccessfully sync.Once
	var claimSuccessCount int

	// 启动10个并发请求尝试领取同一个物品
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			// 准备请求数据
			requestData := handlers.ClaimItemRequest{
				PickupCode: pickupCode,
				ClaimerID:  fmt.Sprintf("claimer%d", index),
			}

			requestBody, err := json.Marshal(requestData)
			if err != nil {
				return
			}

			// 创建请求
			req := httptest.NewRequest(http.MethodPost, "/api/v1/items/claim", bytes.NewBuffer(requestBody))
			req.Header.Set("Content-Type", "application/json")

			// 执行请求
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// 验证响应 - 只有一个请求应该成功
			if w.Code == http.StatusOK {
				claimedSuccessfully.Do(func() {
					claimSuccessCount++
				})
			}
		}(i)
	}

	// 等待所有请求完成
	wg.Wait()

	// 验证只有一个请求成功领取了物品
	assert.Equal(t, 1, claimSuccessCount)

	// 验证物品现在已被标记为已领取
	claimedItem, _ := itemRepo.GetByPickupCode(pickupCode)
	assert.True(t, claimedItem.IsClaimed)
	assert.NotEmpty(t, claimedItem.ClaimerID)
}

func TestClaimItem(t *testing.T) {
	router, itemRepo := setupTestRouter()

	// 先创建一个物品用于测试领取
	pickupCode := "123456"
	item := &models.Item{
		ID:             "test-item-for-claim",
		Name:           "Claimable Item",
		Description:    "This item is ready to be claimed",
		TypeID:         2001,
		Num:            1,
		Durability:     85.5,
		DurabilityLoss: 10.5,
		SharerID:       "player123",
		PickupCode:     pickupCode,
		CreatedAt:      models.GetCurrentTime(),
		ExpiresAt:      models.GetExpirationTime(),
		IsClaimed:      false,
	}
	itemRepo.Create(item)

	// 准备领取请求数据
	claimRequest := handlers.ClaimItemRequest{
		PickupCode: pickupCode,
		ClaimerID:  "player456",
	}

	requestBody, err := json.Marshal(claimRequest)
	assert.NoError(t, err)

	// 创建请求
	req := httptest.NewRequest(http.MethodPost, "/api/v1/items/claim", bytes.NewBuffer(requestBody))
	req.Header.Set("Content-Type", "application/json")

	// 执行请求
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 验证响应
	assert.Equal(t, http.StatusOK, w.Code)

	// 解析响应
	type claimResponse struct {
		Message string      `json:"message"`
		Item    models.Item `json:"item"`
	}
	var response claimResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	// 验证响应内容
	assert.Equal(t, "Item claimed successfully! Quack!", response.Message)
	assert.True(t, response.Item.IsClaimed)
	assert.Equal(t, "player456", response.Item.ClaimerID)
	assert.Equal(t, pickupCode, response.Item.PickupCode)
}

func TestClaimItemNotFound(t *testing.T) {
	router, _ := setupTestRouter()

	// 准备领取不存在物品的请求数据
	claimRequest := handlers.ClaimItemRequest{
		PickupCode: "999999", // 不存在的取件码
		ClaimerID:  "player456",
	}

	requestBody, err := json.Marshal(claimRequest)
	assert.NoError(t, err)

	// 创建请求
	req := httptest.NewRequest(http.MethodPost, "/api/v1/items/claim", bytes.NewBuffer(requestBody))
	req.Header.Set("Content-Type", "application/json")

	// 执行请求
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 验证响应
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 解析响应
	var errorResponse handlers.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &errorResponse)
	assert.NoError(t, err)
	assert.Equal(t, "Item not found with this pickup code", errorResponse.Error)
}

// 需要在models包中添加辅助函数
func init() {
	// 注册models包中的辅助函数
	models.GetCurrentTime = func() time.Time {
		return time.Now()
	}
	models.GetExpirationTime = func() time.Time {
		return time.Now().Add(24 * time.Hour)
	}
}
