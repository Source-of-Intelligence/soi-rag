package dedup

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
)

// DedupResult 去重检查结果
type DedupResult struct {
	IsDuplicate bool             `json:"is_duplicate"` // 是否重复
	Hash        string           `json:"hash"`         // SM3哈希值
	ExistingDoc *models.Document `json:"existing_doc"` // 已存在的文档（如果重复）
	Skipped     bool             `json:"skipped"`      // 是否跳过索引
}

// Service 去重服务
type Service struct {
	store   DedupStore
	cache   *HashCache
	enabled bool
}

// NewService 创建去重服务
func NewService(store DedupStore, enabled bool) *Service {
	return &Service{
		store:   store,
		cache:   NewHashCache(),
		enabled: enabled,
	}
}

// CheckAndDedup 检查并去重
func (s *Service) CheckAndDedup(ctx context.Context, content []byte) (*DedupResult, error) {
	// 计算哈希
	hash := CalculateHash(content)

	result := &DedupResult{
		Hash:    hash,
		Skipped: false,
	}

	// 如果去重未启用，直接返回
	if !s.enabled {
		return result, nil
	}

	// 检查哈希是否存在
	exists, existingDoc, err := s.store.ExistsByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("检查哈希失败: %w", err)
	}

	if exists {
		result.IsDuplicate = true
		result.ExistingDoc = existingDoc
		result.Skipped = true
	}

	return result, nil
}

// CheckFileAndDedup 检查文件并去重
func (s *Service) CheckFileAndDedup(ctx context.Context, filePath string) (*DedupResult, error) {
	// 计算文件哈希
	hashResult, err := CalculateFileHash(filePath)
	if err != nil {
		return nil, fmt.Errorf("计算文件哈希失败: %w", err)
	}

	result := &DedupResult{
		Hash:    hashResult.Hash,
		Skipped: false,
	}

	// 如果去重未启用，直接返回
	if !s.enabled {
		return result, nil
	}

	// 检查哈希是否存在
	exists, existingDoc, err := s.store.ExistsByHash(ctx, hashResult.Hash)
	if err != nil {
		return nil, fmt.Errorf("检查哈希失败: %w", err)
	}

	if exists {
		result.IsDuplicate = true
		result.ExistingDoc = existingDoc
		result.Skipped = true
	}

	return result, nil
}

// CheckReaderAndDedup 从Reader检查并去重
func (s *Service) CheckReaderAndDedup(ctx context.Context, reader io.Reader) (*DedupResult, []byte, error) {
	// 读取全部内容并计算哈希
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, nil, fmt.Errorf("读取数据失败: %w", err)
	}

	result, err := s.CheckAndDedup(ctx, data)
	if err != nil {
		return nil, nil, err
	}

	return result, data, nil
}

// AddDocumentWithDedup 添加文档（带去重）
func (s *Service) AddDocumentWithDedup(ctx context.Context, doc *models.Document) (*DedupResult, error) {
	// 检查去重
	result, err := s.CheckAndDedup(ctx, []byte(doc.Content))
	if err != nil {
		return nil, err
	}

	// 如果重复，跳过
	if result.IsDuplicate {
		return result, nil
	}

	// 添加文档
	if err := s.store.CreateDocumentWithHash(ctx, doc, result.Hash); err != nil {
		return nil, fmt.Errorf("创建文档失败: %w", err)
	}

	return result, nil
}

// AddFileWithDedup 添加文件（带去重）
func (s *Service) AddFileWithDedup(ctx context.Context, filePath string, doc *models.Document) (*DedupResult, error) {
	// 检查文件去重
	result, err := s.CheckFileAndDedup(ctx, filePath)
	if err != nil {
		return nil, err
	}

	// 如果重复，跳过
	if result.IsDuplicate {
		return result, nil
	}

	// 读取文件内容
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	doc.Content = string(content)

	// 添加文档
	if err := s.store.CreateDocumentWithHash(ctx, doc, result.Hash); err != nil {
		return nil, fmt.Errorf("创建文档失败: %w", err)
	}

	return result, nil
}

// GetByHash 通过哈希获取文档
func (s *Service) GetByHash(ctx context.Context, hash string) (*models.Document, error) {
	return s.store.GetByHash(ctx, hash)
}

// DeleteDocument 删除文档
func (s *Service) DeleteDocument(ctx context.Context, docID string) error {
	return s.store.DeleteDocument(ctx, docID)
}

// IsEnabled 是否启用去重
func (s *Service) IsEnabled() bool {
	return s.enabled
}

// SetEnabled 设置是否启用去重
func (s *Service) SetEnabled(enabled bool) {
	s.enabled = enabled
}

// GetStats 获取统计信息
func (s *Service) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})
	stats["enabled"] = s.enabled
	stats["cache_size"] = s.cache.Size()

	if memStore, ok := s.store.(*InMemoryDedupStore); ok {
		stats["store"] = memStore.GetStats()
	}

	return stats
}

// ClearCache 清除缓存
func (s *Service) ClearCache() {
	s.cache.Clear()
}

// SetStore 设置去重存储
func (s *Service) SetStore(store DedupStore) {
	s.store = store
}

// GetStore 获取去重存储
func (s *Service) GetStore() DedupStore {
	return s.store
}
