package knowledgegraph

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// PGGraphStore PostgreSQL图存储实现
type PGGraphStore struct {
	db      *sql.DB
	connStr string
	mu      sync.RWMutex // 用于内存索引的并发保护
	// 内存索引（加速查询）
	entityNameIndex map[string][]string // name -> entity IDs
	relationIndex   map[string][]string // entityID -> relation IDs
}

// PGGraphStoreConfig PostgreSQL存储配置
type PGGraphStoreConfig struct {
	ConnectionString string // PostgreSQL连接字符串
}

// NewPGGraphStore 创建PostgreSQL图存储
func NewPGGraphStore(config PGGraphStoreConfig) (*PGGraphStore, error) {
	if config.ConnectionString == "" {
		return nil, fmt.Errorf("连接字符串不能为空")
	}

	db, err := sql.Open("postgres", config.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("打开PostgreSQL数据库失败: %w", err)
	}

	// 设置连接池参数
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// 验证连接
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("连接PostgreSQL数据库失败: %w", err)
	}

	store := &PGGraphStore{
		db:              db,
		connStr:         config.ConnectionString,
		entityNameIndex: make(map[string][]string),
		relationIndex:   make(map[string][]string),
	}

	return store, nil
}

// InitSchema 初始化数据库表结构
func (s *PGGraphStore) InitSchema(ctx context.Context) error {
	// 实体表（使用JSONB存储properties）
	entityTable := `
	CREATE TABLE IF NOT EXISTS entities (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL,
		type        TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		confidence  DOUBLE PRECISION NOT NULL DEFAULT 0.0,
		properties  JSONB NOT NULL DEFAULT '{}',
		document_id TEXT NOT NULL DEFAULT '',
		aliases     TEXT[] NOT NULL DEFAULT '{}',
		created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_entities_name ON entities(name);
	CREATE INDEX IF NOT EXISTS idx_entities_type ON entities(type);
	CREATE INDEX IF NOT EXISTS idx_entities_document_id ON entities(document_id);
	CREATE INDEX IF NOT EXISTS idx_entities_properties ON entities USING GIN (properties);
	`

	if _, err := s.db.ExecContext(ctx, entityTable); err != nil {
		return fmt.Errorf("创建实体表失败: %w", err)
	}

	// 关系表
	relationTable := `
	CREATE TABLE IF NOT EXISTS relations (
		id          TEXT PRIMARY KEY,
		source_id   TEXT NOT NULL,
		target_id   TEXT NOT NULL,
		type        TEXT NOT NULL,
		confidence  DOUBLE PRECISION NOT NULL DEFAULT 0.0,
		description TEXT NOT NULL DEFAULT '',
		properties  JSONB NOT NULL DEFAULT '{}',
		document_id TEXT NOT NULL DEFAULT '',
		created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
		FOREIGN KEY (source_id) REFERENCES entities(id) ON DELETE CASCADE,
		FOREIGN KEY (target_id) REFERENCES entities(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_relations_source_id ON relations(source_id);
	CREATE INDEX IF NOT EXISTS idx_relations_target_id ON relations(target_id);
	CREATE INDEX IF NOT EXISTS idx_relations_type ON relations(type);
	CREATE INDEX IF NOT EXISTS idx_relations_document_id ON relations(document_id);
	CREATE INDEX IF NOT EXISTS idx_relations_properties ON relations USING GIN (properties);
	`

	if _, err := s.db.ExecContext(ctx, relationTable); err != nil {
		return fmt.Errorf("创建关系表失败: %w", err)
	}

	// 加载现有数据到内存索引
	if err := s.loadIndexes(ctx); err != nil {
		return fmt.Errorf("加载索引失败: %w", err)
	}

	return nil
}

// loadIndexes 加载现有数据到内存索引
func (s *PGGraphStore) loadIndexes(ctx context.Context) error {
	// 加载实体名称索引
	entityRows, err := s.db.QueryContext(ctx, "SELECT id, name, aliases FROM entities")
	if err != nil {
		return fmt.Errorf("查询实体失败: %w", err)
	}
	defer entityRows.Close()

	for entityRows.Next() {
		var id, name string
		var aliases pq.StringArray
		if err := entityRows.Scan(&id, &name, &aliases); err != nil {
			return fmt.Errorf("扫描实体失败: %w", err)
		}

		nameKey := strings.ToLower(name)
		s.entityNameIndex[nameKey] = append(s.entityNameIndex[nameKey], id)

		// 索引别名
		for _, alias := range aliases {
			aliasKey := strings.ToLower(alias)
			s.entityNameIndex[aliasKey] = append(s.entityNameIndex[aliasKey], id)
		}
	}

	// 加载关系索引
	relationRows, err := s.db.QueryContext(ctx, "SELECT id, source_id, target_id FROM relations")
	if err != nil {
		return fmt.Errorf("查询关系失败: %w", err)
	}
	defer relationRows.Close()

	for relationRows.Next() {
		var id, sourceID, targetID string
		if err := relationRows.Scan(&id, &sourceID, &targetID); err != nil {
			return fmt.Errorf("扫描关系失败: %w", err)
		}

		s.relationIndex[sourceID] = append(s.relationIndex[sourceID], id)
		s.relationIndex[targetID] = append(s.relationIndex[targetID], id)
	}

	return nil
}

