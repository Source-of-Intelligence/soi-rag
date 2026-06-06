package pageindex

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
	"github.com/google/uuid"
)

// PageIndex 页面索引接口
type PageIndex interface {
	// 添加文档
	AddDocument(ctx context.Context, doc *models.Document, content io.Reader) error

	// 批量添加文档
	BatchAddDocuments(ctx context.Context, docs []*models.Document) error

	// 更新文档
	UpdateDocument(ctx context.Context, docID string, content io.Reader) error

	// 删除文档
	DeleteDocument(ctx context.Context, docID string) error

	// 获取文档
	GetDocument(ctx context.Context, docID string) (*models.Document, error)

	// 列出文档
	ListDocuments(ctx context.Context, offset, limit int) ([]*models.Document, error)

	// 搜索文档
	Search(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error)

	// 获取文档分块
	GetChunks(ctx context.Context, docID string) ([]*models.Chunk, error)
}

// Config PageIndex配置
type Config struct {
	ChunkSize     int
	ChunkOverlap  int
	ChunkStrategy string // "fixed", "recursive", "semantic"
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		ChunkSize:     512,
		ChunkOverlap:  50,
		ChunkStrategy: "recursive",
	}
}

// pageIndex PageIndex实现
type pageIndex struct {
	store         Store
	parserManager *ParserManager
	chunker       Chunker
	config        *Config
}

// NewPageIndex 创建PageIndex实例
func NewPageIndex(store Store, config *Config) PageIndex {
	if config == nil {
		config = DefaultConfig()
	}

	// 创建分块器
	var chunker Chunker
	switch config.ChunkStrategy {
	case "fixed":
		chunker = NewFixedSizeChunker(config.ChunkSize, config.ChunkOverlap)
	case "semantic":
		chunker = NewSemanticChunker(config.ChunkSize)
	default: // recursive
		chunker = NewRecursiveChunker(config.ChunkSize, config.ChunkOverlap)
	}

	return &pageIndex{
		store:         store,
		parserManager: NewParserManager(),
		chunker:       chunker,
		config:        config,
	}
}

// AddDocument 添加文档
func (p *pageIndex) AddDocument(ctx context.Context, doc *models.Document, content io.Reader) error {
	// 如果没有提供ID，生成一个
	if doc.ID == "" {
		doc.ID = uuid.New().String()
	}

	// 设置初始状态
	doc.Status = models.DocStatusProcessing
	doc.Version = 1
	doc.CreatedAt = time.Now()
	doc.UpdatedAt = time.Now()

	// 如果提供了内容读取器，解析内容
	if content != nil {
		// 检测文档类型
		if doc.DocType == "" {
			doc.DocType = DetectDocType(doc.Source)
		}

		// 解析文档
		parser, err := p.parserManager.GetParser(doc.DocType)
		if err != nil {
			// 使用文本解析器作为后备
			log.Printf("[WARN] 未找到文档类型 %q 的解析器，回退到文本解析器 (source=%s)", doc.DocType, doc.Source)
			parser = NewTextParser()
		}

		parsedDoc, err := parser.Parse(content, doc.Source)
		if err != nil {
			doc.Status = models.DocStatusFailed
			return fmt.Errorf("解析文档失败: %w", err)
		}

		// 合并解析结果
		if doc.Title == "" {
			doc.Title = parsedDoc.Title
		}
		doc.Content = parsedDoc.Content
		if doc.Metadata == nil {
			doc.Metadata = parsedDoc.Metadata
		} else {
			// 合并元数据
			for k, v := range parsedDoc.Metadata {
				if _, exists := doc.Metadata[k]; !exists {
					doc.Metadata[k] = v
				}
			}
		}
	}

	// 保存文档
	if err := p.store.CreateDocument(ctx, doc); err != nil {
		return fmt.Errorf("保存文档失败: %w", err)
	}

	// 分块处理
	if doc.Content != "" {
		chunks, err := p.chunker.Chunk(doc.Content)
		if err != nil {
			return fmt.Errorf("分块失败: %w", err)
		}

		// 设置分块的文档ID
		for _, chunk := range chunks {
			chunk.DocumentID = doc.ID
		}

		// 保存分块
		if err := p.store.CreateChunks(ctx, chunks); err != nil {
			return fmt.Errorf("保存分块失败: %w", err)
		}
	}

	// 更新状态为已索引
	doc.Status = models.DocStatusIndexed
	if err := p.store.UpdateDocument(ctx, doc); err != nil {
		return fmt.Errorf("更新文档状态失败: %w", err)
	}

	return nil
}

// BatchAddDocuments 批量添加文档
func (p *pageIndex) BatchAddDocuments(ctx context.Context, docs []*models.Document) error {
	for _, doc := range docs {
		if err := p.AddDocument(ctx, doc, nil); err != nil {
			return fmt.Errorf("批量添加文档失败 [%s]: %w", doc.ID, err)
		}
	}
	return nil
}

