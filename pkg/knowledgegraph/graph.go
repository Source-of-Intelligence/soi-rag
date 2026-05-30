package knowledgegraph

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/google/uuid"
	"github.com/ragtool/rag/pkg/models"
)

// GraphStore 图存储接口
type GraphStore interface {
	AddEntity(ctx context.Context, entity *models.Entity) error
	AddRelation(ctx context.Context, relation *models.Relation) error
	GetEntity(ctx context.Context, id string) (*models.Entity, error)
	GetRelation(ctx context.Context, id string) (*models.Relation, error)
	SearchEntities(ctx context.Context, query string, limit int) ([]*models.Entity, error)
	QueryRelations(ctx context.Context, entityID string, relationType models.RelationType) ([]*models.Relation, error)
	GetSubgraph(ctx context.Context, entityIDs []string, depth int) (*models.Subgraph, error)
	FindPath(ctx context.Context, sourceID, targetID string, maxDepth int) ([]*models.Path, error)
	DeleteByDocument(ctx context.Context, docID string) error
}

// InMemoryGraphStore 内存图存储
type InMemoryGraphStore struct {
	entities  map[string]*models.Entity
	relations map[string]*models.Relation
	// 索引
	entityNameIndex map[string][]string // name -> entity IDs
	relationIndex   map[string][]string // entityID -> relation IDs
	mu              sync.RWMutex
}

// NewInMemoryGraphStore 创建内存图存储
func NewInMemoryGraphStore() *InMemoryGraphStore {
	return &InMemoryGraphStore{
		entities:        make(map[string]*models.Entity),
		relations:       make(map[string]*models.Relation),
		entityNameIndex: make(map[string][]string),
		relationIndex:   make(map[string][]string),
	}
}

// AddEntity 添加实体
func (s *InMemoryGraphStore) AddEntity(ctx context.Context, entity *models.Entity) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entity.ID == "" {
		entity.ID = uuid.New().String()
	}

	s.entities[entity.ID] = entity

	// 更新名称索引
	nameKey := strings.ToLower(entity.Name)
	s.entityNameIndex[nameKey] = append(s.entityNameIndex[nameKey], entity.ID)

	// 索引别名
	for _, alias := range entity.Aliases {
		aliasKey := strings.ToLower(alias)
		s.entityNameIndex[aliasKey] = append(s.entityNameIndex[aliasKey], entity.ID)
	}

	return nil
}

// AddRelation 添加关系
func (s *InMemoryGraphStore) AddRelation(ctx context.Context, relation *models.Relation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if relation.ID == "" {
		relation.ID = uuid.New().String()
	}

	s.relations[relation.ID] = relation

	// 更新关系索引
	s.relationIndex[relation.SourceID] = append(s.relationIndex[relation.SourceID], relation.ID)
	s.relationIndex[relation.TargetID] = append(s.relationIndex[relation.TargetID], relation.ID)

	return nil
}

// GetEntity 获取实体
func (s *InMemoryGraphStore) GetEntity(ctx context.Context, id string) (*models.Entity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entity, ok := s.entities[id]
	if !ok {
		return nil, fmt.Errorf("实体不存在: %s", id)
	}
	return entity, nil
}

// GetRelation 获取关系
func (s *InMemoryGraphStore) GetRelation(ctx context.Context, id string) (*models.Relation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	relation, ok := s.relations[id]
	if !ok {
		return nil, fmt.Errorf("关系不存在: %s", id)
	}
	return relation, nil
}

