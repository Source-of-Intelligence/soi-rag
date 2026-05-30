package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAILLM OpenAI LLM实现
type OpenAILLM struct {
	apiKey     string
	model      string
	apiURL     string
	httpClient *http.Client
	defaultCfg *GenerateConfig
}

// OpenAIConfig OpenAI配置
type OpenAIConfig struct {
	APIKey     string
	Model      string
	APIURL     string // 可选，默认为OpenAI官方API
	Timeout    time.Duration
	MaxRetries int
}

// NewOpenAILLM 创建OpenAI LLM
func NewOpenAILLM(cfg *OpenAIConfig) *OpenAILLM {
	if cfg.APIURL == "" {
		cfg.APIURL = "https://api.openai.com/v1"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}

	return &OpenAILLM{
		apiKey:     cfg.APIKey,
		model:      cfg.Model,
		apiURL:     cfg.APIURL,
		httpClient: &http.Client{Timeout: cfg.Timeout},
		defaultCfg: DefaultGenerateConfig(),
	}
}

// openAIRequest OpenAI请求结构
type openAIRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	Stop        []string  `json:"stop,omitempty"`
	Stream      bool      `json:"stream"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIResponse OpenAI响应结构
type openAIResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []choice     `json:"choices"`
	Usage   usage        `json:"usage"`
	Error   *openAIError `json:"error,omitempty"`
}

type choice struct {
	Index        int      `json:"index"`
	Message      *message `json:"message,omitempty"`
	Delta        *message `json:"delta,omitempty"`
	FinishReason string   `json:"finish_reason"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// Generate 生成回答
func (l *OpenAILLM) Generate(ctx context.Context, prompt string, opts ...GenerateOption) (string, error) {
	cfg := l.defaultCfg.ApplyOptions(opts...)

	req := &openAIRequest{
		Model:       l.model,
		Messages:    []message{{Role: "user", Content: prompt}},
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
		TopP:        cfg.TopP,
		Stop:        cfg.StopWords,
		Stream:      false,
	}

	resp, err := l.doRequest(ctx, req)
	if err != nil {
		return "", err
	}

	if resp.Error != nil {
		return "", fmt.Errorf("OpenAI API错误: %s", resp.Error.Message)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message == nil {
		return "", fmt.Errorf("OpenAI返回空响应")
	}

	return resp.Choices[0].Message.Content, nil
}

// GenerateStream 流式生成回答
func (l *OpenAILLM) GenerateStream(ctx context.Context, prompt string, callback func(string), opts ...GenerateOption) error {
	cfg := l.defaultCfg.ApplyOptions(opts...)

	req := &openAIRequest{
		Model:       l.model,
		Messages:    []message{{Role: "user", Content: prompt}},
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
		TopP:        cfg.TopP,
		Stop:        cfg.StopWords,
		Stream:      true,
	}

	return l.doStreamRequest(ctx, req, callback)
}

// Name 返回名称
func (l *OpenAILLM) Name() string {
	return fmt.Sprintf("openai:%s", l.model)
}

// doRequest 执行HTTP请求
func (l *OpenAILLM) doRequest(ctx context.Context, req *openAIRequest) (*openAIResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", l.apiURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+l.apiKey)

	httpResp, err := l.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("API返回错误状态码 %d: %s", httpResp.StatusCode, string(respBody))
	}

	var resp openAIResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &resp, nil
}

// doStreamRequest 执行流式请求
func (l *OpenAILLM) doStreamRequest(ctx context.Context, req *openAIRequest, callback func(string)) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", l.apiURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+l.apiKey)

	httpResp, err := l.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("API返回错误状态码 %d: %s", httpResp.StatusCode, string(respBody))
	}

	// 解析SSE流
	buf := make([]byte, 4096)
	var lineBuf string

	for {
		n, err := httpResp.Body.Read(buf)
		if n > 0 {
			lineBuf += string(buf[:n])
		}
		if err != nil {
			break
		}

		// 处理完整的行
		for {
			idx := strings.Index(lineBuf, "\n")
			if idx == -1 {
				break
			}

			line := strings.TrimSpace(lineBuf[:idx])
			lineBuf = lineBuf[idx+1:]

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return nil
			}

			var resp openAIResponse
			if err := json.Unmarshal([]byte(data), &resp); err != nil {
				continue
			}

			if len(resp.Choices) > 0 && resp.Choices[0].Delta != nil {
				content := resp.Choices[0].Delta.Content
				if content != "" {
					callback(content)
				}
			}
		}
	}

	return nil
}