// UpdateDocument 更新文档
func (p *pageIndex) UpdateDocument(ctx context.Context, docID string, content io.Reader) error {
	// 获取现有文档
	doc, err := p.store.GetDocument(ctx, docID)
	if err != nil {
		return fmt.Errorf("获取文档失败: %w", err)
	}

	// 更新状态
	doc.Status = models.DocStatusProcessing
	doc.UpdatedAt = time.Now()

	// 如果提供了新内容
	if content != nil {
		parser, err := p.parserManager.GetParser(doc.DocType)
		if err != nil {
			parser = NewTextParser()
		}

		parsedDoc, err := parser.Parse(content, doc.Source)
		if err != nil {
			doc.Status = models.DocStatusFailed
			return fmt.Errorf("解析文档失败: %w", err)
		}

		doc.Content = parsedDoc.Content
		if doc.Title == "" {
			doc.Title = parsedDoc.Title
		}
	}

	// 更新文档
	if err := p.store.UpdateDocument(ctx, doc); err != nil {
		return fmt.Errorf("更新文档失败: %w", err)
	}

	// 删除旧分块
	if err := p.store.DeleteChunksByDocument(ctx, docID); err != nil {
		return fmt.Errorf("删除旧分块失败: %w", err)
	}

	// 重新分块
	if doc.Content != "" {
		chunks, err := p.chunker.Chunk(doc.Content)
		if err != nil {
			return fmt.Errorf("分块失败: %w", err)
		}

		for _, chunk := range chunks {
			chunk.DocumentID = doc.ID
		}

		if err := p.store.CreateChunks(ctx, chunks); err != nil {
			return fmt.Errorf("保存分块失败: %w", err)
		}
	}

	// 更新状态
	doc.Status = models.DocStatusIndexed
	if err := p.store.UpdateDocument(ctx, doc); err != nil {
		return fmt.Errorf("更新文档状态失败: %w", err)
	}

	return nil
}

// DeleteDocument 删除文档
func (p *pageIndex) DeleteDocument(ctx context.Context, docID string) error {
	// 删除分块
	if err := p.store.DeleteChunksByDocument(ctx, docID); err != nil {
		return fmt.Errorf("删除分块失败: %w", err)
	}

	// 删除文档
	if err := p.store.DeleteDocument(ctx, docID); err != nil {
		return fmt.Errorf("删除文档失败: %w", err)
	}

	return nil
}

// GetDocument 获取文档
func (p *pageIndex) GetDocument(ctx context.Context, docID string) (*models.Document, error) {
	return p.store.GetDocument(ctx, docID)
}

// ListDocuments 列出文档
func (p *pageIndex) ListDocuments(ctx context.Context, offset, limit int) ([]*models.Document, error) {
	return p.store.ListDocuments(ctx, offset, limit)
}

// Search 搜索文档
func (p *pageIndex) Search(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error) {
	return p.store.Search(ctx, query, opts)
}

// GetChunks 获取文档分块
func (p *pageIndex) GetChunks(ctx context.Context, docID string) ([]*models.Chunk, error) {
	return p.store.GetChunksByDocument(ctx, docID)
}

// IndexFromText 从文本创建索引（便捷方法）
func (p *pageIndex) IndexFromText(ctx context.Context, title, content, source string) (*models.Document, error) {
	doc := &models.Document{
		Title:   title,
		Content: content,
		Source:  source,
		DocType: models.DocTypeText,
	}

	if err := p.AddDocument(ctx, doc, nil); err != nil {
		return nil, err
	}

	return doc, nil
}

// IndexFromFile 从文件创建索引（便捷方法）
func (p *pageIndex) IndexFromFile(ctx context.Context, filePath string, content io.Reader) (*models.Document, error) {
	doc := &models.Document{
		Source: filePath,
	}

	if err := p.AddDocument(ctx, doc, content); err != nil {
		return nil, err
	}

	return doc, nil
}

// SearchWithHighlight 带高亮的搜索（简化版）
func (p *pageIndex) SearchWithHighlight(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error) {
	result, err := p.store.Search(ctx, query, opts)
	if err != nil {
		return nil, err
	}

	// 添加高亮
	for _, item := range result.Results {
		item.Content = highlightText(item.Content, query)
	}

	return result, nil
}

// highlightText 高亮文本
func highlightText(text, query string) string {
	if query == "" {
		return text
	}

	// 简单的关键词高亮
	highlighted := text
	keywords := strings.Fields(query)
	for _, keyword := range keywords {
		if len(keyword) < 2 {
			continue
		}
		// 使用简单的标记进行高亮
		highlighted = strings.ReplaceAll(highlighted, keyword, "**"+keyword+"**")
	}

	return highlighted
}
