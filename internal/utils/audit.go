package utils

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AuditAction 定义审计操作类型
type AuditAction string

const (
	// ActionShare 分享物品操作
	ActionShare AuditAction = "share"
	// ActionClaim 领取物品操作
	ActionClaim AuditAction = "claim"
	// ActionInvalidCode 使用无效取件码
	ActionInvalidCode AuditAction = "invalid_code"
	// ActionDuplicateCode 重复使用取件码
	ActionDuplicateCode AuditAction = "duplicate_code"
	// ActionExpiredCode 使用过期取件码
	ActionExpiredCode AuditAction = "expired_code"
	// ActionError 其他错误操作
	ActionError AuditAction = "error"
)

// AuditLevel 定义审计日志级别
type AuditLevel string

const (
	// LevelInfo 普通信息日志
	LevelInfo AuditLevel = "info"
	// LevelWarning 警告日志
	LevelWarning AuditLevel = "warning"
	// LevelError 错误日志
	LevelError AuditLevel = "error"
	// LevelAlert 告警日志（需要特别关注的可疑行为）
	LevelAlert AuditLevel = "alert"
)

// AuditRecord 审计记录结构
type AuditRecord struct {
	Timestamp       time.Time   `json:"timestamp"`
	Action          AuditAction `json:"action"`
	Level           AuditLevel  `json:"level"`
	UserID          string      `json:"user_id"`
	PickupCode      string      `json:"pickup_code,omitempty"`
	ItemID          string      `json:"item_id,omitempty"`
	Message         string      `json:"message"`
	IPAddress       string      `json:"ip_address,omitempty"`
	UserAgent       string      `json:"user_agent,omitempty"`
	StatusCode      int         `json:"status_code,omitempty"`
	IsSuspicious    bool        `json:"is_suspicious"`
	SuspiciousReason string     `json:"suspicious_reason,omitempty"`
}

// PaginatedLogs 分页日志响应结构
type PaginatedLogs struct {
	Total       int           `json:"total"`
	Page        int           `json:"page"`
	PageSize    int           `json:"page_size"`
	TotalPages  int           `json:"total_pages"`
	Logs        []AuditRecord `json:"logs"`
}

// AuditService 审计服务接口
type AuditService interface {
	LogRecord(record AuditRecord)
	LogShare(userID, pickupCode, itemID, ipAddress, userAgent string)
	LogClaim(userID, pickupCode, itemID, ipAddress, userAgent string, success bool)
	LogInvalidCode(userID, pickupCode, ipAddress, userAgent string)
	LogDuplicateCode(userID, pickupCode, ipAddress, userAgent string)
	LogExpiredCode(userID, pickupCode, ipAddress, userAgent string)
	LogError(userID, action, message, ipAddress, userAgent string, statusCode int)
	GetCodeAttempts(pickupCode string) int
	GetUserAttempts(userID string) int
	GetAllLogs() []AuditRecord
	GetLogsWithPagination(page, pageSize int, filters map[string]string) PaginatedLogs
	SaveAuditLog() error
}

// InMemoryAuditService 基于内存的审计服务实现
type InMemoryAuditService struct {
	records        []AuditRecord
	codeAttempts   map[string]int // 记录每个取件码的尝试次数
	userAttempts   map[string]int // 记录每个用户的尝试次数
	lastSaveTime   time.Time
	mutex          sync.RWMutex
	logFilePath    string
}

// NewAuditService 创建新的审计服务实例
func NewAuditService(logFilePath string) *InMemoryAuditService {
	if logFilePath == "" {
		logFilePath = "./audit_log.json"
	}
	
	return &InMemoryAuditService{
		records:      make([]AuditRecord, 0),
		codeAttempts: make(map[string]int),
		userAttempts: make(map[string]int),
		lastSaveTime: time.Now(),
		logFilePath:  logFilePath,
	}
}

// LogRecord 记录审计日志
func (s *InMemoryAuditService) LogRecord(record AuditRecord) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	// 设置时间戳
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now()
	}
	
	// 记录尝试次数
	if record.PickupCode != "" {
		s.codeAttempts[record.PickupCode]++
	}
	
	if record.UserID != "" {
		s.userAttempts[record.UserID]++
	}
	
	// 添加到记录列表
	s.records = append(s.records, record)
	
	// 打印日志
	logLevel := "INFO"
	switch record.Level {
	case LevelWarning:
		logLevel = "WARNING"
	case LevelError:
		logLevel = "ERROR"
	case LevelAlert:
		logLevel = "ALERT"
	}
	
	suspiciousMark := ""
	if record.IsSuspicious {
		suspiciousMark = " [SUSPICIOUS]"
	}
	
	log.Printf("[AUDIT] [%s] %s: %s%s - User: %s, Code: %s, Item: %s", 
		logLevel, record.Action, record.Message, suspiciousMark,
		record.UserID, record.PickupCode, record.ItemID)
	
	// 定期保存日志到文件
	if time.Since(s.lastSaveTime) > 5*time.Minute || len(s.records) > 1000 {
		go s.SaveAuditLog()
	}
	
	// 限制内存中的记录数量，防止内存泄漏
	if len(s.records) > 10000 {
		s.records = s.records[len(s.records)-5000:]
	}
}