// Close 关闭数据库连接
func (s *PGGraphStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// DB 返回底层*sql.DB
func (s *PGGraphStore) DB() *sql.DB {
	return s.db
}

// AddEntity 添加实体
func (s *PGGraphStore) AddEntity(ctx context.Context, entity *models.Entity) error {
	if entity.ID == "" {
		entity.ID = uuid.New().String()
	}

	propertiesJSON, err := json.Marshal(entity.Properties)
	if err != nil {
		return fmt.Errorf("序列化属性失败: %w", err)
	}

	query := `
		INSERT INTO entities (id, name, type, description, confidence, properties, document_id, aliases, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	aliases := pq.StringArray(entity.Aliases)

	_, err = s.db.ExecContext(ctx, query,
		entity.ID, entity.Name, string(entity.Type), entity.Description,
		entity.Confidence, string(propertiesJSON), entity.SourceDocID, aliases,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("插入实体失败: %w", err)
	}

	// 更新内存索引
	s.mu.Lock()
	defer s.mu.Unlock()

	nameKey := strings.ToLower(entity.Name)
	s.entityNameIndex[nameKey] = append(s.entityNameIndex[nameKey], entity.ID)

	for _, alias := range entity.Aliases {
		aliasKey := strings.ToLower(alias)
		s.entityNameIndex[aliasKey] = append(s.entityNameIndex[aliasKey], entity.ID)
	}

	return nil
}

// AddRelation 添加关系
func (s *PGGraphStore) AddRelation(ctx context.Context, relation *models.Relation) error {
	if relation.ID == "" {
		relation.ID = uuid.New().String()
	}

	propertiesJSON, err := json.Marshal(relation.Properties)
	if err != nil {
		return fmt.Errorf("序列化属性失败: %w", err)
	}

	// 获取关系的描述（从properties中提取，如果没有则为空）
	description := ""
	if desc, ok := relation.Properties["description"].(string); ok {
		description = desc
	}

	query := `
		INSERT INTO relations (id, source_id, target_id, type, confidence, description, properties, document_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err = s.db.ExecContext(ctx, query,
		relation.ID, relation.SourceID, relation.TargetID, string(relation.Type),
		relation.Confidence, description, string(propertiesJSON), relation.SourceDocID,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("插入关系失败: %w", err)
	}

	// 更新内存索引
	s.mu.Lock()
	defer s.mu.Unlock()

	s.relationIndex[relation.SourceID] = append(s.relationIndex[relation.SourceID], relation.ID)
	s.relationIndex[relation.TargetID] = append(s.relationIndex[relation.TargetID], relation.ID)

	return nil
}

// GetEntity 获取实体
func (s *PGGraphStore) GetEntity(ctx context.Context, id string) (*models.Entity, error) {
	query := `
		SELECT id, name, type, description, confidence, properties, document_id, aliases
		FROM entities
		WHERE id = $1
	`

	var entity models.Entity
	var entityType string
	var propertiesJSON []byte
	var aliases pq.StringArray

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&entity.ID, &entity.Name, &entityType, &entity.Description,
		&entity.Confidence, &propertiesJSON, &entity.SourceDocID, &aliases,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("实体不存在: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询实体失败: %w", err)
	}

	entity.Type = models.EntityType(entityType)
	entity.Aliases = []string(aliases)

	if err := json.Unmarshal(propertiesJSON, &entity.Properties); err != nil {
		entity.Properties = make(map[string]interface{})
	}

	return &entity, nil
}

// GetRelation 获取关系
func (s *PGGraphStore) GetRelation(ctx context.Context, id string) (*models.Relation, error) {
	query := `
		SELECT id, source_id, target_id, type, confidence, properties, document_id
		FROM relations
		WHERE id = $1
	`

	var relation models.Relation
	var relationType string
	var propertiesJSON []byte

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&relation.ID, &relation.SourceID, &relation.TargetID, &relationType,
		&relation.Confidence, &propertiesJSON, &relation.SourceDocID,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("关系不存在: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询关系失败: %w", err)
	}

	relation.Type = models.RelationType(relationType)

	if err := json.Unmarshal(propertiesJSON, &relation.Properties); err != nil {
		relation.Properties = make(map[string]interface{})
	}

	return &relation, nil
}

