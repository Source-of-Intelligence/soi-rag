package keyword

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
	"github.com/google/uuid"
)

// Tokenizer 分词器接口
type Tokenizer interface {
	Tokenize(text string) []string
}

// SimpleTokenizer 简单分词器
type SimpleTokenizer struct{}

// Tokenize 分词
func (t *SimpleTokenizer) Tokenize(text string) []string {
	var tokens []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			current.WriteRune(unicode.ToLower(r))
		} else if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// ChineseTokenizer 中文分词器（简化版）
type ChineseTokenizer struct{}

// Tokenize 分词
func (t *ChineseTokenizer) Tokenize(text string) []string {
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

// Document 关键词索引中的文档
type Document struct {
	ID       string
	Content  string
	Tokens   []string
	TermFreq map[string]int
}

// InvertedIndex 倒排索引
type InvertedIndex struct {
	index       map[string]map[string]int // term -> docID -> frequency
	docs        map[string]*Document
	tokenizer   Tokenizer
	docCount    int
	totalDocLen int // 所有文档的token总数，用于计算avgDocLength
	mu          sync.RWMutex
}

// NewInvertedIndex 创建倒排索引
func NewInvertedIndex(tokenizer Tokenizer) *InvertedIndex {
	if tokenizer == nil {
		tokenizer = &ChineseTokenizer{}
	}
	return &InvertedIndex{
		index:     make(map[string]map[string]int),
		docs:      make(map[string]*Document),
		tokenizer: tokenizer,
	}
}

// AddDocument 添加文档
func (idx *InvertedIndex) AddDocument(id, content string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if id == "" {
		id = uuid.New().String()
	}

	// 如果文档已存在，先删除
	if _, exists := idx.docs[id]; exists {
		idx.removeDocumentInternal(id)
	}

	// 分词
	tokens := idx.tokenizer.Tokenize(content)
	termFreq := make(map[string]int)

	for _, token := range tokens {
		termFreq[token]++
	}

	doc := &Document{
		ID:       id,
		Content:  content,
		Tokens:   tokens,
		TermFreq: termFreq,
	}

	idx.docs[id] = doc
	idx.docCount++
	idx.totalDocLen += len(tokens)

	// 更新倒排索引
	for term, freq := range termFreq {
		if idx.index[term] == nil {
			idx.index[term] = make(map[string]int)
		}
		idx.index[term][id] = freq
	}
}

// RemoveDocument 删除文档
func (idx *InvertedIndex) RemoveDocument(id string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.removeDocumentInternal(id)
}

func (idx *InvertedIndex) removeDocumentInternal(id string) {
	doc, exists := idx.docs[id]
	if !exists {
		return
	}

	// 从倒排索引中删除
	for term := range doc.TermFreq {
		delete(idx.index[term], id)
		if len(idx.index[term]) == 0 {
			delete(idx.index, term)
		}
	}

	delete(idx.docs, id)
	idx.docCount--
	idx.totalDocLen -= len(doc.Tokens)
}

// Search 搜索
func (idx *InvertedIndex) Search(query string, topK int) []*models.RetrievalResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	queryTokens := idx.tokenizer.Tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	// 动态计算平均文档长度
	avgDocLength := 100.0
	if idx.docCount > 0 {
		avgDocLength = float64(idx.totalDocLen) / float64(idx.docCount)
	}

	// 计算每个文档的BM25分数
	docScores := make(map[string]float64)

	for _, token := range queryTokens {
		docFreq := len(idx.index[token])
		if docFreq == 0 {
			continue
		}

		idf := calculateIDF(idx.docCount, docFreq)

		for docID, termFreq := range idx.index[token] {
			doc := idx.docs[docID]
			bm25 := calculateBM25(termFreq, len(doc.Tokens), idf, avgDocLength)
			docScores[docID] += bm25
		}
	}

	// 排序
	type scoredDoc struct {
		id    string
		score float64
	}

	var scoredDocs []scoredDoc
	for id, score := range docScores {
		scoredDocs = append(scoredDocs, scoredDoc{id: id, score: score})
	}

	sort.Slice(scoredDocs, func(i, j int) bool {
		return scoredDocs[i].score > scoredDocs[j].score
	})

	// 限制结果数
	if topK > 0 && len(scoredDocs) > topK {
		scoredDocs = scoredDocs[:topK]
	}

	// 构建结果
	var results []*models.RetrievalResult
	for _, sd := range scoredDocs {
		doc := idx.docs[sd.id]
		results = append(results, &models.RetrievalResult{
			ID:         doc.ID,
			Content:    doc.Content,
			Score:      sd.score,
			DocumentID: doc.ID,
		})
	}

	return results
}

// calculateIDF 计算IDF
func calculateIDF(totalDocs, docFreq int) float64 {
	if docFreq == 0 {
		return 0
	}
	return math.Log((float64(totalDocs-docFreq) + 0.5) / (float64(docFreq) + 0.5))
}

