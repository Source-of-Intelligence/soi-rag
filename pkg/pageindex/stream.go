package pageindex

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
)

// StreamReader 流式读取器接口，用于逐块读取大文件
type StreamReader interface {
	// ReadChunk 读取一块数据
	// 返回 io.EOF 表示读取完毕
	ReadChunk() ([]byte, error)

	// Reset 重置读取器，可以重新从头读取
	Reset(r io.Reader) error

	// Close 关闭读取器
	Close() error
}

// DefaultStreamReader 默认流式读取器实现
type DefaultStreamReader struct {
	reader    *bufio.Reader
	chunkSize int
}

// NewStreamReader 创建默认流式读取器
func NewStreamReader(r io.Reader, chunkSize int) *DefaultStreamReader {
	if chunkSize <= 0 {
		chunkSize = 4096 // 默认4KB
	}
	return &DefaultStreamReader{
		reader:    bufio.NewReader(r),
		chunkSize: chunkSize,
	}
}

// ReadChunk 读取一块数据
func (r *DefaultStreamReader) ReadChunk() ([]byte, error) {
	buf := make([]byte, r.chunkSize)
	n, err := r.reader.Read(buf)
	if n > 0 {
		return buf[:n], nil
	}
	if err == io.EOF {
		return nil, io.EOF
	}
	return nil, err
}

// Reset 重置读取器
func (r *DefaultStreamReader) Reset(reader io.Reader) error {
	r.reader = bufio.NewReader(reader)
	return nil
}

// Close 关闭读取器
func (r *DefaultStreamReader) Close() error {
	// DefaultStreamReader 不拥有底层 reader，不需要关闭
	return nil
}

// StreamChunker 流式分块器配置
type StreamChunkerConfig struct {
	ChunkSize    int // 每个分块的目标字符数
	ChunkOverlap int // 分块之间的重叠字符数
	BufferSize   int // 读取缓冲区大小（字节）
}

// DefaultStreamChunkerConfig 默认流式分块器配置
func DefaultStreamChunkerConfig() *StreamChunkerConfig {
	return &StreamChunkerConfig{
		ChunkSize:    512,
		ChunkOverlap: 50,
		BufferSize:   8192, // 8KB
	}
}

// StreamChunker 流式分块器
type StreamChunker struct {
	config     *StreamChunkerConfig
	reader     io.Reader
	bufReader  *bufio.Reader
	buffer     bytes.Buffer // 累积缓冲区
	chunkIndex int          // 当前分块索引
	totalRead  int          // 已读取的总字符数（按rune计算）
	overlapBuf []rune       // 重叠缓冲区
	done       bool         // 是否已完成
}

// NewStreamChunker 从 io.Reader 创建流式分块器
func NewStreamChunker(r io.Reader, config *StreamChunkerConfig) *StreamChunker {
	if config == nil {
		config = DefaultStreamChunkerConfig()
	}
	if config.ChunkSize <= 0 {
		config.ChunkSize = 512
	}
	if config.ChunkOverlap < 0 {
		config.ChunkOverlap = 50
	}
	if config.BufferSize <= 0 {
		config.BufferSize = 8192
	}

	return &StreamChunker{
		config:     config,
		reader:     r,
		bufReader:  bufio.NewReader(r),
		chunkIndex: 0,
		totalRead:  0,
		done:       false,
	}
}

// NextChunk 返回下一个分块
// 返回 nil, io.EOF 表示所有分块已读取完毕
func (s *StreamChunker) NextChunk() (*models.Chunk, error) {
	if s.done {
		return nil, io.EOF
	}

	// 目标字符数
	targetSize := s.config.ChunkSize

	// 如果有重叠缓冲区，先使用重叠内容
	var chunkRunes []rune
	if len(s.overlapBuf) > 0 {
		chunkRunes = append(chunkRunes, s.overlapBuf...)
		s.overlapBuf = nil
	}

	// 读取足够的数据
	for len(chunkRunes) < targetSize {
		// 尝试读取更多数据
		r, size, err := s.bufReader.ReadRune()
		if err != nil {
			if err == io.EOF {
				// 读取完毕
				s.done = true
				break
			}
			return nil, err
		}

		// 跳过无效的 rune
		if r == utf8.RuneError && size == 1 {
			continue
		}

		chunkRunes = append(chunkRunes, r)
		s.totalRead++
	}

	// 如果没有数据，返回 EOF
	if len(chunkRunes) == 0 {
		return nil, io.EOF
	}

	// 创建分块
	chunkContent := string(chunkRunes)
	startPos := s.totalRead - len(chunkRunes)

	chunk := &models.Chunk{
		Content:    chunkContent,
		StartPos:   startPos,
		EndPos:     s.totalRead,
		ChunkIndex: s.chunkIndex,
		TokenCount: estimateTokenCount(chunkContent),
	}

	// 准备重叠缓冲区（用于下一个分块）
	if s.config.ChunkOverlap > 0 && len(chunkRunes) > s.config.ChunkOverlap {
		overlapStart := len(chunkRunes) - s.config.ChunkOverlap
		s.overlapBuf = make([]rune, s.config.ChunkOverlap)
		copy(s.overlapBuf, chunkRunes[overlapStart:])
	}

	s.chunkIndex++

	return chunk, nil
}

