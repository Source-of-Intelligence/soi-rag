package config

import (
	"strings"
	"testing"
)

func TestDefaultFileConfig(t *testing.T) {
	cfg := DefaultFileConfig()
	if cfg == nil {
		t.Fatal("DefaultFileConfig 返回 nil")
	}
	if cfg.Engine.ChunkSize != 512 {
		t.Errorf("默认 ChunkSize 应为 512，实际为 %d", cfg.Engine.ChunkSize)
	}
	if cfg.Engine.ChunkStrategy != "recursive" {
		t.Errorf("默认 ChunkStrategy 应为 recursive，实际为 %s", cfg.Engine.ChunkStrategy)
	}
	if cfg.LLM.Provider != "mock" {
		t.Errorf("默认 LLM.Provider 应为 mock，实际为 %s", cfg.LLM.Provider)
	}
	if cfg.Storage.Type != "memory" {
		t.Errorf("默认 Storage.Type 应为 memory，实际为 %s", cfg.Storage.Type)
	}
}

func TestFileConfig_ToRagConfig(t *testing.T) {
	cfg := DefaultFileConfig()
	cfg.Storage.Type = "sqlite"
	cfg.Storage.SQLite.Path = "test.db"

	ragCfg := cfg.ToRagConfig()
	if ragCfg.StorageType != "sqlite" {
		t.Errorf("ToRagConfig StorageType 应为 sqlite，实际为 %s", ragCfg.StorageType)
	}
	if ragCfg.SQLitePath != "test.db" {
		t.Errorf("ToRagConfig SQLitePath 应为 test.db，实际为 %s", ragCfg.SQLitePath)
	}
}

func TestFileConfig_ToLLM(t *testing.T) {
	cfg := DefaultFileConfig()

	// Test mock provider
	cfg.LLM.Provider = "mock"
	llm, err := cfg.ToLLM()
	if err != nil {
		t.Fatalf("ToLLM mock 失败: %v", err)
	}
	if llm.Name() != "mock" {
		t.Errorf("Mock LLM 名称应为 mock，实际为 %s", llm.Name())
	}
}

func TestFileConfig_Validate(t *testing.T) {
	// 有效配置
	cfg := DefaultFileConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("默认配置应验证通过，实际错误: %v", err)
	}

	// OpenAI 无 API key
	cfg2 := DefaultFileConfig()
	cfg2.LLM.Provider = "openai"
	cfg2.LLM.APIKey = ""
	if err := cfg2.Validate(); err == nil {
		t.Error("OpenAI 无 API key 应返回错误")
	}
}

func TestPostgresConf_DSN(t *testing.T) {
	cfg := &PostgresConf{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "secret",
		DBName:   "testdb",
		SSLMode:  "disable",
	}
	dsn := cfg.DSN()
	if !strings.Contains(dsn, "host=localhost") {
		t.Errorf("DSN 应包含 host=localhost，实际: %s", dsn)
	}
	if !strings.Contains(dsn, "password=secret") {
		t.Errorf("DSN 应包含 password=secret，实际: %s", dsn)
	}
}

func TestAPIConfig_Addr(t *testing.T) {
	cfg := &APIConfig{Host: "0.0.0.0", Port: 8080}
	if addr := cfg.Addr(); addr != "0.0.0.0:8080" {
		t.Errorf("Addr 应为 0.0.0.0:8080，实际为 %s", addr)
	}

	cfg2 := &APIConfig{}
	if addr := cfg2.Addr(); addr != "0.0.0.0:8080" {
		t.Errorf("空配置 Addr 应默认为 0.0.0.0:8080，实际为 %s", addr)
	}
}

func TestFileConfig_Merge(t *testing.T) {
	base := DefaultFileConfig()
	base.Engine.ChunkSize = 256

	override := &FileConfig{}
	override.Engine.ChunkSize = 1024
	override.Engine.TopK = 20
	override.LLM.Provider = "openai"

	merged := base.Merge(override)

	if merged.Engine.ChunkSize != 1024 {
		t.Errorf("Merge 后 ChunkSize 应为 1024，实际为 %d", merged.Engine.ChunkSize)
	}
	if merged.Engine.TopK != 20 {
		t.Errorf("Merge 后 TopK 应为 20，实际为 %d", merged.Engine.TopK)
	}
	if merged.LLM.Provider != "openai" {
		t.Errorf("Merge 后 LLM.Provider 应为 openai，实际为 %s", merged.LLM.Provider)
	}
	// 未覆盖的字段应保持原值
	if merged.Engine.ChunkStrategy != "recursive" {
		t.Errorf("Merge 后 ChunkStrategy 应保持 recursive，实际为 %s", merged.Engine.ChunkStrategy)
	}
}