// calculateBM25 计算BM25分数
func calculateBM25(termFreq, docLength int, idf, avgDocLength float64) float64 {
	k1 := 1.2
	b := 0.75

	tf := float64(termFreq)
	dl := float64(docLength)

	return idf * (tf * (k1 + 1)) / (tf + k1*(1-b+b*dl/avgDocLength))
}

// KeywordRetriever 关键词检索器
type KeywordRetriever struct {
	index *InvertedIndex
}

// NewKeywordRetriever 创建关键词检索器
func NewKeywordRetriever(tokenizer Tokenizer) *KeywordRetriever {
	return &KeywordRetriever{
		index: NewInvertedIndex(tokenizer),
	}
}

// IndexChunks 索引分块
func (r *KeywordRetriever) IndexChunks(ctx context.Context, chunks []*models.Chunk) error {
	for _, chunk := range chunks {
		r.index.AddDocument(chunk.ID, chunk.Content)
	}
	return nil
}

// Search 关键词搜索
func (r *KeywordRetriever) Search(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error) {
	results := r.index.Search(query, opts.TopK)

	// 填充文档ID
	for _, result := range results {
		if result.DocumentID == "" {
			result.DocumentID = result.ID
		}
	}

	return &models.SearchResult{
		Total:   len(results),
		Results: results,
	}, nil
}

// DeleteDocument 删除文档
func (r *KeywordRetriever) DeleteDocument(ctx context.Context, docID string) error {
	r.index.RemoveDocument(docID)
	return nil
}

// BooleanQuery 布尔查询
type BooleanQuery struct {
	Must    []string // 必须包含
	Should  []string // 应该包含
	MustNot []string // 必须不包含
}

// BooleanSearch 布尔搜索
func (r *KeywordRetriever) BooleanSearch(ctx context.Context, query BooleanQuery, opts models.SearchOptions) (*models.SearchResult, error) {
	r.index.mu.RLock()
	defer r.index.mu.RUnlock()

	// 收集候选文档
	candidates := make(map[string]float64)

	// Must条件
	if len(query.Must) > 0 {
		first := true
		for _, term := range query.Must {
			termDocs := make(map[string]bool)
			tokens := r.index.tokenizer.Tokenize(term)
			for _, token := range tokens {
				for docID := range r.index.index[token] {
					termDocs[docID] = true
				}
			}

			if first {
				for docID := range termDocs {
					candidates[docID] = 1.0
				}
				first = false
			} else {
				// 交集
				for docID := range candidates {
					if !termDocs[docID] {
						delete(candidates, docID)
					}
				}
			}
		}
	}

	// Should条件（增加分数）
	for _, term := range query.Should {
		tokens := r.index.tokenizer.Tokenize(term)
		for _, token := range tokens {
			for docID := range r.index.index[token] {
				if _, exists := candidates[docID]; exists || len(query.Must) == 0 {
					candidates[docID] += 0.5
				}
			}
		}
	}

	// MustNot条件
	for _, term := range query.MustNot {
		tokens := r.index.tokenizer.Tokenize(term)
		for _, token := range tokens {
			for docID := range r.index.index[token] {
				delete(candidates, docID)
			}
		}
	}

	// 构建结果
	var results []*models.RetrievalResult
	for docID, score := range candidates {
		doc := r.index.docs[docID]
		results = append(results, &models.RetrievalResult{
			ID:         docID,
			Content:    doc.Content,
			Score:      score,
			DocumentID: docID,
		})
	}

	// 排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// 限制结果数
	if opts.TopK > 0 && len(results) > opts.TopK {
		results = results[:opts.TopK]
	}

	return &models.SearchResult{
		Total:   len(results),
		Results: results,
	}, nil
}

// PhraseSearch 短语搜索
func (r *KeywordRetriever) PhraseSearch(ctx context.Context, phrase string, opts models.SearchOptions) (*models.SearchResult, error) {
	r.index.mu.RLock()
	defer r.index.mu.RUnlock()

	phraseTokens := r.index.tokenizer.Tokenize(phrase)
	if len(phraseTokens) == 0 {
		return &models.SearchResult{Results: []*models.RetrievalResult{}}, nil
	}

	// 找到包含所有词的文档
	var candidates []string
	for docID, doc := range r.index.docs {
		if containsPhrase(doc.Content, phrase) {
			candidates = append(candidates, docID)
		}
	}

	// 构建结果
	var results []*models.RetrievalResult
	for _, docID := range candidates {
		doc := r.index.docs[docID]
		results = append(results, &models.RetrievalResult{
			ID:         docID,
			Content:    doc.Content,
			Score:      float64(len(phraseTokens)),
			DocumentID: docID,
		})
	}

	// 限制结果数
	if opts.TopK > 0 && len(results) > opts.TopK {
		results = results[:opts.TopK]
	}

	return &models.SearchResult{
		Total:   len(results),
		Results: results,
	}, nil
}

