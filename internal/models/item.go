package models

import (
	"sync"
	"time"
)

// 导出的辅助函数，用于测试
var (
	GetCurrentTime    func() time.Time
	GetExpirationTime func() time.Time
)

// 初始化默认实现
func init() {
	GetCurrentTime = func() time.Time {
		return time.Now()
	}
	GetExpirationTime = func() time.Time {
		return time.Now().Add(24 * time.Hour)
	}
}

// Item 物品模型
type Item struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	TypeID      int       `json:"type_id"`
	Num         int       `json:"num"`
	Durability  float64   `json:"durability"`
	SharerID    string    `json:"sharer_id"`
	PickupCode  string    `json:"pickup_code"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	IsClaimed   bool      `json:"is_claimed"`
	ClaimerID   string    `json:"claimer_id"`
}

// ItemRepository 物品仓库接口
type ItemRepository interface {
	Create(item *Item) error
	GetByPickupCode(pickupCode string) (*Item, error)
	Update(item *Item) error
	Delete(pickupCode string) error
	DeleteExpired() error
	GetAll() []*Item
}

// InMemoryItemRepository 内存实现的物品仓库
type InMemoryItemRepository struct {
	items map[string]*Item
	mutex sync.RWMutex
}

// NewInMemoryItemRepository 创建新的内存仓库实例
func NewInMemoryItemRepository() *InMemoryItemRepository {
	return &InMemoryItemRepository{
		items: make(map[string]*Item),
	}
}

// Create 创建新物品
func (r *InMemoryItemRepository) Create(item *Item) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.items[item.PickupCode] = item
	return nil
}

// GetByPickupCode 通过取件码获取物品
func (r *InMemoryItemRepository) GetByPickupCode(pickupCode string) (*Item, error) {
	r.mutex.RLock()
	item, exists := r.items[pickupCode]
	if !exists {
		r.mutex.RUnlock()
		return nil, nil
	}
	
	// 检查物品是否过期
	if GetCurrentTime().After(item.ExpiresAt) {
		// 解锁读锁，获取写锁删除过期物品
		r.mutex.RUnlock()
		r.mutex.Lock()
		// 再次检查物品是否存在（防止并发删除）
		if _, stillExists := r.items[pickupCode]; stillExists {
			delete(r.items, pickupCode)
		}
		r.mutex.Unlock()
		return nil, nil
	}
	
	r.mutex.RUnlock()
	return item, nil
}

// Update 更新物品信息
func (r *InMemoryItemRepository) Update(item *Item) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.items[item.PickupCode] = item
	return nil
}

// DeleteExpired 删除过期物品
func (r *InMemoryItemRepository) DeleteExpired() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	now := GetCurrentTime()
	for code, item := range r.items {
		if item.ExpiresAt.Before(now) {
			delete(r.items, code)
		}
	}
	return nil
}

// Delete 删除物品
func (r *InMemoryItemRepository) Delete(pickupCode string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	delete(r.items, pickupCode)
	return nil
}

// GetAll 获取所有物品（主要用于测试）
func (r *InMemoryItemRepository) GetAll() []*Item {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	items := make([]*Item, 0, len(r.items))
	for _, item := range r.items {
		// 只返回未过期的物品
		if !GetCurrentTime().After(item.ExpiresAt) {
			items = append(items, item)
		}
	}
	return items
}