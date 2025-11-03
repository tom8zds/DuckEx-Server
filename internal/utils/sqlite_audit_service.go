package utils

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"

	"duckex-server/internal/database"
)

// SQLiteAuditService 基于SQLite的审计服务实现
type SQLiteAuditService struct {
}

// NewSQLiteAuditService 创建新的SQLite审计服务实例
func NewSQLiteAuditService() *SQLiteAuditService {
	return &SQLiteAuditService{}
}

// LogRecord 记录审计日志
func (s *SQLiteAuditService) LogRecord(record AuditRecord) {
	// 设置时间戳
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now()
	}

	// 插入审计日志
	_, err := database.DB.Exec(
		`INSERT INTO audit_logs (timestamp, action, level, user_id, pickup_code, item_id, message, ip_address, user_agent, status_code, is_suspicious, suspicious_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Timestamp,
		record.Action,
		record.Level,
		record.UserID,
		record.PickupCode,
		record.ItemID,
		record.Message,
		record.IPAddress,
		record.UserAgent,
		record.StatusCode,
		record.IsSuspicious,
		record.SuspiciousReason,
	)
	if err != nil {
		log.Printf("Error inserting audit log: %v", err)
		return
	}

	// 更新尝试次数
	if record.PickupCode != "" {
		s.updateAttempts("code", record.PickupCode)
	}

	if record.UserID != "" {
		s.updateAttempts("user", record.UserID)
	}

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

	// 定期清理过期日志（保留30天）
	go s.cleanupOldLogs()
}

// updateAttempts 更新尝试次数
func (s *SQLiteAuditService) updateAttempts(attemptType, key string) {
	// 使用UPSERT语法更新尝试次数
	_, err := database.DB.Exec(
		`INSERT INTO attempts (type, key, count, last_attempt) VALUES (?, ?, 1, ?) 
		ON CONFLICT (type, key) DO UPDATE SET count = count + 1, last_attempt = ?`,
		attemptType,
		key,
		time.Now(),
		time.Now(),
	)
	if err != nil {
		log.Printf("Error updating attempts: %v", err)
	}
}

// LogShare 记录分享操作
func (s *SQLiteAuditService) LogShare(userID, pickupCode, itemID, ipAddress, userAgent string) {
	record := AuditRecord{
		Action:     ActionShare,
		Level:      LevelInfo,
		UserID:     userID,
		PickupCode: pickupCode,
		ItemID:     itemID,
		Message:    "物品分享成功",
		IPAddress:  ipAddress,
		UserAgent:  userAgent,
	}
	s.LogRecord(record)
}

// LogClaim 记录领取操作
func (s *SQLiteAuditService) LogClaim(userID, pickupCode, itemID, ipAddress, userAgent string, success bool) {
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
	codeAttemptCount := s.GetCodeAttempts(pickupCode)

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
func (s *SQLiteAuditService) LogInvalidCode(userID, pickupCode, ipAddress, userAgent string) {
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
func (s *SQLiteAuditService) LogDuplicateCode(userID, pickupCode, ipAddress, userAgent string) {
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
func (s *SQLiteAuditService) LogExpiredCode(userID, pickupCode, ipAddress, userAgent string) {
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
func (s *SQLiteAuditService) LogError(userID, action, message, ipAddress, userAgent string, statusCode int) {
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
func (s *SQLiteAuditService) GetCodeAttempts(pickupCode string) int {
	var count int
	err := database.DB.QueryRow(
		"SELECT count FROM attempts WHERE type = 'code' AND key = ?",
		pickupCode,
	).Scan(&count)

	if err == sql.ErrNoRows {
		return 0
	} else if err != nil {
		log.Printf("Error getting code attempts: %v", err)
		return 0
	}

	return count
}

// GetUserAttempts 获取某个用户的尝试次数
func (s *SQLiteAuditService) GetUserAttempts(userID string) int {
	var count int
	err := database.DB.QueryRow(
		"SELECT count FROM attempts WHERE type = 'user' AND key = ?",
		userID,
	).Scan(&count)

	if err == sql.ErrNoRows {
		return 0
	} else if err != nil {
		log.Printf("Error getting user attempts: %v", err)
		return 0
	}

	return count
}

// GetAllLogs 获取所有审计日志（按时间倒序）
func (s *SQLiteAuditService) GetAllLogs() []AuditRecord {
	rows, err := database.DB.Query(
		"SELECT timestamp, action, level, user_id, pickup_code, item_id, message, ip_address, user_agent, status_code, is_suspicious, suspicious_reason "+
		"FROM audit_logs ORDER BY timestamp DESC",
	)
	if err != nil {
		log.Printf("Error getting all logs: %v", err)
		return []AuditRecord{}
	}
	defer rows.Close()

	return s.scanAuditRecords(rows)
}

// GetLogsWithPagination 获取分页的审计日志，支持过滤
func (s *SQLiteAuditService) GetLogsWithPagination(page, pageSize int, filters map[string]string) PaginatedLogs {
	// 参数验证
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10 // 默认每页10条
	} else if pageSize > 100 {
		pageSize = 100 // 限制最大每页100条
	}

	// 计算偏移量
	offset := (page - 1) * pageSize

	// 构建SQL查询和参数
	query := "SELECT timestamp, action, level, user_id, pickup_code, item_id, message, ip_address, user_agent, status_code, is_suspicious, suspicious_reason FROM audit_logs WHERE 1=1"
	countQuery := "SELECT COUNT(*) FROM audit_logs WHERE 1=1"
	params := []interface{}{}

	// 添加过滤条件
	if action, ok := filters["action"]; ok && action != "" {
		query += " AND action = ?"
		countQuery += " AND action = ?"
		params = append(params, action)
	}

	if level, ok := filters["level"]; ok && level != "" {
		query += " AND level = ?"
		countQuery += " AND level = ?"
		params = append(params, level)
	}

	if userID, ok := filters["user_id"]; ok && userID != "" {
		query += " AND user_id LIKE ?"
		countQuery += " AND user_id LIKE ?"
		params = append(params, "%"+userID+"%")
	}

	if code, ok := filters["pickup_code"]; ok && code != "" {
		query += " AND pickup_code LIKE ?"
		countQuery += " AND pickup_code LIKE ?"
		params = append(params, "%"+code+"%")
	}

	// 处理时间范围过滤
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
		
		if !cutoffTime.IsZero() {
			query += " AND timestamp >= ?"
			countQuery += " AND timestamp >= ?"
			params = append(params, cutoffTime)
		}
	}

	// 获取总记录数
	var total int
	err := database.DB.QueryRow(countQuery, params...).Scan(&total)
	if err != nil {
		log.Printf("Error counting filtered logs: %v", err)
		total = 0
	}

	// 计算总页数
	totalPages := (total + pageSize - 1) / pageSize

	// 添加排序和分页
	query += " ORDER BY timestamp DESC LIMIT ? OFFSET ?"
	params = append(params, pageSize, offset)

	// 获取分页数据
	rows, err := database.DB.Query(query, params...)
	if err != nil {
		log.Printf("Error getting paginated logs: %v", err)
		return PaginatedLogs{
			Total:      total,
			Page:       page,
			PageSize:   pageSize,
			TotalPages: totalPages,
			Logs:       []AuditRecord{},
		}
	}
	defer rows.Close()

	logs := s.scanAuditRecords(rows)

	return PaginatedLogs{
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		Logs:       logs,
	}
}

// SaveAuditLog 保存审计日志（SQLite版本不需要特殊处理）
func (s *SQLiteAuditService) SaveAuditLog() error {
	// 在SQLite实现中，日志已经实时保存到数据库，这里只需要定期清理过期数据
	return s.cleanupOldLogs()
}

// scanAuditRecords 扫描审计记录
func (s *SQLiteAuditService) scanAuditRecords(rows *sql.Rows) []AuditRecord {
	var records []AuditRecord

	for rows.Next() {
		var record AuditRecord
		var userID, pickupCode, itemID, ipAddress, userAgent, suspiciousReason sql.NullString
		var statusCode sql.NullInt64
		var isSuspicious sql.NullBool

		err := rows.Scan(
			&record.Timestamp,
			&record.Action,
			&record.Level,
			&userID,
			&pickupCode,
			&itemID,
			&record.Message,
			&ipAddress,
			&userAgent,
			&statusCode,
			&isSuspicious,
			&suspiciousReason,
		)

		if err != nil {
			log.Printf("Error scanning audit record: %v", err)
			continue
		}

		// 处理可空字段
		if userID.Valid {
			record.UserID = userID.String
		}
		if pickupCode.Valid {
			record.PickupCode = pickupCode.String
		}
		if itemID.Valid {
			record.ItemID = itemID.String
		}
		if ipAddress.Valid {
			record.IPAddress = ipAddress.String
		}
		if userAgent.Valid {
			record.UserAgent = userAgent.String
		}
		if statusCode.Valid {
			record.StatusCode = int(statusCode.Int64)
		}
		if isSuspicious.Valid {
			record.IsSuspicious = isSuspicious.Bool
		}
		if suspiciousReason.Valid {
			record.SuspiciousReason = suspiciousReason.String
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Error iterating audit records: %v", err)
	}

	return records
}

// cleanupOldLogs 清理过期日志（保留30天）
func (s *SQLiteAuditService) cleanupOldLogs() error {
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)

	result, err := database.DB.Exec(
		"DELETE FROM audit_logs WHERE timestamp < ?",
		thirtyDaysAgo,
	)
	if err != nil {
		return fmt.Errorf("failed to cleanup old logs: %w", err)
	}

	deleted, err := result.RowsAffected()
	if err == nil && deleted > 0 {
		log.Printf("Cleaned up %d old audit logs", deleted)
	}

	// 清理过期的尝试记录（保留7天）
	sevenDaysAgo := time.Now().Add(-7 * 24 * time.Hour)
	_, err = database.DB.Exec(
		"DELETE FROM attempts WHERE last_attempt < ?",
		sevenDaysAgo,
	)
	if err != nil {
		log.Printf("Warning: failed to cleanup old attempts: %v", err)
	}

	return nil
}