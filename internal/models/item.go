package models

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
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
	items       map[string]*Item
	mutex       sync.RWMutex
	storagePath string
	fileMutex   sync.Mutex // 用于文件操作的互斥锁
	ticker      *time.Ticker
	stopChan    chan struct{}
}

// NewInMemoryItemRepository 创建新的内存仓库实例
func NewInMemoryItemRepository() *InMemoryItemRepository {
	// 默认存储路径
	storagePath := "./items_backup.json"
	
	// 创建仓库实例
	repo := &InMemoryItemRepository{
		items:       make(map[string]*Item),
		storagePath: storagePath,
		fileMutex:   sync.Mutex{},
		ticker:      time.NewTicker(5 * time.Minute),
		stopChan:    make(chan struct{}),
	}
	
	// 从文件加载未领取的物品
	repo.LoadFromFile()
	
	// 启动定时保存任务
	repo.startPeriodicSave()
	
	return repo
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

// startPeriodicSave 启动定期保存任务
func (r *InMemoryItemRepository) startPeriodicSave() {
	go func() {
		log.Println("Starting periodic save task (every 5 minutes)")
		for {
			select {
			case <-r.ticker.C:
				r.SaveToFile()
			case <-r.stopChan:
				r.ticker.Stop()
				log.Println("Periodic save task stopped")
				return
			}
		}
	}()
}

// Shutdown 优雅关闭，保存数据
func (r *InMemoryItemRepository) Shutdown() error {
	log.Println("Shutting down item repository, saving data...")
	
	// 停止定时保存任务
	close(r.stopChan)
	
	// 保存当前数据
	return r.SaveToFile()
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

// GetTotalCount 获取物品总数（包括已过期和已领取的）
func (r *InMemoryItemRepository) GetTotalCount() int {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return len(r.items)
}

// GetProcessedCountInTimeRange 获取指定时间范围内处理的物品数量（分享和领取）
func (r *InMemoryItemRepository) GetProcessedCountInTimeRange(startTime, endTime time.Time) int {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	processedCount := 0
	for _, item := range r.items {
		// 统计创建时间在指定范围内的物品（分享）
		// 注意：对于已领取的物品，我们也计入统计
		if (item.CreatedAt.After(startTime) || item.CreatedAt.Equal(startTime)) &&
		   (item.CreatedAt.Before(endTime) || item.CreatedAt.Equal(endTime)) {
			processedCount++
		}
	}
	return processedCount
}

// LoadFromFile 从JSON文件加载未领取的物品
func (r *InMemoryItemRepository) LoadFromFile() error {
	// 检查文件是否存在
	if _, err := os.Stat(r.storagePath); os.IsNotExist(err) {
		log.Printf("No existing backup file found at %s", r.storagePath)
		return nil
	}
	
	// 使用文件锁确保线程安全地读取文件
	r.fileMutex.Lock()
	
	// 读取文件内容
	data, err := ioutil.ReadFile(r.storagePath)
	
	// 先释放文件锁，因为后续操作不再需要访问文件
	r.fileMutex.Unlock()
	
	if err != nil {
		log.Printf("Error reading backup file: %v", err)
		return err
	}
	
	// 检查数据是否为空
	if len(data) == 0 {
		log.Printf("Backup file is empty, skipping load")
		return nil
	}
	
	// 解析JSON数据
	var items []*Item
	if err := json.Unmarshal(data, &items); err != nil {
		log.Printf("Error unmarshaling backup data: %v", err)
		return err
	}
	
	// 加锁并加载物品到内存
	r.mutex.Lock()
	defer r.mutex.Unlock()
	
	// 过滤出未过期的物品并加载到内存
	successfullyLoaded := 0
	for _, item := range items {
		// 只加载未过期且未被领取的物品
		if !GetCurrentTime().After(item.ExpiresAt) && !item.IsClaimed {
			r.items[item.PickupCode] = item
			successfullyLoaded++
		}
	}
	
	log.Printf("Successfully loaded %d unclaimed items from backup", successfullyLoaded)
	return nil
}

// SaveToFile 将当前未领取的物品保存到JSON文件
func (r *InMemoryItemRepository) SaveToFile() error {
	// 获取当前未过期且未被领取的物品
	r.mutex.RLock()
	var itemsToSave []*Item
	for _, item := range r.items {
		if !item.IsClaimed && !GetCurrentTime().After(item.ExpiresAt) {
			itemsToSave = append(itemsToSave, item)
		}
	}
	r.mutex.RUnlock()
	
	// 将物品序列化为JSON
	data, err := json.MarshalIndent(itemsToSave, "", "  ")
	if err != nil {
		log.Printf("Error marshaling items to JSON: %v", err)
		return err
	}
	
	// 使用文件锁确保线程安全地写入文件
	r.fileMutex.Lock()
	defer r.fileMutex.Unlock()
	
	// 写入文件
	if err := ioutil.WriteFile(r.storagePath, data, 0644); err != nil {
		log.Printf("Error writing to backup file: %v", err)
		return err
	}
	
	log.Printf("Successfully saved %d unclaimed items to backup", len(itemsToSave))
	return nil
}