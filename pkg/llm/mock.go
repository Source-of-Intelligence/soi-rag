package llm

import (
	"context"
	"fmt"
)

// MockLLM 模拟LLM（用于测试）
type MockLLM struct {
	defaultCfg *GenerateConfig
	responses  map[string]string // 预设响应
}

// NewMockLLM 创建Mock LLM
func NewMockLLM() *MockLLM {
	return &MockLLM{
		defaultCfg: DefaultGenerateConfig(),
		responses:  make(map[string]string),
	}
}

// SetResponse 设置预设响应
func (l *MockLLM) SetResponse(prompt string, response string) {
	l.responses[prompt] = response
}

// Generate 生成回答
func (l *MockLLM) Generate(ctx context.Context, prompt string, opts ...GenerateOption) (string, error) {
	// 检查是否有预设响应
	if resp, ok := l.responses[prompt]; ok {
		return resp, nil
	}

	// 默认响应：返回提示词的摘要
	return fmt.Sprintf("[Mock响应] 收到提示: %s", truncate(prompt, 100)), nil
}

// GenerateStream 流式生成回答
func (l *MockLLM) GenerateStream(ctx context.Context, prompt string, callback func(string), opts ...GenerateOption) error {
	resp, err := l.Generate(ctx, prompt, opts...)
	if err != nil {
		return err
	}

	// 逐字符流式输出
	for _, c := range resp {
		callback(string(c))
	}

	return nil
}

// Name 返回名称
func (l *MockLLM) Name() string {
	return "mock"
}

// truncate 截断字符串
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
