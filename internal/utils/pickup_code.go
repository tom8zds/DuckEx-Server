package utils

import (
	"math/rand"
	"time"
)

const (
	// 取件码长度
	pickupCodeLength = 6
	// 取件码有效期（24小时）
	expirationDuration = 24 * time.Hour
)

// 初始化随机数种子
func init() {
	rand.Seed(time.Now().UnixNano())
}

// GeneratePickupCode 生成6位数的取件码
func GeneratePickupCode() string {
	const charset = "0123456789"
	code := make([]byte, pickupCodeLength)
	for i := range code {
		code[i] = charset[rand.Intn(len(charset))]
	}
	return string(code)
}

// GetExpirationTime 获取过期时间
func GetExpirationTime() time.Time {
	return time.Now().Add(expirationDuration)
}