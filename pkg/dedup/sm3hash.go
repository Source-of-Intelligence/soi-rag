package dedup

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/Source-of-Intelligence/soi-rag/pkg/resource"
)

// HashResult 哈希计算结果
type HashResult struct {
	Hash      string // SM3哈希值（十六进制字符串）
	Size      int64  // 文件大小（字节）
	Algorithm string // 哈希算法名称
}

// CalculateHash 计算数据的SM3哈希
func CalculateHash(data []byte) string {
	hash := resource.Sm3Sum(data)
	return hex.EncodeToString(hash)
}

// CalculateHashFromReader 从Reader计算SM3哈希（流式处理，适合大文件）
func CalculateHashFromReader(reader io.Reader) (*HashResult, error) {
	sm3 := resource.New()
	buf := make([]byte, 4096) // 4KB缓冲区
	var totalSize int64

	for {
		n, err := reader.Read(buf)
		if n > 0 {
			totalSize += int64(n)
			sm3.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取数据失败: %w", err)
		}
	}

	hash := sm3.Sum(nil)
	return &HashResult{
		Hash:      hex.EncodeToString(hash),
		Size:      totalSize,
		Algorithm: "SM3",
	}, nil
}

// CalculateFileHash 计算文件的SM3哈希
func CalculateFileHash(filePath string) (*HashResult, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	// 获取文件信息
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("获取文件信息失败: %w", err)
	}

	result, err := CalculateHashFromReader(file)
	if err != nil {
		return nil, err
	}

	// 验证大小
	if result.Size != info.Size() {
		return nil, fmt.Errorf("文件大小不匹配: expected %d, got %d", info.Size(), result.Size)
	}

	return result, nil
}

// CalculateStringHash 计算字符串的SM3哈希
func CalculateStringHash(text string) string {
	return CalculateHash([]byte(text))
}

// VerifyHash 验证数据哈希是否匹配
func VerifyHash(data []byte, expectedHash string) bool {
	actualHash := CalculateHash(data)
	return actualHash == expectedHash
}

// VerifyFileHash 验证文件哈希是否匹配
func VerifyFileHash(filePath, expectedHash string) (bool, error) {
	result, err := CalculateFileHash(filePath)
	if err != nil {
		return false, err
	}
	return result.Hash == expectedHash, nil
}

// HashCache 哈希缓存（用于加速重复计算）
type HashCache struct {
	cache map[string]string // filePath -> hash
}

// NewHashCache 创建哈希缓存
func NewHashCache() *HashCache {
	return &HashCache{
		cache: make(map[string]string),
	}
}

// Get 获取缓存的哈希
func (c *HashCache) Get(filePath string) (string, bool) {
	hash, ok := c.cache[filePath]
	return hash, ok
}

// Set 设置缓存哈希
func (c *HashCache) Set(filePath, hash string) {
	c.cache[filePath] = hash
}

// GetOrCalculate 获取或计算哈希
func (c *HashCache) GetOrCalculate(filePath string) (string, error) {
	// 先查缓存
	if hash, ok := c.Get(filePath); ok {
		return hash, nil
	}

	// 计算哈希
	result, err := CalculateFileHash(filePath)
	if err != nil {
		return "", err
	}

	// 缓存结果
	c.Set(filePath, result.Hash)
	return result.Hash, nil
}

// Clear 清除缓存
func (c *HashCache) Clear() {
	c.cache = make(map[string]string)
}

// Size 返回缓存大小
func (c *HashCache) Size() int {
	return len(c.cache)
}
