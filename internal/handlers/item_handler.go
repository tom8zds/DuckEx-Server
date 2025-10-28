package handlers

import (
	"net/http"
	"time"

	"duckex-server/internal/models"
	"duckex-server/internal/utils"

	"github.com/gin-gonic/gin"
)

// ItemHandler 物品处理器
type ItemHandler struct {
	itemRepo       models.ItemRepository
	memoryMonitor  *utils.MemoryMonitor
}

// NewItemHandler 创建新的物品处理器
func NewItemHandler(itemRepo models.ItemRepository, memoryMonitor *utils.MemoryMonitor) *ItemHandler {
	return &ItemHandler{
		itemRepo:      itemRepo,
		memoryMonitor: memoryMonitor,
	}
}

// 分享物品的请求结构
type ShareItemRequest struct {
	Name        string  `json:"name" binding:"required"`
	Description string  `json:"description" binding:"required"`
	TypeID      int     `json:"type_id" binding:"required"`
	Num         int     `json:"num" binding:"required,min=1"`
	Durability  float64 `json:"durability" binding:"required,min=0"`
	SharerID    string  `json:"sharer_id" binding:"required"`
}

// 分享物品的响应结构
type ShareItemResponse struct {
	Message    string `json:"message"`
	PickupCode string `json:"pickup_code"`
	ExpiresAt  string `json:"expires_at"`
}

// 领取物品的请求结构
type ClaimItemRequest struct {
	PickupCode string `json:"pickup_code" binding:"required"`
	ClaimerID  string `json:"claimer_id" binding:"required"`
}

// 错误响应结构
type ErrorResponse struct {
	Error string `json:"error"`
}

// ShareItem 分享物品
func (h *ItemHandler) ShareItem(c *gin.Context) {
	// 检查内存使用情况，如果内存占用过高，暂停存放接口响应
	if h.memoryMonitor != nil {
		h.memoryMonitor.UpdateStatus()
		if h.memoryMonitor.IsShareDisabled() {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "Storage temporarily disabled due to high memory usage. Please try again later.",
				"memory_status": h.memoryMonitor.GetStatus(),
			})
			return
		}
	}
	
	var req ShareItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid request format: " + err.Error(),
		})
		return
	}

	// 生成取件码
	pickupCode := utils.GeneratePickupCode()
	expiresAt := utils.GetExpirationTime()

	// 创建物品
	item := &models.Item{
		ID:          models.GetCurrentTime().Format("20060102150405") + req.SharerID,
		Name:        req.Name,
		Description: req.Description,
		TypeID:      req.TypeID,
		Num:         req.Num,
		Durability:  req.Durability,
		SharerID:    req.SharerID,
		PickupCode:  pickupCode,
		CreatedAt:   models.GetCurrentTime(),
		ExpiresAt:   models.GetExpirationTime(),
		IsClaimed:   false,
	}

	// 保存物品
	if err := h.itemRepo.Create(item); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Failed to share item: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, ShareItemResponse{
		Message:    "Item shared successfully! Quack!",
		PickupCode: pickupCode,
		ExpiresAt:  expiresAt.Format(time.RFC3339),
	})
}

// ClaimItem 领取物品
func (h *ItemHandler) ClaimItem(c *gin.Context) {
	var req ClaimItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid request format: " + err.Error(),
		})
		return
	}

	// 根据取件码查找物品
	item, err := h.itemRepo.GetByPickupCode(req.PickupCode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Failed to claim item: " + err.Error(),
		})
		return
	}

	if item == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error: "Item not found with this pickup code",
		})
		return
	}

	// 检查物品是否已被领取
	if item.IsClaimed {
		c.JSON(http.StatusConflict, ErrorResponse{
			Error: "Item has already been claimed",
		})
		return
	}

	// 检查物品是否过期
	if models.GetCurrentTime().After(item.ExpiresAt) {
		c.JSON(http.StatusGone, ErrorResponse{
			Error: "Item has expired",
		})
		return
	}

	// 标记物品为已领取
	item.IsClaimed = true
	item.ClaimerID = req.ClaimerID

	// 更新物品信息
	if err := h.itemRepo.Update(item); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Failed to update item: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Item claimed successfully! Quack!",
		"item":    item,
	})
}