package auth

import (
	"context"
	"sync"
)

// AccessControl 访问控制器
type AccessControl struct {
	store Store
	mu    sync.RWMutex
}

// Store 权限存储接口
type Store interface {
	GetACL(ctx context.Context, docID string) (*DocumentACL, error)
	SaveACL(ctx context.Context, acl *DocumentACL) error
	DeleteACL(ctx context.Context, docID string) error
	ListUserDocuments(ctx context.Context, userID string) ([]string, error)
}

// DocumentACL 文档访问控制列表
type DocumentACL struct {
	DocID   string   `json:"doc_id"`
	Owner   string   `json:"owner"`   // 所有者
	Readers []string `json:"readers"` // 读取权限用户列表
	Writers []string `json:"writers"` // 写入权限用户列表
	Public  bool     `json:"public"`  // 是否公开
	Groups  []string `json:"groups"`  // 有权限的组
}

// NewAccessControl 创建访问控制器
func NewAccessControl(store Store) *AccessControl {
	return &AccessControl{store: store}
}

// CanRead 检查用户是否有读取权限
func (a *AccessControl) CanRead(ctx context.Context, userID, docID string) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	acl, err := a.store.GetACL(ctx, docID)
	if err != nil {
		return false, err
	}

	// 公开文档
	if acl.Public {
		return true, nil
	}

	// 所有者
	if acl.Owner == userID {
		return true, nil
	}

	// 检查读取权限列表
	for _, reader := range acl.Readers {
		if reader == userID {
			return true, nil
		}
	}

	// 检查写入权限列表（写入权限隐含读取权限）
	for _, writer := range acl.Writers {
		if writer == userID {
			return true, nil
		}
	}

	return false, nil
}

// CanWrite 检查用户是否有写入权限
func (a *AccessControl) CanWrite(ctx context.Context, userID, docID string) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	acl, err := a.store.GetACL(ctx, docID)
	if err != nil {
		return false, err
	}

	// 所有者
	if acl.Owner == userID {
		return true, nil
	}

	// 检查写入权限列表
	for _, writer := range acl.Writers {
		if writer == userID {
			return true, nil
		}
	}

	return false, nil
}

// IsOwner 检查用户是否是所有者
func (a *AccessControl) IsOwner(ctx context.Context, userID, docID string) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	acl, err := a.store.GetACL(ctx, docID)
	if err != nil {
		return false, err
	}

	return acl.Owner == userID, nil
}

// GrantRead 授予读取权限
func (a *AccessControl) GrantRead(ctx context.Context, docID, userID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	acl, err := a.store.GetACL(ctx, docID)
	if err != nil {
		return err
	}

	// 检查是否已有权限
	for _, r := range acl.Readers {
		if r == userID {
			return nil // 已有权限
		}
	}

	acl.Readers = append(acl.Readers, userID)
	return a.store.SaveACL(ctx, acl)
}

// GrantWrite 授予写入权限
func (a *AccessControl) GrantWrite(ctx context.Context, docID, userID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	acl, err := a.store.GetACL(ctx, docID)
	if err != nil {
		return err
	}

	// 检查是否已有权限
	for _, w := range acl.Writers {
		if w == userID {
			return nil // 已有权限
		}
	}

	acl.Writers = append(acl.Writers, userID)
	return a.store.SaveACL(ctx, acl)
}

// RevokeRead 撤销读取权限
func (a *AccessControl) RevokeRead(ctx context.Context, docID, userID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	acl, err := a.store.GetACL(ctx, docID)
	if err != nil {
		return err
	}

	// 从读取列表移除
	newReaders := make([]string, 0, len(acl.Readers))
	for _, r := range acl.Readers {
		if r != userID {
			newReaders = append(newReaders, r)
		}
	}
	acl.Readers = newReaders

	return a.store.SaveACL(ctx, acl)
}

