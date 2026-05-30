package models

// EntityType 实体类型
type EntityType string

const (
	EntityPerson       EntityType = "PERSON"
	EntityOrganization EntityType = "ORGANIZATION"
	EntityLocation     EntityType = "LOCATION"
	EntityDate         EntityType = "DATE"
	EntityProduct      EntityType = "PRODUCT"
	EntityEvent        EntityType = "EVENT"
	EntityConcept      EntityType = "CONCEPT"
	EntityTechnology   EntityType = "TECHNOLOGY"
	EntityIndustry     EntityType = "INDUSTRY"
)

// RelationType 关系类型
type RelationType string

const (
	RelWorksFor      RelationType = "WORKS_FOR"
	RelLocatedIn     RelationType = "LOCATED_IN"
	RelFoundedBy     RelationType = "FOUNDED_BY"
	RelPartOf        RelationType = "PART_OF"
	RelRelatedTo     RelationType = "RELATED_TO"
	RelUses          RelationType = "USES"
	RelProduces      RelationType = "PRODUCES"
	RelCompetesWith  RelationType = "COMPETES_WITH"
	RelInvestedIn    RelationType = "INVESTED_IN"
	RelAcquired      RelationType = "ACQUIRED"
	RelPartneredWith RelationType = "PARTNERED_WITH"
)

// Entity 实体模型
type Entity struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        EntityType             `json:"type"`
	Aliases     []string               `json:"aliases"` // 别名
	Description string                 `json:"description"`
	Properties  map[string]interface{} `json:"properties"`
	SourceDocID string                 `json:"source_doc_id"`
	Confidence  float64                `json:"confidence"`
}

// Relation 关系模型
type Relation struct {
	ID          string                 `json:"id"`
	SourceID    string                 `json:"source_id"` // 源实体ID
	TargetID    string                 `json:"target_id"` // 目标实体ID
	Type        RelationType           `json:"type"`
	Properties  map[string]interface{} `json:"properties"`
	SourceDocID string                 `json:"source_doc_id"`
	Confidence  float64                `json:"confidence"`
}

// Subgraph 子图
type Subgraph struct {
	Entities  []*Entity   `json:"entities"`
	Relations []*Relation `json:"relations"`
}

// Path 路径
type Path struct {
	Nodes  []*Entity   `json:"nodes"`
	Edges  []*Relation `json:"edges"`
	Length int         `json:"length"`
}

// QueryResult 查询结果
type QueryResult struct {
	Answer   string    `json:"answer"`
	Entities []*Entity `json:"entities"`
	Paths    []*Path   `json:"paths"`
	Subgraph *Subgraph `json:"subgraph"`
}

// ExtractionResult 抽取结果
type ExtractionResult struct {
	Entities  []*Entity   `json:"entities"`
	Relations []*Relation `json:"relations"`
}