// LogShare 记录分享操作
func (s *InMemoryAuditService) LogShare(userID, pickupCode, itemID, ipAddress, userAgent string) {
	record := AuditRecord{
		Action:    ActionShare,
		Level:     LevelInfo,
		UserID:    userID,
		PickupCode: pickupCode,
		ItemID:    itemID,
		Message:   "物品分享成功",
		IPAddress: ipAddress,
		UserAgent: userAgent,
	}
	s.LogRecord(record)
}

// LogClaim 记录领取操作
func (s *InMemoryAuditService) LogClaim(userID, pickupCode, itemID, ipAddress, userAgent string, success bool) {
	level := LevelInfo
	message := "物品领取成功"
	statusCode := 200

	if !success {
		level = LevelWarning
		message = "物品领取失败"
		statusCode = 400
	}
	
	// 检查是否是可疑操作（取件码尝试次数过多）
	isSuspicious := false
	suspiciousReason := ""
	s.mutex.RLock()
	codeAttemptCount := s.codeAttempts[pickupCode]
	s.mutex.RUnlock()
	
	if codeAttemptCount > 3 {
		isSuspicious = true
		level = LevelAlert
		suspiciousReason = "取件码尝试次数超限 (" + strconv.Itoa(codeAttemptCount) + ")"
	}
	
	record := AuditRecord{
		Action:           ActionClaim,
		Level:            level,
		UserID:           userID,
		PickupCode:       pickupCode,
		ItemID:           itemID,
		Message:          message,
		IPAddress:        ipAddress,
		UserAgent:        userAgent,
		StatusCode:       statusCode,
		IsSuspicious:     isSuspicious,
		SuspiciousReason: suspiciousReason,
	}
	s.LogRecord(record)
}

// LogInvalidCode 记录使用无效取件码
func (s *InMemoryAuditService) LogInvalidCode(userID, pickupCode, ipAddress, userAgent string) {
	// 移除用户尝试次数检测，保持简单的记录功能
	isSuspicious := false
	
	record := AuditRecord{
		Action:       ActionInvalidCode,
		Level:        LevelWarning,
		UserID:       userID,
		PickupCode:   pickupCode,
		Message:      "尝试使用不存在的取件码",
		IPAddress:    ipAddress,
		UserAgent:    userAgent,
		StatusCode:   404,
		IsSuspicious: isSuspicious,
	}
	s.LogRecord(record)
}

// LogDuplicateCode 记录重复使用取件码
func (s *InMemoryAuditService) LogDuplicateCode(userID, pickupCode, ipAddress, userAgent string) {
	record := AuditRecord{
		Action:       ActionDuplicateCode,
		Level:        LevelAlert,
		UserID:       userID,
		PickupCode:   pickupCode,
		Message:      "尝试使用已被领取的取件码",
		IPAddress:    ipAddress,
		UserAgent:    userAgent,
		StatusCode:   409,
		IsSuspicious: true,
	}
	s.LogRecord(record)
}

// LogExpiredCode 记录使用过期取件码
func (s *InMemoryAuditService) LogExpiredCode(userID, pickupCode, ipAddress, userAgent string) {
	record := AuditRecord{
		Action:       ActionExpiredCode,
		Level:        LevelWarning,
		UserID:       userID,
		PickupCode:   pickupCode,
		Message:      "尝试使用过期的取件码",
		IPAddress:    ipAddress,
		UserAgent:    userAgent,
		StatusCode:   410,
		IsSuspicious: false,
	}
	s.LogRecord(record)
}

// LogError 记录接口报错审计
func (s *InMemoryAuditService) LogError(userID, action, message, ipAddress, userAgent string, statusCode int) {
	record := AuditRecord{
		Action:     AuditAction(action),
		Level:      LevelError,
		UserID:     userID,
		Message:    message,
		IPAddress:  ipAddress,
		UserAgent:  userAgent,
		StatusCode: statusCode,
	}
	s.LogRecord(record)
}

