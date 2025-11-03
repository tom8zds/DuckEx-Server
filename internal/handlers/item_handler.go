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
	itemRepo      models.ItemRepository
	memoryMonitor *utils.MemoryMonitor
	auditService  utils.AuditService
}

// NewItemHandler 创建新的物品处理器
func NewItemHandler(itemRepo models.ItemRepository, memoryMonitor *utils.MemoryMonitor, auditService utils.AuditService) *ItemHandler {
	return &ItemHandler{
		itemRepo:      itemRepo,
		memoryMonitor: memoryMonitor,
		auditService:  auditService,
	}
}

// 分享物品的请求结构
type ShareItemRequest struct {
	Name           string  `json:"name" binding:"required"`
	Description    string  `json:"description" binding:"required"`
	TypeID         int     `json:"type_id" binding:"required"`
	Num            int     `json:"num" binding:"required,min=1"`
	Durability     float64 `json:"durability" binding:"omitempty,min=0"`
	DurabilityLoss float64 `json:"durability_loss" binding:"omitempty,min=0"`
	SharerID       string  `json:"sharer_id" binding:"required"`
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

// 领取物品的响应结构
type ClaimItemResponse struct {
	Code    int          `json:"code"`
	Message string       `json:"message"`
	Item    *models.Item `json:"item,omitempty"`
}

// ShareItem 分享物品
func (h *ItemHandler) ShareItem(c *gin.Context) {
	// 获取客户端信息
	ipAddress := c.ClientIP()
	userAgent := c.GetHeader("User-Agent")
	// 检查内存使用情况，如果内存占用过高，暂停存放接口响应
	if h.memoryMonitor != nil {
		h.memoryMonitor.UpdateStatus()
		if h.memoryMonitor.IsShareDisabled() {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":         "Storage temporarily disabled due to high memory usage. Please try again later.",
				"memory_status": h.memoryMonitor.GetStatus(),
			})

			// 记录服务不可用状态到审计日志
			if h.auditService != nil {
				var userID string
				var req ShareItemRequest
				if err := c.ShouldBindJSON(&req); err == nil {
					userID = req.SharerID
				} else {
					userID = "unknown"
				}
				h.auditService.LogError(userID, "share", "Storage disabled due to high memory usage", ipAddress, userAgent, http.StatusServiceUnavailable)
			}
			return
		}
	}

	var req ShareItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid request format: " + err.Error(),
		})

		// 记录无效请求到审计日志
		if h.auditService != nil {
			h.auditService.LogError("unknown", "share", "Invalid request format: "+err.Error(), ipAddress, userAgent, http.StatusBadRequest)
		}
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
		SharerID:    req.SharerID,
		PickupCode:  pickupCode,
		CreatedAt:   models.GetCurrentTime(),
		ExpiresAt:   models.GetExpirationTime(),
		IsClaimed:   false,
	}

	// 只有当字段被提供时才设置值（使用JSON标签处理了omitempty，这里主要是为了清晰表达逻辑）
	if req.Durability > 0 {
		item.Durability = req.Durability
	}
	if req.DurabilityLoss > 0 {
		item.DurabilityLoss = req.DurabilityLoss
	}

	// 保存物品
	if err := h.itemRepo.Create(item); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Failed to share item: " + err.Error(),
		})

		// 记录错误到审计日志
		if h.auditService != nil {
			h.auditService.LogError(req.SharerID, "share", "Failed to share item: "+err.Error(), ipAddress, userAgent, http.StatusInternalServerError)
		}
		return
	}

	c.JSON(http.StatusOK, ShareItemResponse{
		Message:    "Item shared successfully! Quack!",
		PickupCode: pickupCode,
		ExpiresAt:  expiresAt.Format(time.RFC3339),
	})

	// 记录分享操作到审计日志
	if h.auditService != nil {
		h.auditService.LogShare(req.SharerID, pickupCode, item.ID, ipAddress, userAgent)
	}

	// 记录成功的API调用
	h.itemRepo.RecordAPICall(true, "share")
}

// ClaimItem 领取物品
func (h *ItemHandler) ClaimItem(c *gin.Context) {
	// 获取客户端信息
	ipAddress := c.ClientIP()
	userAgent := c.GetHeader("User-Agent")
	var req ClaimItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ClaimItemResponse{
			Code:    400,
			Message: "请求格式无效: " + err.Error(),
		})

		// 记录无效请求到审计日志
		if h.auditService != nil {
			h.auditService.LogError("unknown", "claim", "Invalid request format: "+err.Error(), ipAddress, userAgent, http.StatusBadRequest)
		}
		return
	}

	// 根据取件码查找物品
	item, err := h.itemRepo.GetByPickupCode(req.PickupCode)
	if err != nil {
		c.JSON(http.StatusOK, ClaimItemResponse{
			Code:    500,
			Message: "领取物品失败: " + err.Error(),
		})

		// 记录错误到审计日志
		if h.auditService != nil {
			h.auditService.LogError(req.ClaimerID, "claim", "Failed to claim item: "+err.Error(), ipAddress, userAgent, http.StatusInternalServerError)
		}
		return
	}

	if item == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error: "Item not found with this pickup code",
		})

		// 记录无效取件码到审计日志
		if h.auditService != nil {
			h.auditService.LogInvalidCode(req.ClaimerID, req.PickupCode, ipAddress, userAgent)
		}
		return
	}

	// 检查物品是否已被领取
	if item.IsClaimed {
		c.JSON(http.StatusOK, ClaimItemResponse{
			Code:    409,
			Message: "该物品已被领取",
		})

		// 记录重复使用取件码到审计日志
		if h.auditService != nil {
			h.auditService.LogDuplicateCode(req.ClaimerID, req.PickupCode, ipAddress, userAgent)
		}
		return
	}

	// 检查物品是否过期
	if models.GetCurrentTime().After(item.ExpiresAt) {
		c.JSON(http.StatusOK, ClaimItemResponse{
			Code:    410,
			Message: "该物品已过期",
		})

		// 记录使用过期取件码到审计日志
		if h.auditService != nil {
			h.auditService.LogExpiredCode(req.ClaimerID, req.PickupCode, ipAddress, userAgent)
		}
		return
	}

	// 更新物品状态为已领取
	item.IsClaimed = true
	item.ClaimerID = req.ClaimerID

	// 物品被领取后从仓库中删除，这样总数统计就会下降
	if err := h.itemRepo.Delete(req.PickupCode); err != nil {
		c.JSON(http.StatusInternalServerError, ClaimItemResponse{
			Code:    500,
			Message: "领取物品失败: " + err.Error(),
		})

		// 记录错误到审计日志
		if h.auditService != nil {
			h.auditService.LogError(req.ClaimerID, "claim", "Failed to update item status: "+err.Error(), ipAddress, userAgent, http.StatusInternalServerError)
		}
		return
	}

	c.JSON(http.StatusOK, ClaimItemResponse{
		Code:    200,
		Message: "Item claimed successfully! Quack!",
		Item:    item,
	})

	// 记录成功领取到审计日志
	if h.auditService != nil {
		h.auditService.LogClaim(req.ClaimerID, req.PickupCode, item.ID, ipAddress, userAgent, true)
	}

	// 记录成功的API调用
	h.itemRepo.RecordAPICall(true, "claim")
}
