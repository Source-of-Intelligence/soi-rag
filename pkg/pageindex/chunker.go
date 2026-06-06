package pageindex

import (
	"strings"
	"unicode/utf8"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
)

// Chunker 分块器接口
type Chunker interface {
	Chunk(content string) ([]*models.Chunk, error)
}

// FixedSizeChunker 固定大小分块器
type FixedSizeChunker struct {
	ChunkSize    int // 每个分块的目标字符数
	ChunkOverlap int // 分块之间的重叠字符数
}

// NewFixedSizeChunker 创建固定大小分块器
func NewFixedSizeChunker(chunkSize, chunkOverlap int) *FixedSizeChunker {
	if chunkSize <= 0 {
		chunkSize = 512
	}
	if chunkOverlap < 0 {
		chunkOverlap = 50
	}
	return &FixedSizeChunker{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
	}
}

// Chunk 执行分块
func (c *FixedSizeChunker) Chunk(content string) ([]*models.Chunk, error) {
	if content == "" {
		return []*models.Chunk{}, nil
	}

	var chunks []*models.Chunk
	contentLen := utf8.RuneCountInString(content)

	start := 0
	chunkIndex := 0

	for start < contentLen {
		end := start + c.ChunkSize
		if end > contentLen {
			end = contentLen
		}

		// 获取子串
		chunkContent := substringByRunes(content, start, end)

		chunk := &models.Chunk{
			Content:    chunkContent,
			StartPos:   start,
			EndPos:     end,
			ChunkIndex: chunkIndex,
			TokenCount: estimateTokenCount(chunkContent),
		}

		chunks = append(chunks, chunk)

		// 移动起始位置，考虑重叠
		start = end - c.ChunkOverlap
		if start < 0 {
			start = 0
		}
		if start <= end-c.ChunkSize && end < contentLen {
			start = end
		}

		chunkIndex++
	}

	return chunks, nil
}

// RecursiveChunker 递归分块器
type RecursiveChunker struct {
	Separators   []string // 分隔符优先级列表
	ChunkSize    int
	ChunkOverlap int
}

// NewRecursiveChunker 创建递归分块器
func NewRecursiveChunker(chunkSize, chunkOverlap int) *RecursiveChunker {
	if chunkSize <= 0 {
		chunkSize = 512
	}
	if chunkOverlap < 0 {
		chunkOverlap = 50
	}
	return &RecursiveChunker{
		Separators:   []string{"\n\n", "\n", "。", ".", " ", ""},
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
	}
}

// Chunk 执行递归分块
func (c *RecursiveChunker) Chunk(content string) ([]*models.Chunk, error) {
	if content == "" {
		return []*models.Chunk{}, nil
	}

	chunks, _ := c.splitText(content, 0, 0)
	return chunks, nil
}

func (c *RecursiveChunker) splitText(text string, separatorIndex, chunkIndex int) ([]*models.Chunk, int) {
	var chunks []*models.Chunk

	// 如果文本长度小于分块大小，直接返回
	if utf8.RuneCountInString(text) <= c.ChunkSize {
		chunk := &models.Chunk{
			Content:    text,
			StartPos:   0,
			EndPos:     utf8.RuneCountInString(text),
			ChunkIndex: chunkIndex,
			TokenCount: estimateTokenCount(text),
		}
		return []*models.Chunk{chunk}, chunkIndex + 1
	}

	// 获取当前分隔符
	separator := ""
	if separatorIndex < len(c.Separators) {
		separator = c.Separators[separatorIndex]
	}

	// 分割文本
	splits := splitBySeparator(text, separator)

	// 尝试合并小分块
	var currentChunk strings.Builder
	currentStart := 0

	for i, split := range splits {
		splitLen := utf8.RuneCountInString(split)
		currentLen := utf8.RuneCountInString(currentChunk.String())

		// 如果当前分块加上新分块超过限制
		if currentLen+splitLen > c.ChunkSize && currentLen > 0 {
			// 保存当前分块
			chunkContent := currentChunk.String()
			chunk := &models.Chunk{
				Content:    chunkContent,
				StartPos:   currentStart,
				EndPos:     currentStart + currentLen,
				ChunkIndex: chunkIndex,
				TokenCount: estimateTokenCount(chunkContent),
			}
			chunks = append(chunks, chunk)
			chunkIndex++

			// 考虑重叠
			overlapStart := currentLen - c.ChunkOverlap
			if overlapStart < 0 {
				overlapStart = 0
			}
			overlapText := substringByRunes(chunkContent, overlapStart, currentLen)

			currentChunk.Reset()
			currentChunk.WriteString(overlapText)
			currentStart = currentStart + overlapStart
		}

		currentChunk.WriteString(split)

		// 处理最后一个分块
		if i == len(splits)-1 && currentChunk.Len() > 0 {
			chunkContent := currentChunk.String()
			chunk := &models.Chunk{
				Content:    chunkContent,
				StartPos:   currentStart,
				EndPos:     currentStart + utf8.RuneCountInString(chunkContent),
				ChunkIndex: chunkIndex,
				TokenCount: estimateTokenCount(chunkContent),
			}
			chunks = append(chunks, chunk)
			chunkIndex++
		}
	}

	return chunks, chunkIndex
}