// SearchEntities 搜索实体
func (s *PGGraphStore) SearchEntities(ctx context.Context, query string, limit int) ([]*models.Entity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query = strings.ToLower(query)
	var results []*models.Entity
	seen := make(map[string]bool)

	// 先通过内存索引查找
	if ids, ok := s.entityNameIndex[query]; ok {
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				entity, err := s.GetEntity(ctx, id)
				if err == nil {
					results = append(results, entity)
				}
			}
		}
	}

	// 模糊匹配
	for name, ids := range s.entityNameIndex {
		if strings.Contains(name, query) || strings.Contains(query, name) {
			for _, id := range ids {
				if !seen[id] {
					seen[id] = true
					entity, err := s.GetEntity(ctx, id)
					if err == nil {
						results = append(results, entity)
					}
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
func (s *PGGraphStore) QueryRelations(ctx context.Context, entityID string, relationType models.RelationType) ([]*models.Relation, error) {
	s.mu.RLock()
	relationIDs, ok := s.relationIndex[entityID]
	s.mu.RUnlock()

	if !ok {
		return []*models.Relation{}, nil
	}

	var results []*models.Relation

	for _, id := range relationIDs {
		relation, err := s.GetRelation(ctx, id)
		if err != nil {
			continue
		}

		if relationType == "" || relation.Type == relationType {
			results = append(results, relation)
		}
	}

	return results, nil
}

// GetSubgraph 获取子图
func (s *PGGraphStore) GetSubgraph(ctx context.Context, entityIDs []string, depth int) (*models.Subgraph, error) {
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

			entity, err := s.GetEntity(ctx, entityID)
			if err != nil {
				continue
			}
			subgraph.Entities = append(subgraph.Entities, entity)

			// 获取相关关系
			s.mu.RLock()
			relationIDs, ok := s.relationIndex[entityID]
			s.mu.RUnlock()

			if !ok {
				continue
			}

			for _, relationID := range relationIDs {
				if relationSet[relationID] {
					continue
				}
				relationSet[relationID] = true

				relation, err := s.GetRelation(ctx, relationID)
				if err != nil {
					continue
				}
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
func (s *PGGraphStore) FindPath(ctx context.Context, sourceID, targetID string, maxDepth int) ([]*models.Path, error) {
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

		entity, err := s.GetEntity(ctx, currentID)
		if err != nil {
			return
		}

		visited[currentID] = true
		currentPath = append(currentPath, entity)

		s.mu.RLock()
		relationIDs, ok := s.relationIndex[currentID]
		s.mu.RUnlock()

		if ok {
			for _, relationID := range relationIDs {
				relation, err := s.GetRelation(ctx, relationID)
				if err != nil {
					continue
				}

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
func (s *PGGraphStore) DeleteByDocument(ctx context.Context, docID string) error {
	// 获取要删除的实体和关系
	var entityIDs []string
	entityRows, err := s.db.QueryContext(ctx, "SELECT id, name, aliases FROM entities WHERE document_id = $1", docID)
	if err != nil {
		return fmt.Errorf("查询实体失败: %w", err)
	}
	defer entityRows.Close()

	for entityRows.Next() {
		var id, name string
		var aliases pq.StringArray
		if err := entityRows.Scan(&id, &name, &aliases); err != nil {
			continue
		}
		entityIDs = append(entityIDs, id)

		// 从内存索引中删除
		s.mu.Lock()
		nameKey := strings.ToLower(name)
		s.entityNameIndex[nameKey] = removeFromSlice(s.entityNameIndex[nameKey], id)

		for _, alias := range aliases {
			aliasKey := strings.ToLower(alias)
			s.entityNameIndex[aliasKey] = removeFromSlice(s.entityNameIndex[aliasKey], id)
		}
		s.mu.Unlock()
	}

	// 获取要删除的关系ID
	var relationIDs []string
	relationRows, err := s.db.QueryContext(ctx, "SELECT id, source_id, target_id FROM relations WHERE document_id = $1", docID)
	if err != nil {
		return fmt.Errorf("查询关系失败: %w", err)
	}
	defer relationRows.Close()

	for relationRows.Next() {
		var id, sourceID, targetID string
		if err := relationRows.Scan(&id, &sourceID, &targetID); err != nil {
			continue
		}
		relationIDs = append(relationIDs, id)

		// 从内存索引中删除
		s.mu.Lock()
		s.relationIndex[sourceID] = removeFromSlice(s.relationIndex[sourceID], id)
		s.relationIndex[targetID] = removeFromSlice(s.relationIndex[targetID], id)
		s.mu.Unlock()
	}

	// 删除数据库中的数据
	if _, err := s.db.ExecContext(ctx, "DELETE FROM relations WHERE document_id = $1", docID); err != nil {
		return fmt.Errorf("删除关系失败: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, "DELETE FROM entities WHERE document_id = $1", docID); err != nil {
		return fmt.Errorf("删除实体失败: %w", err)
	}

	return nil
}
