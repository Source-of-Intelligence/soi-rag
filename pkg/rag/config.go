package rag

import (
	"github.com/Source-of-Intelligence/soi-rag/pkg/llm"
)

// StorageType 存储类型
type StorageType string

const (
	StorageMemory   StorageType = "memory"
	StorageSQLite   StorageType = "sqlite"
	StoragePostgres StorageType = "postgres"
)

// Config RAG引擎配置
type Config struct {
	ChunkSize      int
	ChunkOverlap   int
	ChunkStrategy  string
	TopK           int
	UseReranker    bool
	UseHybrid      bool
	UseDedup       bool
	StorageType    StorageType
	SQLitePath     string
	PostgresConfig *PostgresConfig
	LLM            llm.LLM
}

// PostgresConfig PostgreSQL配置
type PostgresConfig struct {
	Host     string
	Port     int
	DBName   string
	User     string
	Password string
	SSLMode  string
	MaxOpen  int
	MaxIdle  int
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		ChunkSize:     512,
		ChunkOverlap:  50,
		ChunkStrategy: "recursive",
		TopK:          10,
		UseReranker:   false,
		UseHybrid:     true,
		UseDedup:      true,
		StorageType:   StorageMemory,
	}
}

// QueryRequest 查询请求
type QueryRequest struct {
	Query         string
	TopK          int
	RetrievalType string
	UseRerank     bool
}

// QueryResponse 查询响应
type QueryResponse struct {
	Query     string
	Answer    string
	Results   []*QueryResult
	Total     int
	QueryTime int64
}

// QueryResult 查询结果
type QueryResult struct {
	ID         string
	Content    string
	Source     string
	DocumentID string
	Score      float64
	Metadata   map[string]interface{}
}