// SemanticChunker 语义分块器（简化版）
type SemanticChunker struct {
	MaxChunkSize int
}

// NewSemanticChunker 创建语义分块器
func NewSemanticChunker(maxChunkSize int) *SemanticChunker {
	if maxChunkSize <= 0 {
		maxChunkSize = 512
	}
	return &SemanticChunker{
		MaxChunkSize: maxChunkSize,
	}
}

// Chunk 执行语义分块（基于段落和句子边界）
func (c *SemanticChunker) Chunk(content string) ([]*models.Chunk, error) {
	if content == "" {
		return []*models.Chunk{}, nil
	}

	var chunks []*models.Chunk
	paragraphs := strings.Split(content, "\n\n")

	currentChunk := &strings.Builder{}
	currentStart := 0
	chunkIndex := 0

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		paraLen := utf8.RuneCountInString(para)
		currentLen := utf8.RuneCountInString(currentChunk.String())

		// 如果段落太长，需要进一步分割
		if paraLen > c.MaxChunkSize {
			// 先保存当前累积的内容
			if currentLen > 0 {
				chunkContent := currentChunk.String()
				chunk := &models.Chunk{
					Content:    chunkContent,
					StartPos:   currentStart,
					EndPos:     currentStart + currentLen,
					ChunkIndex: chunkIndex,
					TokenCount: estimateTokenCount(chunkContent),
				}
				chunks = append(chunks, chunk)
				chunkIndex++
				currentChunk.Reset()
			}

			// 按句子分割长段落
			sentences := splitSentences(para)
			for _, sentence := range sentences {
				sentenceLen := utf8.RuneCountInString(sentence)
				currentLen = utf8.RuneCountInString(currentChunk.String())

				if currentLen+sentenceLen > c.MaxChunkSize && currentLen > 0 {
					chunkContent := currentChunk.String()
					chunk := &models.Chunk{
						Content:    chunkContent,
						StartPos:   currentStart,
						EndPos:     currentStart + currentLen,
						ChunkIndex: chunkIndex,
						TokenCount: estimateTokenCount(chunkContent),
					}
					chunks = append(chunks, chunk)
					chunkIndex++
					currentChunk.Reset()
					currentStart += currentLen
				}

				currentChunk.WriteString(sentence)
			}
		} else if currentLen+paraLen > c.MaxChunkSize && currentLen > 0 {
			// 保存当前分块
			chunkContent := currentChunk.String()
			chunk := &models.Chunk{
				Content:    chunkContent,
				StartPos:   currentStart,
				EndPos:     currentStart + currentLen,
				ChunkIndex: chunkIndex,
				TokenCount: estimateTokenCount(chunkContent),
			}
			chunks = append(chunks, chunk)
			chunkIndex++

			currentChunk.Reset()
			currentChunk.WriteString(para)
			currentStart += currentLen
		} else {
			if currentLen > 0 {
				currentChunk.WriteString("\n\n")
			}
			currentChunk.WriteString(para)
		}
	}

	// 保存最后一个分块
	if currentChunk.Len() > 0 {
		chunkContent := currentChunk.String()
		chunk := &models.Chunk{
			Content:    chunkContent,
			StartPos:   currentStart,
			EndPos:     currentStart + utf8.RuneCountInString(chunkContent),
			ChunkIndex: chunkIndex,
			TokenCount: estimateTokenCount(chunkContent),
		}
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// 辅助函数

// substringByRunes 按rune截取子串
func substringByRunes(s string, start, end int) string {
	runes := []rune(s)
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start >= end {
		return ""
	}
	return string(runes[start:end])
}

// splitBySeparator 按分隔符分割文本
func splitBySeparator(text, separator string) []string {
	if separator == "" {
		// 按字符分割
		runes := []rune(text)
		result := make([]string, len(runes))
		for i, r := range runes {
			result[i] = string(r)
		}
		return result
	}
	return strings.Split(text, separator)
}

// splitSentences 按句子分割（简化版）
func splitSentences(text string) []string {
	// 简单的句子分割，考虑中英文标点
	separators := []string{".", "!", "?", "。", "！", "？", "\n"}

	var sentences []string
	current := &strings.Builder{}

	runes := []rune(text)
	for i, r := range runes {
		current.WriteRune(r)

		// 检查是否是句子结束符
		isEnd := false
		for _, sep := range separators {
			if string(r) == sep {
				isEnd = true
				break
			}
		}

		if isEnd && current.Len() > 0 {
			sentences = append(sentences, strings.TrimSpace(current.String()))
			current.Reset()
		} else if i == len(runes)-1 && current.Len() > 0 {
			sentences = append(sentences, strings.TrimSpace(current.String()))
		}
	}

	return sentences
}

// estimateTokenCount 估算token数量（简化版：按字符数/4估算）
func estimateTokenCount(text string) int {
	// 粗略估算：1个token约等于4个字符
	return utf8.RuneCountInString(text) / 4
}