// GetCodeAttempts 获取某个取件码的尝试次数
func (s *InMemoryAuditService) GetCodeAttempts(pickupCode string) int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.codeAttempts[pickupCode]
}

// GetUserAttempts 获取某个用户的尝试次数
func (s *InMemoryAuditService) GetUserAttempts(userID string) int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.userAttempts[userID]
}

// GetAllLogs 获取所有审计日志（按时间倒序）
func (s *InMemoryAuditService) GetAllLogs() []AuditRecord {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	// 返回记录的副本，避免并发访问问题
	logs := make([]AuditRecord, len(s.records))
	copy(logs, s.records)
	
	// 按时间戳倒序排序（最新的在前）
	for i := 0; i < len(logs)-1; i++ {
		for j := 0; j < len(logs)-i-1; j++ {
			if logs[j].Timestamp.Before(logs[j+1].Timestamp) {
				logs[j], logs[j+1] = logs[j+1], logs[j]
			}
		}
	}
	
	return logs
}

// GetLogsWithPagination 获取分页的审计日志，支持过滤
func (s *InMemoryAuditService) GetLogsWithPagination(page, pageSize int, filters map[string]string) PaginatedLogs {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	// 参数验证
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10 // 默认每页10条
	} else if pageSize > 100 {
		pageSize = 100 // 限制最大每页100条
	}
	
	// 复制所有记录以进行过滤和排序
	allRecords := make([]AuditRecord, len(s.records))
	copy(allRecords, s.records)
	
	// 应用过滤条件
	var filteredRecords []AuditRecord
	for _, record := range allRecords {
		// 操作类型过滤
		if action, ok := filters["action"]; ok && action != "" && record.Action != AuditAction(action) {
			continue
		}
		
		// 日志级别过滤
		if level, ok := filters["level"]; ok && level != "" && record.Level != AuditLevel(level) {
			continue
		}
		
		// 用户ID过滤（模糊匹配）
		if userID, ok := filters["user_id"]; ok && userID != "" {
			if !strings.Contains(strings.ToLower(record.UserID), strings.ToLower(userID)) {
				continue
			}
		}
		
		// 取件码过滤（模糊匹配）
		if code, ok := filters["pickup_code"]; ok && code != "" {
			if !strings.Contains(strings.ToUpper(record.PickupCode), strings.ToUpper(code)) {
				continue
			}
		}
		
		// 时间范围过滤
		if timeRange, ok := filters["time_range"]; ok && timeRange != "" && timeRange != "all" {
			var cutoffTime time.Time
			now := time.Now()
			
			switch timeRange {
			case "1h":
				cutoffTime = now.Add(-1 * time.Hour)
			case "6h":
				cutoffTime = now.Add(-6 * time.Hour)
			case "24h":
				cutoffTime = now.Add(-24 * time.Hour)
			case "7d":
				cutoffTime = now.Add(-7 * 24 * time.Hour)
			}
			
			if !cutoffTime.IsZero() && record.Timestamp.Before(cutoffTime) {
				continue
			}
		}
		
		filteredRecords = append(filteredRecords, record)
	}
	
	// 按时间戳倒序排序（最新的在前）
	for i := 0; i < len(filteredRecords)-1; i++ {
		for j := 0; j < len(filteredRecords)-i-1; j++ {
			if filteredRecords[j].Timestamp.Before(filteredRecords[j+1].Timestamp) {
				filteredRecords[j], filteredRecords[j+1] = filteredRecords[j+1], filteredRecords[j]
			}
		}
	}
	
	// 计算总页数
	total := len(filteredRecords)
	totalPages := (total + pageSize - 1) / pageSize
	
	// 计算偏移量
	offset := (page - 1) * pageSize
	end := offset + pageSize
	
	// 调整结束位置
	if end > total {
		end = total
	}
	
	// 创建分页日志响应
	response := PaginatedLogs{
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		Logs:       []AuditRecord{},
	}
	
	// 如果有数据，复制对应页的数据
	if offset < total {
		response.Logs = make([]AuditRecord, end-offset)
		copy(response.Logs, filteredRecords[offset:end])
	}
	
	return response
}

// SaveAuditLog 保存审计日志到文件
func (s *InMemoryAuditService) SaveAuditLog() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if len(s.records) == 0 {
		return nil
	}
	
	// 将记录转换为JSON
	logData, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal audit records: %w", err)
	}
	
	// 保存到文件
	if err := os.WriteFile(s.logFilePath, logData, 0644); err != nil {
		return fmt.Errorf("failed to write audit log to file: %w", err)
	}
	
	log.Printf("Saved %d audit records to %s", len(s.records), s.logFilePath)
	s.lastSaveTime = time.Now()
	
	return nil
}