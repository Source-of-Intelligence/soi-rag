package llm

import (
	"context"
	"testing"
)

func TestMockLLM_Generate(t *testing.T) {
	mock := NewMockLLM()
	ctx := context.Background()

	// 无预设响应时返回默认格式
	resp, err := mock.Generate(ctx, "Hello")
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	if resp == "" {
		t.Error("响应不应为空")
	}
}

func TestMockLLM_SetResponse(t *testing.T) {
	mock := NewMockLLM()
	mock.SetResponse("test prompt", "expected response")

	ctx := context.Background()
	resp, err := mock.Generate(ctx, "test prompt")
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	if resp != "expected response" {
		t.Errorf("应返回预设响应 expected response，实际为 %s", resp)
	}
}

func TestMockLLM_Name(t *testing.T) {
	mock := NewMockLLM()
	if mock.Name() != "mock" {
		t.Errorf("Name 应为 mock，实际为 %s", mock.Name())
	}
}

func TestMockLLM_GenerateStream(t *testing.T) {
	mock := NewMockLLM()
	mock.SetResponse("test", "streaming response")

	ctx := context.Background()
	var result string
	err := mock.GenerateStream(ctx, "test", func(s string) {
		result += s
	})
	if err != nil {
		t.Fatalf("GenerateStream 失败: %v", err)
	}
	if result != "streaming response" {
		t.Errorf("流式响应应为 streaming response，实际为 %s", result)
	}
}

func TestDefaultGenerateConfig(t *testing.T) {
	cfg := DefaultGenerateConfig()
	if cfg.Temperature != 0.7 {
		t.Errorf("默认 Temperature 应为 0.7，实际为 %f", cfg.Temperature)
	}
	if cfg.MaxTokens != 2048 {
		t.Errorf("默认 MaxTokens 应为 2048，实际为 %d", cfg.MaxTokens)
	}
}

func TestGenerateConfig_Clone(t *testing.T) {
	cfg := DefaultGenerateConfig()
	cfg.Temperature = 0.5
	cfg.MaxTokens = 1000

	clone := cfg.Clone()
	clone.Temperature = 0.9

	// 克隆后修改不影响原对象
	if cfg.Temperature != 0.5 {
		t.Errorf("克隆后修改不应影响原对象，Temperature 应为 0.5")
	}
}

func TestGenerateConfig_ApplyOptions(t *testing.T) {
	cfg := DefaultGenerateConfig()
	cfg.Temperature = 0.5

	newCfg := cfg.ApplyOptions(WithTemperature(0.9), WithMaxTokens(500))

	// 新配置应有新值
	if newCfg.Temperature != 0.9 {
		t.Errorf("新配置 Temperature 应为 0.9，实际为 %f", newCfg.Temperature)
	}
	if newCfg.MaxTokens != 500 {
		t.Errorf("新配置 MaxTokens 应为 500，实际为 %d", newCfg.MaxTokens)
	}
	// 原配置不变
	if cfg.Temperature != 0.5 {
		t.Errorf("原配置 Temperature 应保持 0.5")
	}
}

func TestWithStopWords(t *testing.T) {
	cfg := DefaultGenerateConfig()
	newCfg := cfg.ApplyOptions(WithStopWords([]string{"STOP", "END"}))

	if len(newCfg.StopWords) != 2 {
		t.Errorf("StopWords 长度应为 2，实际为 %d", len(newCfg.StopWords))
	}
	if newCfg.StopWords[0] != "STOP" {
		t.Errorf("StopWords[0] 应为 STOP，实际为 %s", newCfg.StopWords[0])
	}
}

func TestWithTopP(t *testing.T) {
	cfg := DefaultGenerateConfig()
	newCfg := cfg.ApplyOptions(WithTopP(0.8))

	if newCfg.TopP != 0.8 {
		t.Errorf("TopP 应为 0.8，实际为 %f", newCfg.TopP)
	}
}
