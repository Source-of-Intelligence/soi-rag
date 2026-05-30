package llm

import (
	"context"
)

// LLM 大语言模型接口
type LLM interface {
	// Generate 生成回答
	Generate(ctx context.Context, prompt string, opts ...GenerateOption) (string, error)

	// GenerateStream 流式生成回答
	GenerateStream(ctx context.Context, prompt string, callback func(string), opts ...GenerateOption) error

	// Name 返回LLM名称
	Name() string
}

// GenerateConfig 生成配置
type GenerateConfig struct {
	Temperature float64  // 生成温度
	MaxTokens   int      // 最大token数
	TopP        float64  // nucleus sampling
	StopWords   []string // 停止词
}

// GenerateOption 生成选项函数
type GenerateOption func(*GenerateConfig)

// WithTemperature 设置生成温度
func WithTemperature(temp float64) GenerateOption {
	return func(c *GenerateConfig) {
		c.Temperature = temp
	}
}

// WithMaxTokens 设置最大token数
func WithMaxTokens(maxTokens int) GenerateOption {
	return func(c *GenerateConfig) {
		c.MaxTokens = maxTokens
	}
}

// WithTopP 设置TopP
func WithTopP(topP float64) GenerateOption {
	return func(c *GenerateConfig) {
		c.TopP = topP
	}
}

// WithStopWords 设置停止词
func WithStopWords(stopWords []string) GenerateOption {
	return func(c *GenerateConfig) {
		c.StopWords = stopWords
	}
}

// DefaultGenerateConfig 返回默认生成配置
func DefaultGenerateConfig() *GenerateConfig {
	return &GenerateConfig{
		Temperature: 0.7,
		MaxTokens:   2048,
		TopP:        1.0,
		StopWords:   nil,
	}
}

// Clone 创建配置的副本
func (c *GenerateConfig) Clone() *GenerateConfig {
	return &GenerateConfig{
		Temperature: c.Temperature,
		MaxTokens:   c.MaxTokens,
		TopP:        c.TopP,
		StopWords:   append([]string(nil), c.StopWords...),
	}
}

// ApplyOptions 应用选项（返回新配置，不修改原始配置）
func (c *GenerateConfig) ApplyOptions(opts ...GenerateOption) *GenerateConfig {
	newCfg := c.Clone()
	for _, opt := range opts {
		opt(newCfg)
	}
	return newCfg
}