// SearchEntities 搜索实体
func (s *InMemoryGraphStore) SearchEntities(ctx context.Context, query string, limit int) ([]*models.Entity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query = strings.ToLower(query)
	var results []*models.Entity
	seen := make(map[string]bool)

	// 精确匹配
	if ids, ok := s.entityNameIndex[query]; ok {
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				results = append(results, s.entities[id])
			}
		}
	}

	// 模糊匹配
	for name, ids := range s.entityNameIndex {
		if strings.Contains(name, query) || strings.Contains(query, name) {
			for _, id := range ids {
				if !seen[id] {
					seen[id] = true
					results = append(results, s.entities[id])
				}
			}
		}
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// QueryRelations 查询关系
func (s *InMemoryGraphStore) QueryRelations(ctx context.Context, entityID string, relationType models.RelationType) ([]*models.Relation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*models.Relation

	relationIDs, ok := s.relationIndex[entityID]
	if !ok {
		return results, nil
	}

	for _, id := range relationIDs {
		relation := s.relations[id]
		if relationType == "" || relation.Type == relationType {
			results = append(results, relation)
		}
	}

	return results, nil
}

// GetSubgraph 获取子图
func (s *InMemoryGraphStore) GetSubgraph(ctx context.Context, entityIDs []string, depth int) (*models.Subgraph, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if depth <= 0 {
		depth = 2
	}

	subgraph := &models.Subgraph{
		Entities:  []*models.Entity{},
		Relations: []*models.Relation{},
	}

	entitySet := make(map[string]bool)
	relationSet := make(map[string]bool)

	// BFS遍历
	currentLevel := entityIDs
	for i := 0; i < depth && len(currentLevel) > 0; i++ {
		nextLevel := []string{}

		for _, entityID := range currentLevel {
			if entitySet[entityID] {
				continue
			}
			entitySet[entityID] = true

			entity, ok := s.entities[entityID]
			if !ok {
				continue
			}
			subgraph.Entities = append(subgraph.Entities, entity)

			// 获取相关关系
			relationIDs, ok := s.relationIndex[entityID]
			if !ok {
				continue
			}

			for _, relationID := range relationIDs {
				if relationSet[relationID] {
					continue
				}
				relationSet[relationID] = true

				relation := s.relations[relationID]
				subgraph.Relations = append(subgraph.Relations, relation)

				// 添加相邻实体到下一层
				if !entitySet[relation.SourceID] {
					nextLevel = append(nextLevel, relation.SourceID)
				}
				if !entitySet[relation.TargetID] {
					nextLevel = append(nextLevel, relation.TargetID)
				}
			}
		}

		currentLevel = nextLevel
	}

	return subgraph, nil
}

// FindPath 查找路径
func (s *InMemoryGraphStore) FindPath(ctx context.Context, sourceID, targetID string, maxDepth int) ([]*models.Path, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if maxDepth <= 0 {
		maxDepth = 5
	}

	var paths []*models.Path

	// DFS查找所有路径
	visited := make(map[string]bool)
	var currentPath []*models.Entity
	var currentEdges []*models.Relation

	var dfs func(currentID string, depth int)
	dfs = func(currentID string, depth int) {
		if depth > maxDepth {
			return
		}

		if currentID == targetID && depth > 0 {
			// 找到一条路径
			path := &models.Path{
				Nodes:  make([]*models.Entity, len(currentPath)),
				Edges:  make([]*models.Relation, len(currentEdges)),
				Length: len(currentEdges),
			}
			copy(path.Nodes, currentPath)
			copy(path.Edges, currentEdges)
			paths = append(paths, path)
			return
		}

		entity, ok := s.entities[currentID]
		if !ok {
			return
		}

		visited[currentID] = true
		currentPath = append(currentPath, entity)

		relationIDs, ok := s.relationIndex[currentID]
		if ok {
			for _, relationID := range relationIDs {
				relation := s.relations[relationID]

				// 确定下一个节点
				nextID := relation.TargetID
				if currentID == relation.TargetID {
					nextID = relation.SourceID
				}

				if !visited[nextID] {
					currentEdges = append(currentEdges, relation)
					dfs(nextID, depth+1)
					currentEdges = currentEdges[:len(currentEdges)-1]
				}
			}
		}

		currentPath = currentPath[:len(currentPath)-1]
		visited[currentID] = false
	}

	dfs(sourceID, 0)

	return paths, nil
}

// DeleteByDocument 根据文档删除
func (s *InMemoryGraphStore) DeleteByDocument(ctx context.Context, docID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 删除实体
	for id, entity := range s.entities {
		if entity.SourceDocID == docID {
			delete(s.entities, id)
			// 从索引中删除
			nameKey := strings.ToLower(entity.Name)
			s.entityNameIndex[nameKey] = removeFromSlice(s.entityNameIndex[nameKey], id)
		}
	}

	// 删除关系
	for id, relation := range s.relations {
		if relation.SourceDocID == docID {
			delete(s.relations, id)
			// 从索引中删除
			s.relationIndex[relation.SourceID] = removeFromSlice(s.relationIndex[relation.SourceID], id)
			s.relationIndex[relation.TargetID] = removeFromSlice(s.relationIndex[relation.TargetID], id)
		}
	}

	return nil
}

func removeFromSlice(slice []string, item string) []string {
	var result []string
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}

// EntityExtractor 实体抽取器接口
type EntityExtractor interface {
	Extract(ctx context.Context, text string) (*models.ExtractionResult, error)
}

// RuleBasedExtractor 基于规则的实体抽取器
type RuleBasedExtractor struct {
	// 已知实体词典（可选，用于提升识别准确度）
	entityDict map[string]models.EntityType
	// 关系模式
	relationPatterns []*relationPattern
}

type relationPattern struct {
	regex   *regexp.Regexp
	relType models.RelationType
}

// NewRuleBasedExtractor 创建基于规则的抽取器
func NewRuleBasedExtractor() *RuleBasedExtractor {
	extractor := &RuleBasedExtractor{
		entityDict: make(map[string]models.EntityType),
	}

	// 注册常见关系模式
	extractor.relationPatterns = []*relationPattern{
		// "A位于B" / "A在B"
		{regexp.MustCompile(`(\S+?)(?:位于|在)(\S+?)`), models.RelLocatedIn},
		// "A属于B" / "A是B的一部分"
		{regexp.MustCompile(`(\S+?)(?:属于|是)(\S+?)(?:的一部分|的)`), models.RelPartOf},
		// "A使用了B"
		{regexp.MustCompile(`(\S+?)(?:使用|采用|利用)(\S+?)`), models.RelUses},
		// "A生产了B" / "A产出B"
		{regexp.MustCompile(`(\S+?)(?:生产|产出|制造|开发)(?:了|出)?(\S+?)`), models.RelProduces},
	}

	return extractor
}

// RegisterEntity 注册已知实体
func (e *RuleBasedExtractor) RegisterEntity(name string, entityType models.EntityType) {
	e.entityDict[strings.ToLower(name)] = entityType
}

// Extract 抽取实体和关系
func (e *RuleBasedExtractor) Extract(ctx context.Context, text string) (*models.ExtractionResult, error) {
	result := &models.ExtractionResult{
		Entities:  []*models.Entity{},
		Relations: []*models.Relation{},
	}

	// 1. 抽取实体
	entities := e.extractEntities(text)
	seen := make(map[string]bool)
	for _, ent := range entities {
		key := strings.ToLower(ent.Name)
		if !seen[key] {
			seen[key] = true
			result.Entities = append(result.Entities, ent)
		}
	}

	// 2. 抽取关系
	relations := e.extractRelations(text, result.Entities)
	for _, rel := range relations {
		result.Relations = append(result.Relations, rel)
	}

	return result, nil
}

// extractEntities 从文本中抽取实体
func (e *RuleBasedExtractor) extractEntities(text string) []*models.Entity {
	var entities []*models.Entity
	seen := make(map[string]bool)

	// 策略1：匹配已知实体词典
	for name, entType := range e.entityDict {
		if strings.Contains(text, name) && !seen[name] {
			seen[name] = true
			entities = append(entities, &models.Entity{
				ID:          uuid.New().String(),
				Name:        name,
				Type:        entType,
				Confidence:  0.9,
				Description: fmt.Sprintf("从词典匹配到的%s实体", entType),
			})
		}
	}

	// 策略2：基于引号/书名号识别命名实体
	quotePatterns := []struct {
		pattern string
		entType models.EntityType
	}{
		{`「(.+?)」`, models.EntityConcept}, // 日式引号
		{`"(.+?)"`, models.EntityConcept}, // 英文引号
		{`《(.+?)》`, models.EntityProduct}, // 书名号（作品/产品）
		{`【(.+?)】`, models.EntityConcept}, // 方括号强调
	}

	for _, qp := range quotePatterns {
		re := regexp.MustCompile(qp.pattern)
		matches := re.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) > 1 {
				name := strings.TrimSpace(match[1])
				if len(name) > 1 && len(name) <= 50 && !seen[strings.ToLower(name)] {
					seen[strings.ToLower(name)] = true
					entities = append(entities, &models.Entity{
						ID:          uuid.New().String(),
						Name:        name,
						Type:        qp.entType,
						Confidence:  0.7,
						Description: fmt.Sprintf("从文本中识别的%s", qp.entType),
					})
				}
			}
		}
	}

	// 策略3：识别连续大写英文单词序列（专有名词）
	re := regexp.MustCompile(`[A-Z][a-z]+(?:\s+[A-Z][a-z]+)+`)
	matches := re.FindAllString(text, -1)
	for _, match := range matches {
		name := strings.TrimSpace(match)
		if len(name) > 2 && !seen[strings.ToLower(name)] {
			seen[strings.ToLower(name)] = true
			entities = append(entities, &models.Entity{
				ID:          uuid.New().String(),
				Name:        name,
				Type:        models.EntityOrganization,
				Confidence:  0.6,
				Description: "识别到的英文专有名词",
			})
		}
	}

	// 策略4：识别中文人名模式（2-4个汉字）
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		// 检查是否是连续2-4个汉字且前后有分隔
		if i > 0 && i+4 <= len(runes) {
			for length := 2; length <= 4; length++ {
				if i+length > len(runes) {
					break
				}
				segment := string(runes[i : i+length])
				// 检查前后是否是非汉字（确保是独立词）
				prevIsHan := i > 0 && isChinese(rune(text[i-1]))
				nextIsHan := i+length < len(runes) && isChinese(rune(text[i+length]))

				// 只取边界处的词（前后至少一侧不是汉字）
				if (!prevIsHan || !nextIsHan) && !seen[strings.ToLower(segment)] {
					// 简单过滤：排除常见虚词
					if !isCommonWord(segment) {
						seen[strings.ToLower(segment)] = true
						entities = append(entities, &models.Entity{
							ID:          uuid.New().String(),
							Name:        segment,
							Type:        models.EntityPerson,
							Confidence:  0.4,
							Description: "基于规则识别的潜在实体",
						})
					}
				}
			}
		}
	}

	return entities
}

