package utils

import (
	"crypto/rand"
	"math/big"
	"time"
)

const (
	// 取件码长度
	pickupCodeLength = 6
	// 取件码有效期（7天）
	expirationDuration = 7 * 24 * time.Hour
)

// GeneratePickupCode 生成6位数的取件码，使用加密安全的随机数生成器
func GeneratePickupCode() string {
	const charset = "0123456789"
	code := make([]byte, pickupCodeLength)
	
	// 使用crypto/rand代替math/rand，提供更好的随机性
	for i := range code {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			// 如果加密随机数生成失败，使用回退方案
			code[i] = charset[time.Now().UnixNano()%int64(len(charset))]
		} else {
			code[i] = charset[index.Int64()]
		}
	}
	
	return string(code)
}

// GetExpirationTime 获取过期时间
func GetExpirationTime() time.Time {
	return time.Now().Add(expirationDuration)
}