package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OllamaLLM Ollama本地LLM实现
type OllamaLLM struct {
	baseURL    string
	model      string
	httpClient *http.Client
	defaultCfg *GenerateConfig
}

// OllamaConfig Ollama配置
type OllamaConfig struct {
	BaseURL string // Ollama服务地址，默认 http://localhost:11434
	Model   string // 模型名称
	Timeout time.Duration
}

// NewOllamaLLM 创建Ollama LLM
func NewOllamaLLM(cfg *OllamaConfig) *OllamaLLM {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 120 * time.Second // Ollama可能较慢
	}

	return &OllamaLLM{
		baseURL:    cfg.BaseURL,
		model:      cfg.Model,
		httpClient: &http.Client{Timeout: cfg.Timeout},
		defaultCfg: DefaultGenerateConfig(),
	}
}

// ollamaRequest Ollama请求结构
type ollamaRequest struct {
	Model   string        `json:"model"`
	Prompt  string        `json:"prompt"`
	Stream  bool          `json:"stream"`
	Options ollamaOptions `json:"options,omitempty"`
}

type ollamaOptions struct {
	Temperature float64  `json:"temperature,omitempty"`
	NumPredict  int      `json:"num_predict,omitempty"`
	TopP        float64  `json:"top_p,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

// ollamaResponse Ollama响应结构
type ollamaResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Response  string `json:"response"`
	Done      bool   `json:"done"`
	Context   []int  `json:"context,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Generate 生成回答
func (l *OllamaLLM) Generate(ctx context.Context, prompt string, opts ...GenerateOption) (string, error) {
	cfg := l.defaultCfg.ApplyOptions(opts...)

	req := &ollamaRequest{
		Model:  l.model,
		Prompt: prompt,
		Stream: false,
		Options: ollamaOptions{
			Temperature: cfg.Temperature,
			NumPredict:  cfg.MaxTokens,
			TopP:        cfg.TopP,
			Stop:        cfg.StopWords,
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", l.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := l.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Ollama返回错误状态码: %d", httpResp.StatusCode)
	}

	// Ollama即使stream=false也会返回多行JSON，最后一行包含done=true
	var fullResponse string
	decoder := json.NewDecoder(httpResp.Body)

	for {
		var resp ollamaResponse
		if err := decoder.Decode(&resp); err != nil {
			break // EOF
		}

		if resp.Error != "" {
			return "", fmt.Errorf("Ollama错误: %s", resp.Error)
		}

		fullResponse += resp.Response

		if resp.Done {
			break
		}
	}

	return fullResponse, nil
}

// GenerateStream 流式生成回答
func (l *OllamaLLM) GenerateStream(ctx context.Context, prompt string, callback func(string), opts ...GenerateOption) error {
	cfg := l.defaultCfg.ApplyOptions(opts...)

	req := &ollamaRequest{
		Model:  l.model,
		Prompt: prompt,
		Stream: true,
		Options: ollamaOptions{
			Temperature: cfg.Temperature,
			NumPredict:  cfg.MaxTokens,
			TopP:        cfg.TopP,
			Stop:        cfg.StopWords,
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", l.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := l.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("Ollama返回错误状态码: %d", httpResp.StatusCode)
	}

	decoder := json.NewDecoder(httpResp.Body)

	for {
		var resp ollamaResponse
		if err := decoder.Decode(&resp); err != nil {
			break // EOF
		}

		if resp.Error != "" {
			return fmt.Errorf("Ollama错误: %s", resp.Error)
		}

		if resp.Response != "" {
			callback(resp.Response)
		}

		if resp.Done {
			break
		}
	}

	return nil
}

// Name 返回名称
func (l *OllamaLLM) Name() string {
	return fmt.Sprintf("ollama:%s", l.model)
}
