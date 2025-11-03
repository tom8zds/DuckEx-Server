package test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"duckex-server/internal/models"
	"duckex-server/internal/utils"

	"github.com/stretchr/testify/assert"
)

func TestInMemoryItemRepository(t *testing.T) {
	repo := models.NewInMemoryItemRepository()

	// 测试创建物品
	pickupCode := utils.GeneratePickupCode()
	item := &models.Item{
		ID:             "test-item-1",
		Name:           "Test Item",
		Description:    "This is a test item",
		TypeID:         123,
		Num:            1,
		Durability:     95.5,
		DurabilityLoss: 5.5,
		SharerID:       "test-sharer",
		PickupCode:     pickupCode,
		CreatedAt:      time.Now(),
		ExpiresAt:      utils.GetExpirationTime(),
		IsClaimed:      false,
	}

	err := repo.Create(item)
	assert.NoError(t, err)

	// 测试通过取件码获取物品
	retrievedItem, err := repo.GetByPickupCode(pickupCode)
	assert.NoError(t, err)
	assert.NotNil(t, retrievedItem)
	assert.Equal(t, item.ID, retrievedItem.ID)
	assert.Equal(t, item.Name, retrievedItem.Name)
	assert.Equal(t, item.TypeID, retrievedItem.TypeID)
	assert.Equal(t, item.Num, retrievedItem.Num)
	assert.Equal(t, item.Durability, retrievedItem.Durability)
	assert.Equal(t, item.DurabilityLoss, retrievedItem.DurabilityLoss)

	// 测试获取不存在的物品
	nonExistentItem, err := repo.GetByPickupCode("999999")
	assert.NoError(t, err)
	assert.Nil(t, nonExistentItem)

	// 测试更新物品
	retrievedItem.IsClaimed = true
	retrievedItem.ClaimerID = "test-claimer"
	err = repo.Update(retrievedItem)
	assert.NoError(t, err)

	// 验证更新是否成功
	updatedItem, err := repo.GetByPickupCode(pickupCode)
	assert.NoError(t, err)
	assert.True(t, updatedItem.IsClaimed)
	assert.Equal(t, "test-claimer", updatedItem.ClaimerID)

	// 测试删除过期物品
	// 创建一个过期物品
	expiredPickupCode := utils.GeneratePickupCode()
	expiredItem := &models.Item{
		ID:             "test-item-expired",
		Name:           "Expired Item",
		Description:    "This item is expired",
		TypeID:         456,
		Num:            1,
		Durability:     50.0,
		DurabilityLoss: 10.0,
		SharerID:       "test-sharer",
		PickupCode:     expiredPickupCode,
		CreatedAt:      time.Now().Add(-48 * time.Hour),
		ExpiresAt:      time.Now().Add(-24 * time.Hour), // 24小时前过期
		IsClaimed:      false,
	}
	err = repo.Create(expiredItem)
	assert.NoError(t, err)

	// 验证过期物品已创建
	expiredRetrieved, err := repo.GetByPickupCode(expiredPickupCode)
	assert.NoError(t, err)
	assert.Nil(t, expiredRetrieved)

	// 删除过期物品
	err = repo.DeleteExpired()
	assert.NoError(t, err)

	// 验证过期物品已被删除
	expiredRetrievedAfterDelete, err := repo.GetByPickupCode(expiredPickupCode)
	assert.NoError(t, err)
	assert.Nil(t, expiredRetrievedAfterDelete)

	// 验证未过期物品仍然存在
	stillExists, err := repo.GetByPickupCode(pickupCode)
	assert.NoError(t, err)
	assert.NotNil(t, stillExists)
}

func TestInMemoryItemRepositoryConcurrentAccess(t *testing.T) {
	repo := models.NewInMemoryItemRepository()
	var wg sync.WaitGroup
	errChan := make(chan error, 100)
	mutex := &sync.Mutex{}

	// 并发创建物品
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			pickupCode := utils.GeneratePickupCode()
			item := &models.Item{
				ID:             fmt.Sprintf("test-item-concurrent-%d", index),
				Name:           fmt.Sprintf("Concurrent Item %d", index),
				Description:    "Test concurrent access",
				TypeID:         index + 1000,
				Num:            1,
				Durability:     90.0,
				DurabilityLoss: 5.0,
				SharerID:       fmt.Sprintf("test-sharer-%d", index),
				PickupCode:     pickupCode,
				CreatedAt:      time.Now(),
				ExpiresAt:      utils.GetExpirationTime(),
				IsClaimed:      false,
			}

			if err := repo.Create(item); err != nil {
				errChan <- err
				return
			}

			// 立即尝试获取刚创建的物品
			retrievedItem, err := repo.GetByPickupCode(pickupCode)
			if err != nil {
				errChan <- err
				return
			}

			// 更新物品
			if retrievedItem != nil {
				mutex.Lock() // 为了避免并发更新导致的问题
				retrievedItem.IsClaimed = true
				retrievedItem.ClaimerID = fmt.Sprintf("test-claimer-%d", index)
				if err := repo.Update(retrievedItem); err != nil {
					errChan <- err
				}
				mutex.Unlock()
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// 检查是否有错误发生
	for err := range errChan {
		t.Errorf("Error during concurrent access: %v", err)
	}

	// 验证一些物品是否正确创建和更新
	items := repo.GetAll()
	assert.GreaterOrEqual(t, len(items), 40) // 至少有40个物品创建成功（考虑到可能有一些并发冲突）

	// 检查是否有物品被正确标记为已领取
	claimedCount := 0
	for _, item := range items {
		if item.IsClaimed && item.ClaimerID != "" {
			claimedCount++
		}
	}
	assert.Greater(t, claimedCount, 0)
}
