package llm

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"text/template"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
)

// PromptTemplate RAG提示模板
type PromptTemplate struct {
	SystemPrompt   string // 系统提示
	ContextFormat  string // 检索结果格式化模板
	QuestionFormat string // 问题格式化模板
	NoContextMsg   string // 无上下文时的提示

	// 缓存解析后的模板，避免每次重新解析
	ctxTmplOnce      sync.Once
	ctxTmpl          *template.Template
	ctxTmplErr       error
	questionTmplOnce sync.Once
	questionTmpl     *template.Template
	questionTmplErr  error
}

// DefaultRAGPrompt 默认RAG提示模板
var DefaultRAGPrompt = &PromptTemplate{
	SystemPrompt:   "你是一个知识助手，请根据以下参考资料回答用户问题。如果资料中没有相关信息，请如实说明，不要编造内容。",
	ContextFormat:  "参考资料{{.Index}}：\n{{.Content}}\n来源：{{.Source}}",
	QuestionFormat: "用户问题：{{.Question}}",
	NoContextMsg:   "抱歉，我没有找到相关的参考资料来回答这个问题。",
}

// ChineseRAGPrompt 中文RAG提示模板
var ChineseRAGPrompt = &PromptTemplate{
	SystemPrompt:   "你是一个专业的知识助手。请根据提供的参考资料回答用户问题。要求：\n1. 优先使用参考资料中的信息\n2. 如果资料不足，请明确说明\n3. 回答要准确、简洁、有条理",
	ContextFormat:  "【资料{{.Index}}】\n内容：{{.Content}}\n来源：{{.Source}}",
	QuestionFormat: "【问题】{{.Question}}",
	NoContextMsg:   "抱歉，根据现有资料无法回答这个问题。请提供更多相关信息或尝试其他问题。",
}

// ContextItem 上下文项
type ContextItem struct {
	Index   int     // 序号
	Content string  // 内容
	Source  string  // 来源
	Score   float64 // 相关度分数
}

// BuildPrompt 构建完整提示
func (t *PromptTemplate) BuildPrompt(question string, contexts []*models.RetrievalResult) string {
	var buf bytes.Buffer

	// 系统提示
	buf.WriteString(t.SystemPrompt)
	buf.WriteString("\n\n")

	// 上下文
	if len(contexts) > 0 {
		buf.WriteString("=== 参考资料 ===\n")
		for i, ctx := range contexts {
			item := ContextItem{
				Index:   i + 1,
				Content: ctx.Content,
				Source:  ctx.Source,
				Score:   ctx.Score,
			}
			formatted := t.formatContext(item)
			buf.WriteString(formatted)
			buf.WriteString("\n\n")
		}
	}

	// 问题
	buf.WriteString("=== 问题 ===\n")
	questionFormatted := t.formatQuestion(question)
	buf.WriteString(questionFormatted)
	buf.WriteString("\n\n")

	// 回答提示
	buf.WriteString("=== 回答 ===\n")

	return buf.String()
}

// initTemplates 初始化并缓存解析后的模板
func (t *PromptTemplate) initTemplates() {
	t.ctxTmplOnce.Do(func() {
		t.ctxTmpl, t.ctxTmplErr = template.New("context").Parse(t.ContextFormat)
	})
	t.questionTmplOnce.Do(func() {
		t.questionTmpl, t.questionTmplErr = template.New("question").Parse(t.QuestionFormat)
	})
}

// BuildPromptWithTemplate 使用模板引擎构建提示
func (t *PromptTemplate) BuildPromptWithTemplate(question string, contexts []*models.RetrievalResult) (string, error) {
	// 初始化并缓存模板
	t.initTemplates()
	if t.ctxTmplErr != nil {
		return "", fmt.Errorf("解析上下文模板失败: %w", t.ctxTmplErr)
	}
	if t.questionTmplErr != nil {
		return "", fmt.Errorf("解析问题模板失败: %w", t.questionTmplErr)
	}

	var buf bytes.Buffer

	// 系统提示
	buf.WriteString(t.SystemPrompt)
	buf.WriteString("\n\n")

	// 上下文
	if len(contexts) > 0 {
		buf.WriteString("=== 参考资料 ===\n")
		for i, ctx := range contexts {
			item := ContextItem{
				Index:   i + 1,
				Content: ctx.Content,
				Source:  ctx.Source,
				Score:   ctx.Score,
			}

			var ctxBuf bytes.Buffer
			if err := t.ctxTmpl.Execute(&ctxBuf, item); err != nil {
				return "", fmt.Errorf("执行上下文模板失败: %w", err)
			}
			buf.WriteString(ctxBuf.String())
			buf.WriteString("\n\n")
		}
	}

	// 问题
	buf.WriteString("=== 问题 ===\n")
	var questionBuf bytes.Buffer
	if err := t.questionTmpl.Execute(&questionBuf, map[string]string{"Question": question}); err != nil {
		return "", fmt.Errorf("执行问题模板失败: %w", err)
	}
	buf.WriteString(questionBuf.String())
	buf.WriteString("\n\n")

	// 回答提示
	buf.WriteString("=== 回答 ===\n")

	return buf.String(), nil
}

// formatContext 格式化上下文
func (t *PromptTemplate) formatContext(item ContextItem) string {
	result := t.ContextFormat
	result = strings.ReplaceAll(result, "{{.Index}}", fmt.Sprintf("%d", item.Index))
	result = strings.ReplaceAll(result, "{{.Content}}", item.Content)
	result = strings.ReplaceAll(result, "{{.Source}}", item.Source)
	result = strings.ReplaceAll(result, "{{.Score}}", fmt.Sprintf("%.4f", item.Score))
	return result
}

// formatQuestion 格式化问题
func (t *PromptTemplate) formatQuestion(question string) string {
	result := t.QuestionFormat
	result = strings.ReplaceAll(result, "{{.Question}}", question)
	return result
}

// GetNoContextMessage 获取无上下文时的消息
func (t *PromptTemplate) GetNoContextMessage() string {
	return t.NoContextMsg
}

// CustomPromptTemplate 创建自定义提示模板
func CustomPromptTemplate(systemPrompt, contextFormat, questionFormat, noContextMsg string) *PromptTemplate {
	return &PromptTemplate{
		SystemPrompt:   systemPrompt,
		ContextFormat:  contextFormat,
		QuestionFormat: questionFormat,
		NoContextMsg:   noContextMsg,
	}
}
