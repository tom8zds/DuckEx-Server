package utils

import (
	"runtime"
	"sync"
)

// MemoryMonitor 内存监控器
type MemoryMonitor struct {
	mu               sync.RWMutex
	maxMemoryMB      int64         // 最大允许内存使用量(MB)
	shareDisabled    bool          // 是否禁用分享功能
	disableThreshold float64       // 禁用阈值(0.8表示80%)
	enableThreshold  float64       // 启用阈值(0.7表示70%)
}

// NewMemoryMonitor 创建新的内存监控器
func NewMemoryMonitor(maxMemoryMB int64) *MemoryMonitor {
	return &MemoryMonitor{
		maxMemoryMB:      maxMemoryMB,
		shareDisabled:    false,
		disableThreshold: 0.8, // 80% 阈值时禁用
		enableThreshold:  0.7, // 70% 阈值时恢复
	}
}

// GetMemoryUsage 获取当前内存使用情况(MB)
func (m *MemoryMonitor) GetMemoryUsage() int64 {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	return int64(memStats.Alloc / 1024 / 1024) // 转换为MB并转换为int64
}

// GetMemoryUsagePercentage 获取内存使用百分比
func (m *MemoryMonitor) GetMemoryUsagePercentage() float64 {
	if m.maxMemoryMB <= 0 {
		return 0
	}
	usage := m.GetMemoryUsage()
	return float64(usage) / float64(m.maxMemoryMB)
}

// UpdateStatus 更新内存监控状态
func (m *MemoryMonitor) UpdateStatus() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	percentage := m.GetMemoryUsagePercentage()
	
	// 根据内存使用情况更新分享功能状态
	if percentage >= m.disableThreshold {
		m.shareDisabled = true
	} else if percentage <= m.enableThreshold && m.shareDisabled {
		m.shareDisabled = false
	}
}

// IsShareDisabled 检查分享功能是否被禁用
func (m *MemoryMonitor) IsShareDisabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.shareDisabled
}

// GetStatus 获取当前监控状态
func (m *MemoryMonitor) GetStatus() map[string]interface{} {
	usage := m.GetMemoryUsage()
	percentage := m.GetMemoryUsagePercentage()
	
	return map[string]interface{}{
		"current_usage_mb": usage,
		"max_memory_mb":    m.maxMemoryMB,
		"usage_percentage": percentage,
		"share_disabled":   m.IsShareDisabled(),
	}
}