// RevokeWrite 撤销写入权限
func (a *AccessControl) RevokeWrite(ctx context.Context, docID, userID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	acl, err := a.store.GetACL(ctx, docID)
	if err != nil {
		return err
	}

	// 从写入列表移除
	newWriters := make([]string, 0, len(acl.Writers))
	for _, w := range acl.Writers {
		if w != userID {
			newWriters = append(newWriters, w)
		}
	}
	acl.Writers = newWriters

	return a.store.SaveACL(ctx, acl)
}

// SetPublic 设置文档公开状态
func (a *AccessControl) SetPublic(ctx context.Context, docID string, public bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	acl, err := a.store.GetACL(ctx, docID)
	if err != nil {
		return err
	}

	acl.Public = public
	return a.store.SaveACL(ctx, acl)
}

// CreateACL 创建文档ACL
func (a *AccessControl) CreateACL(ctx context.Context, docID, owner string, public bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	acl := &DocumentACL{
		DocID:   docID,
		Owner:   owner,
		Public:  public,
		Readers: []string{},
		Writers: []string{},
		Groups:  []string{},
	}

	return a.store.SaveACL(ctx, acl)
}

// DeleteACL 删除文档ACL
func (a *AccessControl) DeleteACL(ctx context.Context, docID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.store.DeleteACL(ctx, docID)
}

// ListUserDocuments 列出用户有权限的文档
func (a *AccessControl) ListUserDocuments(ctx context.Context, userID string) ([]string, error) {
	return a.store.ListUserDocuments(ctx, userID)
}

// FilterDocuments 过滤用户有权限的文档
func (a *AccessControl) FilterDocuments(ctx context.Context, userID string, docIDs []string) ([]string, error) {
	var result []string
	for _, docID := range docIDs {
		canRead, err := a.CanRead(ctx, userID, docID)
		if err != nil {
			continue // 忽略错误
		}
		if canRead {
			result = append(result, docID)
		}
	}
	return result, nil
}

// NewDocumentACL 创建文档ACL
func NewDocumentACL(docID, owner string) *DocumentACL {
	return &DocumentACL{
		DocID:   docID,
		Owner:   owner,
		Readers: []string{},
		Writers: []string{},
		Groups:  []string{},
		Public:  false,
	}
}

// GetACL 获取文档的ACL
func (ac *AccessControl) GetACL(ctx context.Context, docID string) (*DocumentACL, error) {
	return ac.store.GetACL(ctx, docID)
}

// CanAdmin 检查用户是否有管理员权限
func (ac *AccessControl) CanAdmin(ctx context.Context, userID, docID string) bool {
	acl, err := ac.store.GetACL(ctx, docID)
	if err != nil {
		return false
	}
	return acl.Owner == userID
}

// MemoryStore 内存权限存储（用于测试）
type MemoryStore struct {
	acls map[string]*DocumentACL
	mu   sync.RWMutex
}

// NewMemoryStore 创建内存存储
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		acls: make(map[string]*DocumentACL),
	}
}

// GetACL 获取ACL
func (s *MemoryStore) GetACL(ctx context.Context, docID string) (*DocumentACL, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	acl, ok := s.acls[docID]
	if !ok {
		// 返回默认ACL（不公开，无权限）
		return &DocumentACL{DocID: docID}, nil
	}
	return acl, nil
}

// SaveACL 保存ACL
func (s *MemoryStore) SaveACL(ctx context.Context, acl *DocumentACL) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.acls[acl.DocID] = acl
	return nil
}

// DeleteACL 删除ACL
func (s *MemoryStore) DeleteACL(ctx context.Context, docID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.acls, docID)
	return nil
}

// ListUserDocuments 列出用户文档
func (s *MemoryStore) ListUserDocuments(ctx context.Context, userID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var docs []string
	for docID, acl := range s.acls {
		if acl.Owner == userID || acl.Public {
			docs = append(docs, docID)
			continue
		}
		for _, r := range acl.Readers {
			if r == userID {
				docs = append(docs, docID)
				break
			}
		}
	}
	return docs, nil
}