// NextChunkWithCallback 读取下一个分块并执行回调
func (s *StreamChunker) NextChunkWithCallback(callback func(*models.Chunk) error) error {
	chunk, err := s.NextChunk()
	if err != nil {
		return err
	}
	return callback(chunk)
}

// ProcessAll 处理所有分块
func (s *StreamChunker) ProcessAll(callback func(*models.Chunk) error) error {
	for {
		chunk, err := s.NextChunk()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := callback(chunk); err != nil {
			return err
		}
	}
}

// CollectAll 收集所有分块
func (s *StreamChunker) CollectAll() ([]*models.Chunk, error) {
	var chunks []*models.Chunk
	for {
		chunk, err := s.NextChunk()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

// Reset 重置分块器，可以重新从头开始
func (s *StreamChunker) Reset(r io.Reader) {
	s.reader = r
	s.bufReader = bufio.NewReader(r)
	s.buffer.Reset()
	s.chunkIndex = 0
	s.totalRead = 0
	s.overlapBuf = nil
	s.done = false
}

// ChunkIndex 返回当前分块索引
func (s *StreamChunker) ChunkIndex() int {
	return s.chunkIndex
}

// TotalRead 返回已读取的总字符数
func (s *StreamChunker) TotalRead() int {
	return s.totalRead
}

// LineStreamChunker 按行流式分块器
// 适用于需要按行边界分割的场景
type LineStreamChunker struct {
	config      *StreamChunkerConfig
	reader      *bufio.Reader
	chunkIndex  int
	totalRead   int
	currentLine int
	done        bool
}

// NewLineStreamChunker 创建按行流式分块器
func NewLineStreamChunker(r io.Reader, config *StreamChunkerConfig) *LineStreamChunker {
	if config == nil {
		config = DefaultStreamChunkerConfig()
	}
	if config.ChunkSize <= 0 {
		config.ChunkSize = 512
	}

	return &LineStreamChunker{
		config:      config,
		reader:      bufio.NewReader(r),
		chunkIndex:  0,
		totalRead:   0,
		currentLine: 0,
		done:        false,
	}
}

// NextChunk 返回下一个分块（按行边界分割）
func (s *LineStreamChunker) NextChunk() (*models.Chunk, error) {
	if s.done {
		return nil, io.EOF
	}

	var lines []string
	var chunkRunes int
	startPos := s.totalRead

	for {
		line, err := s.reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}

		if line != "" {
			lines = append(lines, line)
			lineRunes := utf8.RuneCountInString(line)
			chunkRunes += lineRunes
			s.totalRead += lineRunes
			s.currentLine++
		}

		// 检查是否达到分块大小或读取完毕
		if chunkRunes >= s.config.ChunkSize || err == io.EOF {
			if err == io.EOF {
				s.done = true
			}
			break
		}
	}

	// 如果没有数据，返回 EOF
	if len(lines) == 0 {
		return nil, io.EOF
	}

	// 合并行
	var content bytes.Buffer
	for _, line := range lines {
		content.WriteString(line)
	}

	chunk := &models.Chunk{
		Content:    content.String(),
		StartPos:   startPos,
		EndPos:     s.totalRead,
		ChunkIndex: s.chunkIndex,
		TokenCount: estimateTokenCount(content.String()),
	}

	s.chunkIndex++

	return chunk, nil
}

// CollectAll 收集所有分块
func (s *LineStreamChunker) CollectAll() ([]*models.Chunk, error) {
	var chunks []*models.Chunk
	for {
		chunk, err := s.NextChunk()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

// BufferedStreamChunker 带缓冲的流式分块器
// 支持预读取和回退操作
type BufferedStreamChunker struct {
	config     *StreamChunkerConfig
	reader     *bufio.Reader
	buffer     []rune // 主缓冲区
	chunkIndex int
	totalRead  int
	done       bool
}

// NewBufferedStreamChunker 创建带缓冲的流式分块器
func NewBufferedStreamChunker(r io.Reader, config *StreamChunkerConfig) *BufferedStreamChunker {
	if config == nil {
		config = DefaultStreamChunkerConfig()
	}
	if config.ChunkSize <= 0 {
		config.ChunkSize = 512
	}
	if config.BufferSize <= 0 {
		config.BufferSize = 8192
	}

	return &BufferedStreamChunker{
		config:     config,
		reader:     bufio.NewReader(r),
		buffer:     make([]rune, 0, config.BufferSize),
		chunkIndex: 0,
		totalRead:  0,
		done:       false,
	}
}

// fillBuffer 填充缓冲区
func (s *BufferedStreamChunker) fillBuffer(minSize int) error {
	for len(s.buffer) < minSize {
		r, size, err := s.reader.ReadRune()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if r == utf8.RuneError && size == 1 {
			continue
		}
		s.buffer = append(s.buffer, r)
		s.totalRead++
	}
	return nil
}

// NextChunk 返回下一个分块
func (s *BufferedStreamChunker) NextChunk() (*models.Chunk, error) {
	if s.done && len(s.buffer) == 0 {
		return nil, io.EOF
	}

	// 确保缓冲区有足够数据
	targetSize := s.config.ChunkSize + s.config.ChunkOverlap
	if err := s.fillBuffer(targetSize); err != nil {
		return nil, err
	}

	// 检查是否已读完
	if len(s.buffer) == 0 {
		return nil, io.EOF
	}

	// 确定分块大小
	chunkSize := s.config.ChunkSize
	if len(s.buffer) < chunkSize {
		chunkSize = len(s.buffer)
		s.done = true
	}

	// 提取分块
	chunkRunes := s.buffer[:chunkSize]
	chunkContent := string(chunkRunes)

	startPos := s.totalRead - len(s.buffer)

	chunk := &models.Chunk{
		Content:    chunkContent,
		StartPos:   startPos,
		EndPos:     startPos + chunkSize,
		ChunkIndex: s.chunkIndex,
		TokenCount: estimateTokenCount(chunkContent),
	}

	// 更新缓冲区（保留重叠部分）
	if s.config.ChunkOverlap > 0 && chunkSize > s.config.ChunkOverlap {
		overlapStart := chunkSize - s.config.ChunkOverlap
		s.buffer = s.buffer[overlapStart:]
	} else {
		s.buffer = s.buffer[chunkSize:]
	}

	s.chunkIndex++

	return chunk, nil
}

// Peek 预览接下来的数据（不移动读取位置）
func (s *BufferedStreamChunker) Peek(n int) (string, error) {
	if err := s.fillBuffer(n); err != nil {
		return "", err
	}
	if len(s.buffer) < n {
		n = len(s.buffer)
	}
	return string(s.buffer[:n]), nil
}

// CollectAll 收集所有分块
func (s *BufferedStreamChunker) CollectAll() ([]*models.Chunk, error) {
	var chunks []*models.Chunk
	for {
		chunk, err := s.NextChunk()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

// StreamChunkerBuilder 流式分块器构建器
type StreamChunkerBuilder struct {
	reader io.Reader
	config *StreamChunkerConfig
	mode   string // "default", "line", "buffered"
}

// NewStreamChunkerBuilder 创建流式分块器构建器
func NewStreamChunkerBuilder(r io.Reader) *StreamChunkerBuilder {
	return &StreamChunkerBuilder{
		reader: r,
		config: DefaultStreamChunkerConfig(),
		mode:   "default",
	}
}

// WithChunkSize 设置分块大小
func (b *StreamChunkerBuilder) WithChunkSize(size int) *StreamChunkerBuilder {
	b.config.ChunkSize = size
	return b
}

// WithOverlap 设置重叠大小
func (b *StreamChunkerBuilder) WithOverlap(overlap int) *StreamChunkerBuilder {
	b.config.ChunkOverlap = overlap
	return b
}

// WithBufferSize 设置缓冲区大小
func (b *StreamChunkerBuilder) WithBufferSize(size int) *StreamChunkerBuilder {
	b.config.BufferSize = size
	return b
}

// WithMode 设置分块模式
func (b *StreamChunkerBuilder) WithMode(mode string) *StreamChunkerBuilder {
	b.mode = mode
	return b
}

// Build 构建流式分块器
func (b *StreamChunkerBuilder) Build() interface {
	NextChunk() (*models.Chunk, error)
} {
	switch b.mode {
	case "line":
		return NewLineStreamChunker(b.reader, b.config)
	case "buffered":
		return NewBufferedStreamChunker(b.reader, b.config)
	default:
		return NewStreamChunker(b.reader, b.config)
	}
}

// 常见错误
var (
	ErrInvalidChunkSize   = errors.New("invalid chunk size: must be positive")
	ErrInvalidOverlap     = errors.New("invalid overlap: cannot be negative")
	ErrOverlapExceedsSize = errors.New("overlap cannot exceed chunk size")
	ErrReaderClosed       = errors.New("reader is closed")
)
