package keyword

import (
	"strings"
	"sync"
	"unicode"

	"github.com/go-ego/gse"
)

// SegMode 分词模式
type SegMode int

const (
	// SegModeExact 精确模式 - 尽量将句子切分成有意义的词语
	SegModeExact SegMode = iota
	// SegModeSearch 搜索模式 - 在精确模式基础上，对长词再次切分
	SegModeSearch
)

// GseTokenizer 基于 gse 的中文分词器
type GseTokenizer struct {
	seg       *gse.Segmenter
	mode      SegMode
	initOnce  sync.Once
	initErr   error
	dictPaths []string
	mu        sync.RWMutex
}

// GseTokenizerOption 分词器配置选项
type GseTokenizerOption func(*GseTokenizer)

// WithSegMode 设置分词模式
func WithSegMode(mode SegMode) GseTokenizerOption {
	return func(t *GseTokenizer) {
		t.mode = mode
	}
}

// WithDictPaths 设置自定义词典路径
func WithDictPaths(paths ...string) GseTokenizerOption {
	return func(t *GseTokenizer) {
		t.dictPaths = paths
	}
}

// NewGseTokenizer 创建基于 gse 的中文分词器
func NewGseTokenizer(opts ...GseTokenizerOption) *GseTokenizer {
	t := &GseTokenizer{
		mode: SegModeExact,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// initSegmenter 延迟初始化分词器
func (t *GseTokenizer) initSegmenter() error {
	t.initOnce.Do(func() {
		t.seg = &gse.Segmenter{}

		// 加载词典
		if len(t.dictPaths) > 0 {
			// 加载自定义词典
			t.initErr = t.seg.LoadDict(t.dictPaths...)
		} else {
			// 加载默认词典
			t.initErr = t.seg.LoadDict()
		}
	})
	return t.initErr
}

// Tokenize 实现Tokenizer接口
func (t *GseTokenizer) Tokenize(text string) []string {
	if err := t.initSegmenter(); err != nil {
		// 如果初始化失败，回退到简单分词
		return t.fallbackTokenize(text)
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	var segments []string

	switch t.mode {
	case SegModeSearch:
		// 搜索模式：使用CutSearch进行切分
		segments = t.seg.CutSearch(text, true)
	default:
		// 精确模式：使用Cut进行精确切分
		segments = t.seg.Cut(text, true)
	}

	// 过滤空白和单字符标点
	result := make([]string, 0, len(segments))
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		// 过滤纯标点符号
		if isAllPunctuation(seg) {
			continue
		}
		result = append(result, seg)
	}

	return result
}

// TokenizeWithPos 带词性标注的分词
func (t *GseTokenizer) TokenizeWithPos(text string) []gse.SegPos {
	if err := t.initSegmenter(); err != nil {
		return nil
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.seg.Pos(text, true)
}

// CutAll 全切分模式
func (t *GseTokenizer) CutAll(text string) []string {
	if err := t.initSegmenter(); err != nil {
		return t.fallbackTokenize(text)
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.seg.CutAll(text)
}

// CutForSearch 搜索引擎模式切分
func (t *GseTokenizer) CutForSearch(text string) []string {
	if err := t.initSegmenter(); err != nil {
		return t.fallbackTokenize(text)
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.seg.CutSearch(text, true)
}

// AddDict 添加自定义词典词
func (t *GseTokenizer) AddDict(word string, freq float64, pos ...string) error {
	if err := t.initSegmenter(); err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// gse 支持动态添加词
	return t.seg.AddToken(word, freq, pos...)
}

// LoadUserDict 加载用户自定义词典
func (t *GseTokenizer) LoadUserDict(paths ...string) error {
	if err := t.initSegmenter(); err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	return t.seg.LoadDict(paths...)
}

// SetMode 设置分词模式
func (t *GseTokenizer) SetMode(mode SegMode) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.mode = mode
}

// GetMode 获取当前分词模式
func (t *GseTokenizer) GetMode() SegMode {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.mode
}

// fallbackTokenize 回退到简单分词（当gse初始化失败时使用）
func (t *GseTokenizer) fallbackTokenize(text string) []string {
	var tokens []string
	var current strings.Builder

	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			// 遇到汉字，先保存之前的非汉字token
			if current.Len() > 0 {
				tokens = append(tokens, strings.ToLower(current.String()))
				current.Reset()
			}
			// 汉字单独成词
			tokens = append(tokens, string(r))
		} else if unicode.IsLetter(r) || unicode.IsNumber(r) {
			current.WriteRune(unicode.ToLower(r))
		} else if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, strings.ToLower(current.String()))
	}

	return tokens
}

// isAllPunctuation 检查字符串是否全是标点符号
func isAllPunctuation(s string) bool {
	for _, r := range s {
		if !unicode.IsPunct(r) && !unicode.IsSpace(r) {
			return false
		}
	}
	return len(s) > 0
}

// Ensure GseTokenizer implements Tokenizer interface
var _ Tokenizer = (*GseTokenizer)(nil)