// containsPhrase 检查文本是否包含短语
func containsPhrase(text, phrase string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(phrase))
}

// PrefixSearch 前缀搜索
func (r *KeywordRetriever) PrefixSearch(ctx context.Context, prefix string, opts models.SearchOptions) (*models.SearchResult, error) {
	r.index.mu.RLock()
	defer r.index.mu.RUnlock()

	prefix = strings.ToLower(prefix)
	var results []*models.RetrievalResult

	for docID, doc := range r.index.docs {
		for term := range doc.TermFreq {
			if strings.HasPrefix(term, prefix) {
				results = append(results, &models.RetrievalResult{
					ID:         docID,
					Content:    doc.Content,
					Score:      1.0,
					DocumentID: docID,
				})
				break
			}
		}
	}

	// 限制结果数
	if opts.TopK > 0 && len(results) > opts.TopK {
		results = results[:opts.TopK]
	}

	return &models.SearchResult{
		Total:   len(results),
		Results: results,
	}, nil
}

// FuzzySearch 模糊搜索
func (r *KeywordRetriever) FuzzySearch(ctx context.Context, query string, maxDistance int, opts models.SearchOptions) (*models.SearchResult, error) {
	if maxDistance <= 0 {
		maxDistance = 2
	}

	r.index.mu.RLock()
	defer r.index.mu.RUnlock()

	queryTokens := r.index.tokenizer.Tokenize(query)
	if len(queryTokens) == 0 {
		return &models.SearchResult{Results: []*models.RetrievalResult{}}, nil
	}

	docScores := make(map[string]float64)

	for _, queryToken := range queryTokens {
		for term := range r.index.index {
			distance := levenshteinDistance(queryToken, term)
			if distance <= maxDistance {
				similarity := 1.0 - float64(distance)/float64(max(len(queryToken), len(term)))
				for docID := range r.index.index[term] {
					docScores[docID] += similarity
				}
			}
		}
	}

	// 排序
	type scoredDoc struct {
		id    string
		score float64
	}

	var scoredDocs []scoredDoc
	for id, score := range docScores {
		scoredDocs = append(scoredDocs, scoredDoc{id: id, score: score})
	}

	sort.Slice(scoredDocs, func(i, j int) bool {
		return scoredDocs[i].score > scoredDocs[j].score
	})

	// 限制结果数
	if opts.TopK > 0 && len(scoredDocs) > opts.TopK {
		scoredDocs = scoredDocs[:opts.TopK]
	}

	// 构建结果
	var results []*models.RetrievalResult
	for _, sd := range scoredDocs {
		doc := r.index.docs[sd.id]
		results = append(results, &models.RetrievalResult{
			ID:         sd.id,
			Content:    doc.Content,
			Score:      sd.score,
			DocumentID: sd.id,
		})
	}

	return &models.SearchResult{
		Total:   len(results),
		Results: results,
	}, nil
}

// levenshteinDistance 计算Levenshtein距离
func levenshteinDistance(s, t string) int {
	m, n := len(s), len(t)
	if m == 0 {
		return n
	}
	if n == 0 {
		return m
	}

	// 创建距离矩阵
	d := make([][]int, m+1)
	for i := range d {
		d[i] = make([]int, n+1)
	}

	// 初始化
	for i := 0; i <= m; i++ {
		d[i][0] = i
	}
	for j := 0; j <= n; j++ {
		d[0][j] = j
	}

	// 填充矩阵
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			cost := 0
			if s[i-1] != t[j-1] {
				cost = 1
			}
			d[i][j] = min(d[i-1][j]+1, min(d[i][j-1]+1, d[i-1][j-1]+cost))
		}
	}

	return d[m][n]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetStats 获取索引统计信息
func (r *KeywordRetriever) GetStats() map[string]interface{} {
	r.index.mu.RLock()
	defer r.index.mu.RUnlock()

	return map[string]interface{}{
		"document_count": r.index.docCount,
		"term_count":     len(r.index.index),
	}
}

// SetTokenizer 设置分词器
func (kr *KeywordRetriever) SetTokenizer(tokenizer Tokenizer) {
	kr.index.tokenizer = tokenizer
}

// GetTokenizer 获取分词器
func (kr *KeywordRetriever) GetTokenizer() Tokenizer {
	return kr.index.tokenizer
}

// NewKeywordRetrieverWithGSE 创建使用GSE分词器的关键词检索器
func NewKeywordRetrieverWithGSE(dictPaths ...string) *KeywordRetriever {
	var tokenizer Tokenizer
	if len(dictPaths) > 0 {
		tokenizer = NewGseTokenizer(WithDictPaths(dictPaths...))
	} else {
		tokenizer = NewGseTokenizer()
	}
	return NewKeywordRetriever(tokenizer)
}

// Clear 清空索引
func (idx *InvertedIndex) Clear() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.docs = make(map[string]*Document)
	idx.index = make(map[string]map[string]int)
	idx.totalDocLen = 0
}