// extractRelations 从文本中抽取关系
func (e *RuleBasedExtractor) extractRelations(text string, entities []*models.Entity) []*models.Relation {
	var relations []*models.Relation

	for _, pattern := range e.relationPatterns {
		matches := pattern.regex.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) > 2 {
				sourceName := strings.TrimSpace(match[1])
				targetName := strings.TrimSpace(match[2])

				// 查找匹配的实体
				var sourceID, targetID string
				for _, ent := range entities {
					if strings.Contains(ent.Name, sourceName) && sourceID == "" {
						sourceID = ent.ID
					}
					if strings.Contains(ent.Name, targetName) && targetID == "" {
						targetID = ent.ID
					}
				}

				if sourceID != "" && targetID != "" && sourceID != targetID {
					relations = append(relations, &models.Relation{
						ID:         uuid.New().String(),
						SourceID:   sourceID,
						TargetID:   targetID,
						Type:       pattern.relType,
						Confidence: 0.5,
					})
				}
			}
		}
	}

	return relations
}

// isChinese 判断是否是汉字
func isChinese(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

// commonWords 常见虚词（不应被识别为实体）
var commonWords = map[string]bool{
	"这个": true, "那个": true, "什么": true, "怎么": true, "为什么": true,
	"可以": true, "已经": true, "因为": true, "所以": true, "但是": true,
	"如果": true, "虽然": true, "就是": true, "不是": true, "一个": true,
	"我们": true, "他们": true, "自己": true, "没有": true, "知道": true,
	"技术": true, "方法": true, "系统": true, "问题": true, "进行": true,
}

// isCommonWord 判断是否是常见虚词
func isCommonWord(word string) bool {
	return commonWords[word]
}

// KnowledgeGraph 知识图谱
type KnowledgeGraph struct {
	store     GraphStore
	extractor EntityExtractor
}

// NewKnowledgeGraph 创建知识图谱
func NewKnowledgeGraph(store GraphStore, extractor EntityExtractor) *KnowledgeGraph {
	if extractor == nil {
		extractor = NewRuleBasedExtractor()
	}
	return &KnowledgeGraph{
		store:     store,
		extractor: extractor,
	}
}

// BuildFromDocument 从文档构建图谱
func (kg *KnowledgeGraph) BuildFromDocument(ctx context.Context, docID string, chunks []*models.Chunk) error {
	// 删除旧数据
	if err := kg.store.DeleteByDocument(ctx, docID); err != nil {
		return fmt.Errorf("删除旧数据失败: %w", err)
	}

	// 抽取实体和关系
	for _, chunk := range chunks {
		result, err := kg.extractor.Extract(ctx, chunk.Content)
		if err != nil {
			continue
		}

		// 添加实体
		for _, entity := range result.Entities {
			entity.SourceDocID = docID
			if err := kg.store.AddEntity(ctx, entity); err != nil {
				continue
			}
		}

		// 添加关系
		for _, relation := range result.Relations {
			relation.SourceDocID = docID
			if err := kg.store.AddRelation(ctx, relation); err != nil {
				continue
			}
		}
	}

	return nil
}

// SearchEntities 搜索实体
func (kg *KnowledgeGraph) SearchEntities(ctx context.Context, query string, opts models.SearchOptions) ([]*models.Entity, error) {
	return kg.store.SearchEntities(ctx, query, opts.TopK)
}

// QueryRelations 查询关系
func (kg *KnowledgeGraph) QueryRelations(ctx context.Context, entityID string, relationType models.RelationType) ([]*models.Relation, error) {
	return kg.store.QueryRelations(ctx, entityID, relationType)
}

// GetSubgraph 获取子图
func (kg *KnowledgeGraph) GetSubgraph(ctx context.Context, entityIDs []string, depth int) (*models.Subgraph, error) {
	return kg.store.GetSubgraph(ctx, entityIDs, depth)
}

// FindPath 查找路径
func (kg *KnowledgeGraph) FindPath(ctx context.Context, sourceID, targetID string, maxDepth int) ([]*models.Path, error) {
	return kg.store.FindPath(ctx, sourceID, targetID, maxDepth)
}

// NaturalLanguageQuery 自然语言查询（简化版）
func (kg *KnowledgeGraph) NaturalLanguageQuery(ctx context.Context, question string) (*models.QueryResult, error) {
	// 抽取查询中的实体
	entities, err := kg.store.SearchEntities(ctx, question, 5)
	if err != nil {
		return nil, err
	}

	if len(entities) == 0 {
		return &models.QueryResult{
			Answer: "未找到相关实体",
		}, nil
	}

	// 获取主实体的子图
	entityIDs := make([]string, len(entities))
	for i, e := range entities {
		entityIDs[i] = e.ID
	}

	subgraph, err := kg.store.GetSubgraph(ctx, entityIDs, 2)
	if err != nil {
		return nil, err
	}

	return &models.QueryResult{
		Answer:   "找到相关实体和关系",
		Entities: entities,
		Subgraph: subgraph,
	}, nil
}

// SetExtractor 设置实体抽取器
func (kg *KnowledgeGraph) SetExtractor(extractor EntityExtractor) {
	kg.extractor = extractor
}

// SetStore 设置图谱存储
func (kg *KnowledgeGraph) SetStore(store GraphStore) {
	kg.store = store
}

// GetEntityCount 获取实体数量
func (kg *KnowledgeGraph) GetEntityCount(ctx context.Context) int {
	if memStore, ok := kg.store.(*InMemoryGraphStore); ok {
		memStore.mu.RLock()
		defer memStore.mu.RUnlock()
		return len(memStore.entities)
	}
	return 0
}

// GetRelationCount 获取关系数量
func (kg *KnowledgeGraph) GetRelationCount(ctx context.Context) int {
	if memStore, ok := kg.store.(*InMemoryGraphStore); ok {
		memStore.mu.RLock()
		defer memStore.mu.RUnlock()
		return len(memStore.relations)
	}
	return 0
}

// Clear 清空图谱
func (s *InMemoryGraphStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entities = make(map[string]*models.Entity)
	s.relations = make(map[string]*models.Relation)
	s.entityNameIndex = make(map[string][]string)
	s.relationIndex = make(map[string][]string)
}

// GraphBasedRetrieve 基于图谱的检索
func (kg *KnowledgeGraph) GraphBasedRetrieve(ctx context.Context, query string, topK int) ([]*models.RetrievalResult, error) {
	// 搜索相关实体
	entities, err := kg.store.SearchEntities(ctx, query, 5)
	if err != nil {
		return nil, err
	}

	if len(entities) == 0 {
		return []*models.RetrievalResult{}, nil
	}

	// 获取子图
	entityIDs := make([]string, len(entities))
	for i, e := range entities {
		entityIDs[i] = e.ID
	}

	subgraph, err := kg.store.GetSubgraph(ctx, entityIDs, 2)
	if err != nil {
		return nil, err
	}

	// 构建检索结果
	var results []*models.RetrievalResult
	seenDocs := make(map[string]bool)

	for _, entity := range subgraph.Entities {
		if entity.SourceDocID != "" && !seenDocs[entity.SourceDocID] {
			seenDocs[entity.SourceDocID] = true
			results = append(results, &models.RetrievalResult{
				ID:         entity.ID,
				Content:    fmt.Sprintf("实体: %s (%s)", entity.Name, entity.Type),
				Score:      float64(entity.Confidence),
				DocumentID: entity.SourceDocID,
				Metadata: map[string]interface{}{
					"entity_name": entity.Name,
					"entity_type": entity.Type,
				},
			})
		}
	}

	// 限制结果数
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}

	return results, nil
}
