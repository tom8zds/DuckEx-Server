package models

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"

	"duckex-server/internal/database"
)

// SQLiteItemRepository 基于SQLite的物品仓库实现
type SQLiteItemRepository struct {
	ticker   *time.Ticker
	stopChan chan struct{}
}

// NewSQLiteItemRepository 创建新的SQLite物品仓库实例
func NewSQLiteItemRepository() *SQLiteItemRepository {
	repo := &SQLiteItemRepository{
		ticker:   time.NewTicker(5 * time.Minute),
		stopChan: make(chan struct{}),
	}

	// 启动定期清理任务
	repo.startPeriodicCleanup()

	return repo
}

// Create 创建新物品
func (r *SQLiteItemRepository) Create(item *Item) error {
	result, err := database.DB.Exec(
		`INSERT INTO items (name, description, type_id, num, durability, durability_loss, sharer_id, pickup_code, created_at, expires_at, is_claimed, claimer_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.Name,
		item.Description,
		item.TypeID,
		item.Num,
		item.Durability,
		item.DurabilityLoss,
		item.SharerID,
		item.PickupCode,
		item.CreatedAt,
		item.ExpiresAt,
		item.IsClaimed,
		item.ClaimerID,
	)

	if err != nil {
		return fmt.Errorf("failed to create item: %w", err)
	}

	// 获取自增的ID
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id: %w", err)
	}
	item.ID = int(id)

	return nil
}

// GetByPickupCode 通过取件码获取物品
func (r *SQLiteItemRepository) GetByPickupCode(pickupCode string) (*Item, error) {
	var item Item
	var description sql.NullString
	var claimerID sql.NullString

	err := database.DB.QueryRow(
		`SELECT id, name, description, type_id, num, durability, durability_loss, sharer_id, pickup_code, created_at, expires_at, is_claimed, claimer_id
		FROM items WHERE pickup_code = ?`,
		pickupCode,
	).Scan(
		&item.ID,
		&item.Name,
		&description,
		&item.TypeID,
		&item.Num,
		&item.Durability,
		&item.DurabilityLoss,
		&item.SharerID,
		&item.PickupCode,
		&item.CreatedAt,
		&item.ExpiresAt,
		&item.IsClaimed,
		&claimerID,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get item: %w", err)
	}

	// 处理可空字段
	if description.Valid {
		item.Description = description.String
	}
	if claimerID.Valid {
		item.ClaimerID = claimerID.String
	}

	// 检查物品是否过期
	if GetCurrentTime().After(item.ExpiresAt) {
		// 删除过期物品
		go r.Delete(pickupCode)
		return nil, nil
	}

	return &item, nil
}

// Update 更新物品信息
func (r *SQLiteItemRepository) Update(item *Item) error {
	_, err := database.DB.Exec(
		`UPDATE items SET name = ?, description = ?, type_id = ?, num = ?, durability = ?, durability_loss = ?, sharer_id = ?, 
		created_at = ?, expires_at = ?, is_claimed = ?, claimer_id = ? WHERE pickup_code = ?`,
		item.Name,
		sql.NullString{String: item.Description, Valid: item.Description != ""},
		item.TypeID,
		item.Num,
		item.Durability,
		item.DurabilityLoss,
		item.SharerID,
		item.CreatedAt,
		item.ExpiresAt,
		item.IsClaimed,
		sql.NullString{String: item.ClaimerID, Valid: item.ClaimerID != ""},
		item.PickupCode,
	)

	if err != nil {
		return fmt.Errorf("failed to update item: %w", err)
	}

	return nil
}

// Delete 删除物品
func (r *SQLiteItemRepository) Delete(pickupCode string) error {
	_, err := database.DB.Exec(
		"DELETE FROM items WHERE pickup_code = ?",
		pickupCode,
	)

	if err != nil {
		return fmt.Errorf("failed to delete item: %w", err)
	}

	return nil
}

// DeleteExpired 删除过期物品
func (r *SQLiteItemRepository) DeleteExpired() error {
	result, err := database.DB.Exec(
		"DELETE FROM items WHERE expires_at < ?",
		GetCurrentTime(),
	)

	if err != nil {
		return fmt.Errorf("failed to delete expired items: %w", err)
	}

	deleted, err := result.RowsAffected()
	if err == nil && deleted > 0 {
		log.Printf("Deleted %d expired items", deleted)
	}

	return nil
}

// GetAll 获取所有物品（主要用于测试）
func (r *SQLiteItemRepository) GetAll() []*Item {
	rows, err := database.DB.Query(
		`SELECT id, name, description, type_id, num, durability, durability_loss, sharer_id, pickup_code, created_at, expires_at, is_claimed, claimer_id
		FROM items WHERE expires_at >= ? ORDER BY created_at DESC`,
		GetCurrentTime(),
	)
	if err != nil {
		log.Printf("Error getting all items: %v", err)
		return []*Item{}
	}
	defer rows.Close()

	var items []*Item
	for rows.Next() {
		var item Item
		var description sql.NullString
		var claimerID sql.NullString

		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&description,
			&item.TypeID,
			&item.Num,
			&item.Durability,
			&item.DurabilityLoss,
			&item.SharerID,
			&item.PickupCode,
			&item.CreatedAt,
			&item.ExpiresAt,
			&item.IsClaimed,
			&claimerID,
		); err != nil {
			log.Printf("Error scanning item: %v", err)
			continue
		}

		if description.Valid {
			item.Description = description.String
		}
		if claimerID.Valid {
			item.ClaimerID = claimerID.String
		}

		items = append(items, &item)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Error iterating items: %v", err)
	}

	return items
}

// RecordAPICall 记录API调用
func (r *SQLiteItemRepository) RecordAPICall(isSuccess bool, callType string) {
	_, err := database.DB.Exec(
		"INSERT INTO api_calls (timestamp, is_success, call_type) VALUES (?, ?, ?)",
		GetCurrentTime(),
		isSuccess,
		callType,
	)

	if err != nil {
		log.Printf("Error recording API call: %v", err)
	}
}

// GetTotalCount 获取物品总数
func (r *SQLiteItemRepository) GetTotalCount() int {
	var count int
	err := database.DB.QueryRow("SELECT COUNT(*) FROM items").Scan(&count)
	if err != nil {
		log.Printf("Error getting total item count: %v", err)
		return 0
	}
	return count
}

// GetClaimedCount 获取已领取物品的数量
func (r *SQLiteItemRepository) GetClaimedCount() int {
	var count int
	err := database.DB.QueryRow("SELECT COUNT(*) FROM items WHERE is_claimed = 1").Scan(&count)
	if err != nil {
		log.Printf("Error getting claimed item count: %v", err)
		return 0
	}
	return count
}

// GetProcessedCountInTimeRange 获取指定时间范围内成功处理的API调用次数
func (r *SQLiteItemRepository) GetProcessedCountInTimeRange(startTime, endTime time.Time) int {
	var count int
	err := database.DB.QueryRow(
		"SELECT COUNT(*) FROM api_calls WHERE is_success = 1 AND timestamp BETWEEN ? AND ?",
		startTime,
		endTime,
	).Scan(&count)

	if err != nil {
		log.Printf("Error getting processed count: %v", err)
		return 0
	}

	return count
}

// startPeriodicCleanup 启动定期清理任务
func (r *SQLiteItemRepository) startPeriodicCleanup() {
	go func() {
		log.Println("Starting periodic cleanup task (every 5 minutes)")
		for {
			select {
			case <-r.ticker.C:
				r.performCleanup()
			case <-r.stopChan:
				r.ticker.Stop()
				log.Println("Periodic cleanup task stopped")
				return
			}
		}
	}()
}

// performCleanup 执行清理任务
func (r *SQLiteItemRepository) performCleanup() {
	// 删除过期物品
	r.DeleteExpired()

	// 清理过期的API调用记录（保留7天）
	sevenDaysAgo := GetCurrentTime().AddDate(0, 0, -7)
	result, err := database.DB.Exec(
		"DELETE FROM api_calls WHERE timestamp < ?",
		sevenDaysAgo,
	)
	if err != nil {
		log.Printf("Error cleaning up old API calls: %v", err)
	} else {
		if deleted, err := result.RowsAffected(); err == nil && deleted > 0 {
			log.Printf("Cleaned up %d old API calls", deleted)
		}
	}

	// 执行VACUUM以优化数据库大小（可选，定期执行）
	// 注意：VACUUM会锁定数据库，所以不应该频繁执行
}

// Shutdown 优雅关闭，停止定时任务
func (r *SQLiteItemRepository) Shutdown() error {
	log.Println("Shutting down SQLite item repository...")

	// 停止定时清理任务
	close(r.stopChan)

	// 最后执行一次清理
	r.performCleanup()

	log.Println("SQLite item repository shutdown completed")
	return nil
}

// MigrateFromJSON 从JSON备份文件迁移数据到SQLite（用于初始化）
// 临时结构体，用于JSON反序列化，ID字段为字符串类型
type jsonItem struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	TypeID         int       `json:"type_id"`
	Num            int       `json:"num"`
	Durability     float64   `json:"durability"`
	DurabilityLoss float64   `json:"durability_loss"`
	SharerID       string    `json:"sharer_id"`
	PickupCode     string    `json:"pickup_code"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	IsClaimed      bool      `json:"is_claimed"`
	ClaimerID      string    `json:"claimer_id"`
}

func (r *SQLiteItemRepository) MigrateFromJSON(jsonFilePath string) error {
	// 检查文件是否存在
	if _, err := os.Stat(jsonFilePath); os.IsNotExist(err) {
		log.Printf("No existing JSON backup file found at %s, skipping migration", jsonFilePath)
		return nil
	}

	// 读取文件内容
	data, err := ioutil.ReadFile(jsonFilePath)
	if err != nil {
		return fmt.Errorf("error reading JSON backup file: %w", err)
	}

	// 检查数据是否为空
	if len(data) == 0 {
		log.Printf("JSON backup file is empty, skipping migration")
		return nil
	}

	// 解析JSON数据到临时结构体，处理字符串ID
	var jsonItems []*jsonItem
	if err := json.Unmarshal(data, &jsonItems); err != nil {
		return fmt.Errorf("error unmarshaling JSON backup data: %w", err)
	}

	log.Printf("Found %d items in JSON backup", len(jsonItems))

	// 迁移未过期且未被领取的物品到SQLite
	successfullyMigrated := 0
	skippedDueToPickupCodeExists := 0
	skippedDueToExpired := 0

	for _, jsonItem := range jsonItems {
		// 创建新的Item结构体，ID设为0让SQLite自动生成
		item := &Item{
			Name:           jsonItem.Name,
			Description:    jsonItem.Description,
			TypeID:         jsonItem.TypeID,
			Num:            jsonItem.Num,
			Durability:     jsonItem.Durability,
			DurabilityLoss: jsonItem.DurabilityLoss,
			SharerID:       jsonItem.SharerID,
			PickupCode:     jsonItem.PickupCode,
			CreatedAt:      jsonItem.CreatedAt,
			ExpiresAt:      jsonItem.CreatedAt.AddDate(0, 0, 7),
			IsClaimed:      jsonItem.IsClaimed,
			ClaimerID:      jsonItem.ClaimerID,
			// ID设为0，让SQLite自动生成
			ID: 0,
		}

		// 检查物品是否过期
		if GetCurrentTime().After(item.ExpiresAt) {
			skippedDueToExpired++
			continue
		}

		// 检查取件码是否已存在
		existingItem, err := r.GetByPickupCode(item.PickupCode)
		if err != nil {
			log.Printf("Error checking existing item for pickup code %s: %v, skipping", item.PickupCode, err)
			continue
		}

		if existingItem != nil {
			skippedDueToPickupCodeExists++
			continue
		}

		// 将物品插入SQLite数据库
		if err := r.Create(item); err != nil {
			log.Printf("Error migrating item with pickup code %s: %v, skipping", item.PickupCode, err)
			continue
		}

		successfullyMigrated++
	}

	log.Printf("JSON to SQLite migration completed: %d migrated, %d skipped due to existing pickup code, %d skipped due to expired",
		successfullyMigrated, skippedDueToPickupCodeExists, skippedDueToExpired)

	return nil
}
