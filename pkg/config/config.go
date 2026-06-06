package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Source-of-Intelligence/soi-rag/pkg/llm"
	"github.com/Source-of-Intelligence/soi-rag/pkg/rag"
)

// validModelNamePattern 验证模型名称格式（允许字母、数字、点、冒号、连字符、下划线）
var validModelNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._:\-]+$`)

// FileConfig 完整的配置文件结构
type FileConfig struct {
	Engine    EngineConfig     `yaml:"engine"`
	LLM       LLMConfig        `yaml:"llm"`
	Storage   StorageConfig    `yaml:"storage"`
	API       APIConfig        `yaml:"api"`
	Eval      EvalConfig       `yaml:"eval,omitempty"`      // 检索质量评估配置
	Threshold QualityThreshold `yaml:"threshold,omitempty"` // 质量检验阈值
}

// EngineConfig RAG引擎配置
type EngineConfig struct {
	ChunkSize        int      `yaml:"chunk_size"`
	ChunkOverlap     int      `yaml:"chunk_overlap"`
	ChunkStrategy    string   `yaml:"chunk_strategy"`
	TopK             int      `yaml:"top_k"`
	UseReranker      bool     `yaml:"use_reranker"`
	UseHybrid        bool     `yaml:"use_hybrid"`
	UseDedup         bool     `yaml:"use_dedup"`
	HybridStrategies []string `yaml:"hybrid_strategies"`
}

// LLMConfig LLM配置
type LLMConfig struct {
	Provider    string  `yaml:"provider"`    // openai, ollama, mock
	Model       string  `yaml:"model"`       // gpt-4, llama2, etc.
	APIKey      string  `yaml:"api_key"`     // API密钥
	APIURL      string  `yaml:"api_url"`     // 自定义API地址
	Temperature float64 `yaml:"temperature"` // 生成温度
	MaxTokens   int     `yaml:"max_tokens"`  // 最大token数
}

// StorageConfig 存储配置
type StorageConfig struct {
	Type     string       `yaml:"type"` // memory, sqlite, postgres
	SQLite   SQLiteConfig `yaml:"sqlite"`
	Postgres PostgresConf `yaml:"postgres"`
}

// SQLiteConfig SQLite配置
type SQLiteConfig struct {
	Path string `yaml:"path"` // 数据库文件路径
}

// PostgresConf PostgreSQL配置
type PostgresConf struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	DBName   string `yaml:"dbname"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	SSLMode  string `yaml:"ssl_mode"`
	MaxOpen  int    `yaml:"max_open"`
	MaxIdle  int    `yaml:"max_idle"`
}

// APIConfig HTTP API配置
type APIConfig struct {
	Host string `yaml:"host"` // 监听地址
	Port int    `yaml:"port"` // 监听端口
}

// EvalConfig 检索质量评估配置
type EvalConfig struct {
	Enabled   bool    `yaml:"enabled"`    // 是否启用评估
	Ks        []int   `yaml:"ks"`         // 评估K值列表，如 [1, 5, 10, 20]
	MinRecall float64 `yaml:"min_recall"` // 最低Recall要求
}

// QualityThreshold 质量检验阈值配置
type QualityThreshold struct {
	MinRecallAt1    float64 `yaml:"min_recall_at_1"`    // Recall@1 最低阈值
	MinRecallAt5    float64 `yaml:"min_recall_at_5"`    // Recall@5 最低阈值
	MinRecallAt10   float64 `yaml:"min_recall_at_10"`   // Recall@10 最低阈值
	MinPrecisionAt5 float64 `yaml:"min_precision_at_5"` // Precision@5 最低阈值
	MinMRR          float64 `yaml:"min_mrr"`            // MRR 最低阈值
	MinNDCGAt10     float64 `yaml:"min_ndcg_at_10"`     // NDCG@10 最低阈值
	MinMAP          float64 `yaml:"min_map"`            // MAP 最低阈值
}

// DefaultFileConfig 返回默认配置
func DefaultFileConfig() *FileConfig {
	return &FileConfig{
		Engine: EngineConfig{
			ChunkSize:     512,
			ChunkOverlap:  50,
			ChunkStrategy: "recursive",
			TopK:          10,
			UseReranker:   false,
			UseHybrid:     true,
			UseDedup:      true,
		},
		LLM: LLMConfig{
			Provider:    "mock",
			Temperature: 0.7,
			MaxTokens:   2048,
		},
		Storage: StorageConfig{
			Type: "memory",
			SQLite: SQLiteConfig{
				Path: "rag.db",
			},
			Postgres: PostgresConf{
				Host:    "localhost",
				Port:    5432,
				SSLMode: "disable",
				MaxOpen: 10,
				MaxIdle: 5,
			},
		},
		API: APIConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Eval: EvalConfig{
			Enabled:   false,
			Ks:        []int{1, 5, 10, 20},
			MinRecall: 0.0,
		},
		Threshold: QualityThreshold{
			MinRecallAt1:    0.3,
			MinRecallAt5:    0.5,
			MinRecallAt10:   0.7,
			MinPrecisionAt5: 0.4,
			MinMRR:          0.4,
			MinNDCGAt10:     0.5,
			MinMAP:          0.4,
		},
	}
}

