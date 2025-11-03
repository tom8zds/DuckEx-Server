package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var (
	// DB 全局数据库连接
	DB *sql.DB
)

// InitSQLite 初始化SQLite数据库连接
func InitSQLite(dbPath string) error {
	if dbPath == "" {
		dbPath = "./duckex.db"
	}

	var err error
	DB, err = sql.Open("sqlite3", dbPath+
		"?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL&cache=shared")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// 设置连接池参数
	DB.SetMaxOpenConns(10)
	DB.SetMaxIdleConns(5)
	DB.SetConnMaxLifetime(time.Hour)

	// 测试连接
	if err := DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// 创建必要的表
	if err := createTables(); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	log.Printf("SQLite database initialized successfully: %s", dbPath)
	return nil
}

// createTables 创建所有必要的表
func createTables() error {
	tables := []string{
		// 审计日志表
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		action TEXT NOT NULL,
		level TEXT NOT NULL,
		user_id TEXT,
		pickup_code TEXT,
		item_id TEXT,
		message TEXT NOT NULL,
		ip_address TEXT,
		user_agent TEXT,
		status_code INTEGER,
		is_suspicious BOOLEAN DEFAULT 0,
		suspicious_reason TEXT
		)`,

		// 物品表
		`CREATE TABLE IF NOT EXISTS items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		description TEXT,
		type_id INTEGER,
		num INTEGER,
		durability REAL,
		durability_loss REAL,
		sharer_id TEXT NOT NULL,
		pickup_code TEXT NOT NULL UNIQUE,
		created_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL,
		is_claimed BOOLEAN DEFAULT 0,
		claimer_id TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_items_expires_at ON items(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_items_is_claimed ON items(is_claimed)`,
		`CREATE INDEX IF NOT EXISTS idx_items_sharer_id ON items(sharer_id)`,
		`CREATE INDEX IF NOT EXISTS idx_items_created_at ON items(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_items_sharer_expires ON items(sharer_id, expires_at)`,
		
		// 物品统计表
		`CREATE TABLE IF NOT EXISTS item_statistics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		item_count INTEGER NOT NULL,
		claimed_count INTEGER NOT NULL,
		unclaimed_count INTEGER NOT NULL
		)`,
		// 如果表已存在但结构不同，需要重新创建（仅用于开发环境）
		`DROP TABLE IF EXISTS items_backup`,
		`ALTER TABLE items RENAME TO items_backup`,
		`CREATE TABLE IF NOT EXISTS items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		description TEXT,
		type_id INTEGER,
		num INTEGER,
		durability REAL,
		durability_loss REAL,
		sharer_id TEXT NOT NULL,
		pickup_code TEXT NOT NULL UNIQUE,
		created_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL,
		is_claimed BOOLEAN DEFAULT 0,
		claimer_id TEXT
		)`,
		// 尝试恢复数据（忽略id列）
		`INSERT INTO items (name, description, type_id, num, durability, durability_loss, sharer_id, pickup_code, created_at, expires_at, is_claimed, claimer_id)
		SELECT name, description, type_id, num, durability, durability_loss, sharer_id, pickup_code, created_at, expires_at, is_claimed, claimer_id
		FROM items_backup WHERE pickup_code NOT IN (SELECT pickup_code FROM items)`,
		// 清理备份表
		`DROP TABLE IF EXISTS items_backup`,

		// API调用记录表
		`CREATE TABLE IF NOT EXISTS api_calls (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		is_success BOOLEAN NOT NULL,
		call_type TEXT NOT NULL
		)`,

		// 尝试次数表
		`CREATE TABLE IF NOT EXISTS attempts (
			type TEXT NOT NULL,  -- "code" 或 "user"
			key TEXT NOT NULL,   -- pickup_code 或 user_id
			count INTEGER DEFAULT 0,
			last_attempt DATETIME NOT NULL,
			PRIMARY KEY (type, key)
		)`,
	}

	for _, tableSQL := range tables {
		if _, err := DB.Exec(tableSQL); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// 创建索引以提高查询性能
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_timestamp ON audit_logs(timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_pickup_code ON audit_logs(pickup_code)`,
		`CREATE INDEX IF NOT EXISTS idx_items_pickup_code ON items(pickup_code)`,
		`CREATE INDEX IF NOT EXISTS idx_items_expires_at ON items(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_api_calls_timestamp ON api_calls(timestamp)`,
	}

	for _, indexSQL := range indexes {
		if _, err := DB.Exec(indexSQL); err != nil {
			log.Printf("Warning: failed to create index: %v", err)
		}
	}

	return nil
}

// Close 关闭数据库连接
func Close() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}

// ExecuteTransaction 在事务中执行函数
func ExecuteTransaction(fn func(*sql.Tx) error) error {
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}