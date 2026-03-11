// Package keygen 提供 PairProxy API Key 的生成与验证功能。
//
// Key 格式：sk-pp-<48字符字母数字>，总长度 54 字符。
// 设计原则：Key 主体中嵌入用户名的字母数字字符（打散到随机位置），
// 其余位置用随机字母数字填充，无需数据库存储即可反向识别用户。
package keygen

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"go.uber.org/zap"
)

const (
	// KeyPrefix 是所有 PairProxy API Key 的固定前缀。
	KeyPrefix = "sk-pp-"
	// KeyBodyLen 是 Key 前缀之后的主体长度（字母数字字符）。
	KeyBodyLen = 48
	// KeyTotalLen 是 Key 的总长度（前缀 + 主体）。
	KeyTotalLen = len(KeyPrefix) + KeyBodyLen
	// Charset 是 Key 主体允许使用的字符集（仅字母和数字）。
	Charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// GenerateKey 根据用户名生成一个 API Key。
//
// 算法：
//  1. 提取用户名中的字母和数字（转小写），作为"用户指纹"字符序列
//  2. 用随机字符填满 48 字节的主体
//  3. 在主体中随机选取 len(指纹) 个不重复位置，将指纹字符写入这些位置
//  4. 拼接前缀返回
//
// 返回的 Key 可通过 ValidateAndGetUser 反向识别用户。
func GenerateKey(username string) (string, error) {
	chars := ExtractAlphanumeric(username)
	if len(chars) == 0 {
		return "", fmt.Errorf("username %q contains no alphanumeric characters", username)
	}
	if len(chars) > KeyBodyLen {
		chars = chars[:KeyBodyLen]
	}

	body := make([]byte, KeyBodyLen)
	for i := range body {
		body[i] = randomChar()
	}

	positions := randomPositions(KeyBodyLen, len(chars))
	for i, pos := range positions {
		body[pos] = chars[i]
	}

	key := KeyPrefix + string(body)
	zap.L().Debug("api key generated",
		zap.String("username", username),
		zap.Int("fingerprint_chars", len(chars)),
	)
	return key, nil
}

// ExtractAlphanumeric 提取字符串中的字母和数字，统一转为小写。
// 例如 "Alice-123" → []byte("alice123")。
// 此函数是公开的，供测试和 Validator 共用。
func ExtractAlphanumeric(s string) []byte {
	lower := strings.ToLower(s)
	var result []byte
	for _, c := range lower {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			result = append(result, byte(c))
		}
	}
	return result
}

// randomChar 从 Charset 中随机返回一个字符。
func randomChar() byte {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(Charset))))
	if err != nil {
		panic("keygen: crypto/rand failed: " + err.Error())
	}
	return Charset[n.Int64()]
}

// randomPositions 在 [0, max) 范围内生成 count 个不重复的随机下标。
// 若 count >= max，返回所有下标的随机排列。
func randomPositions(max, count int) []int {
	if count >= max {
		count = max
	}
	// Fisher-Yates 部分洗牌：只生成前 count 个元素
	perm := make([]int, max)
	for i := range perm {
		perm[i] = i
	}
	for i := 0; i < count; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(max-i)))
		if err != nil {
			panic("keygen: crypto/rand failed: " + err.Error())
		}
		j := i + int(n.Int64())
		perm[i], perm[j] = perm[j], perm[i]
	}
	return perm[:count]
}