// LoadFromFile 从YAML文件加载配置
func LoadFromFile(path string) (*FileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	config := DefaultFileConfig()
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 展开环境变量
	config.expandEnv()

	return config, nil
}

// LoadFromFileWithDefaults 从文件加载，未设置的字段使用默认值
func LoadFromFileWithDefaults(path string) (*FileConfig, error) {
	if path == "" {
		return DefaultFileConfig(), nil
	}

	// 检查文件是否存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("配置文件不存在: %s", path)
	}

	return LoadFromFile(path)
}

// SaveToFile 保存配置到YAML文件
func (c *FileConfig) SaveToFile(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	// 确保目录存在
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}

// ToRagConfig 转换为RAG引擎配置
func (c *FileConfig) ToRagConfig() *rag.Config {
	config := rag.DefaultConfig()

	// 引擎配置
	if c.Engine.ChunkSize > 0 {
		config.ChunkSize = c.Engine.ChunkSize
	}
	if c.Engine.ChunkOverlap > 0 {
		config.ChunkOverlap = c.Engine.ChunkOverlap
	}
	if c.Engine.ChunkStrategy != "" {
		config.ChunkStrategy = c.Engine.ChunkStrategy
	}
	if c.Engine.TopK > 0 {
		config.TopK = c.Engine.TopK
	}
	config.UseReranker = c.Engine.UseReranker
	config.UseHybrid = c.Engine.UseHybrid
	config.UseDedup = c.Engine.UseDedup

	// 存储配置
	switch strings.ToLower(c.Storage.Type) {
	case "sqlite":
		config.StorageType = rag.StorageSQLite
		config.SQLitePath = c.Storage.SQLite.Path
		if config.SQLitePath == "" {
			config.SQLitePath = "rag.db"
		}
	case "postgres", "postgresql":
		config.StorageType = rag.StoragePostgres
		config.PostgresConfig = &rag.PostgresConfig{
			Host:     c.Storage.Postgres.Host,
			Port:     c.Storage.Postgres.Port,
			DBName:   c.Storage.Postgres.DBName,
			User:     c.Storage.Postgres.User,
			Password: c.Storage.Postgres.Password,
			SSLMode:  c.Storage.Postgres.SSLMode,
			MaxOpen:  c.Storage.Postgres.MaxOpen,
			MaxIdle:  c.Storage.Postgres.MaxIdle,
		}
	default:
		config.StorageType = rag.StorageMemory
	}

	return config
}

// ToLLM 根据配置创建LLM实例
func (c *FileConfig) ToLLM() (llm.LLM, error) {
	switch strings.ToLower(c.LLM.Provider) {
	case "openai":
		if c.LLM.APIKey == "" {
			return nil, fmt.Errorf("OpenAI provider requires api_key, please set it in config.yaml or via environment variable")
		}
		cfg := &llm.OpenAIConfig{
			APIKey:  c.LLM.APIKey,
			Model:   c.LLM.Model,
			APIURL:  c.LLM.APIURL,
			Timeout: 60 * time.Second,
		}
		if cfg.Model == "" {
			cfg.Model = "gpt-4"
		}
		return llm.NewOpenAILLM(cfg), nil

	case "ollama":
		cfg := &llm.OllamaConfig{
			Model:   c.LLM.Model,
			BaseURL: c.LLM.APIURL,
			Timeout: 120 * time.Second,
		}
		if cfg.Model == "" {
			cfg.Model = "llama3"
		}
		if cfg.BaseURL == "" {
			cfg.BaseURL = "http://localhost:11434"
		}
		return llm.NewOllamaLLM(cfg), nil

	case "mock":
		return llm.NewMockLLM(), nil

	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s (supported: openai, ollama, mock)", c.LLM.Provider)
	}
}

// LoadFromReader 从 io.Reader 加载配置
func LoadFromReader(r io.Reader) (*FileConfig, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	config := DefaultFileConfig()
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	config.expandEnv()
	return config, nil
}

// DSN 返回 PostgreSQL 连接字符串
func (p *PostgresConf) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		p.Host, p.Port, p.User, p.Password, p.DBName, p.SSLMode)
}

// Addr 返回 API 监听地址 (host:port)
func (a *APIConfig) Addr() string {
	host := a.Host
	if host == "" {
		host = "0.0.0.0"
	}
	port := a.Port
	if port == 0 {
		port = 8080
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// Merge 合并另一个配置（非零值覆盖当前配置）
func (c *FileConfig) Merge(other *FileConfig) *FileConfig {
	if other == nil {
		return c
	}
	if other.Engine.ChunkSize > 0 {
		c.Engine.ChunkSize = other.Engine.ChunkSize
	}
	if other.Engine.ChunkOverlap > 0 {
		c.Engine.ChunkOverlap = other.Engine.ChunkOverlap
	}
	if other.Engine.ChunkStrategy != "" {
		c.Engine.ChunkStrategy = other.Engine.ChunkStrategy
	}
	if other.Engine.TopK > 0 {
		c.Engine.TopK = other.Engine.TopK
	}
	if other.LLM.Provider != "" {
		c.LLM.Provider = other.LLM.Provider
	}
	if other.LLM.Model != "" {
		c.LLM.Model = other.LLM.Model
	}
	if other.LLM.APIKey != "" {
		c.LLM.APIKey = other.LLM.APIKey
	}
	if other.LLM.APIURL != "" {
		c.LLM.APIURL = other.LLM.APIURL
	}
	if other.Storage.Type != "" {
		c.Storage.Type = other.Storage.Type
	}
	if other.API.Host != "" {
		c.API.Host = other.API.Host
	}
	if other.API.Port > 0 {
		c.API.Port = other.API.Port
	}
	return c
}

// expandEnv 展开配置中的环境变量
func (c *FileConfig) expandEnv() {
	// LLM API Key 可能来自环境变量
	if strings.HasPrefix(c.LLM.APIKey, "$") {
		c.LLM.APIKey = os.Getenv(strings.TrimPrefix(c.LLM.APIKey, "$"))
	}
	if strings.HasPrefix(c.LLM.APIKey, "${") && strings.HasSuffix(c.LLM.APIKey, "}") {
		c.LLM.APIKey = os.Getenv(strings.TrimSuffix(strings.TrimPrefix(c.LLM.APIKey, "${"), "}"))
	}

	// PostgreSQL 密码可能来自环境变量
	if strings.HasPrefix(c.Storage.Postgres.Password, "$") {
		c.Storage.Postgres.Password = os.Getenv(strings.TrimPrefix(c.Storage.Postgres.Password, "$"))
	}
	if strings.HasPrefix(c.Storage.Postgres.Password, "${") && strings.HasSuffix(c.Storage.Postgres.Password, "}") {
		c.Storage.Postgres.Password = os.Getenv(strings.TrimSuffix(strings.TrimPrefix(c.Storage.Postgres.Password, "${"), "}"))
	}
}

// Validate 验证配置有效性
func (c *FileConfig) Validate() error {
	// 验证存储配置
	if c.Storage.Type == "postgres" || c.Storage.Type == "postgresql" {
		if c.Storage.Postgres.DBName == "" {
			return fmt.Errorf("PostgreSQL配置缺少dbname")
		}
		if c.Storage.Postgres.User == "" {
			return fmt.Errorf("PostgreSQL配置缺少user")
		}
	}

	// 验证LLM配置
	if c.LLM.Provider == "openai" && c.LLM.APIKey == "" {
		return fmt.Errorf("OpenAI配置缺少api_key")
	}

	// 验证LLM Provider
	validProviders := map[string]bool{"openai": true, "ollama": true, "mock": true}
	if !validProviders[strings.ToLower(c.LLM.Provider)] {
		return fmt.Errorf("不支持的LLM provider: %s (支持: openai, ollama, mock)", c.LLM.Provider)
	}

	// 验证模型名称格式
	if c.LLM.Model != "" && !validModelNamePattern.MatchString(c.LLM.Model) {
		return fmt.Errorf("模型名称格式无效: %q (仅允许字母、数字、点、冒号、连字符、下划线)", c.LLM.Model)
	}

	// 验证引擎参数
	if c.Engine.ChunkSize < 0 {
		return fmt.Errorf("chunk_size 不能为负数")
	}
	if c.Engine.ChunkOverlap < 0 {
		return fmt.Errorf("chunk_overlap 不能为负数")
	}
	if c.Engine.ChunkOverlap >= c.Engine.ChunkSize && c.Engine.ChunkSize > 0 {
		return fmt.Errorf("chunk_overlap (%d) 不能大于等于 chunk_size (%d)", c.Engine.ChunkOverlap, c.Engine.ChunkSize)
	}
	if c.Engine.TopK < 0 {
		return fmt.Errorf("top_k 不能为负数")
	}

	// 验证存储类型
	validStorageTypes := map[string]bool{"memory": true, "sqlite": true, "postgres": true, "postgresql": true}
	if !validStorageTypes[strings.ToLower(c.Storage.Type)] {
		return fmt.Errorf("不支持的存储类型: %s (支持: memory, sqlite, postgres)", c.Storage.Type)
	}

	// 验证API端口
	if c.API.Port < 0 || c.API.Port > 65535 {
		return fmt.Errorf("API端口无效: %d (范围: 0-65535)", c.API.Port)
	}

	return nil
}

// String 返回配置的字符串表示（隐藏敏感信息）
func (c *FileConfig) String() string {
	// 复制配置，隐藏敏感字段
	copy := *c
	if copy.LLM.APIKey != "" {
		copy.LLM.APIKey = "***"
	}
	if copy.Storage.Postgres.Password != "" {
		copy.Storage.Postgres.Password = "***"
	}

	data, _ := yaml.Marshal(&copy)
	return string(data)
}
