// Package permission 负责「当前用户能看到什么」：角色编码、权限标识集合、数据权限范围。
//
// 设计要点：
//   - 权限集合按用户缓存 60 秒，角色/菜单变更时由 service 层主动失效；
//   - 超级管理员（sa_system_user.is_super = 1）跳过权限校验并放开全部数据范围，
//     但角色列表仍按其真实绑定关系返回，避免前端展示误导；
//   - 本包只依赖 gorm 与 model，不引用 gin，便于在 service 与 middleware 中复用。
package permission

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/model"
)

// cacheTTL 是权限缓存有效期。权限变更时会主动失效，TTL 仅作为兜底。
const cacheTTL = 60 * time.Second

// Scope 描述当前用户可见的数据范围。
type Scope struct {
	// All 是否可见全部数据（超级管理员或 data_scope=1）。
	All bool
	// SelfOnly 是否仅可见本人数据。
	SelfOnly bool
	// DeptIDs 允许访问的部门集合（含下级部门）。
	DeptIDs []int64
}

// Entry 是一个用户的权限快照。
type Entry struct {
	// Roles 角色编码集合。
	Roles []string
	// Perms 权限标识集合。
	Perms map[string]bool
	// IsSuper 是否超级管理员。
	IsSuper bool
	// Scope 数据权限范围。
	Scope Scope
	// ExpireAt 缓存过期时间。
	ExpireAt time.Time
}

// Allows 判断快照是否包含指定权限标识（超级管理员恒为 true）。
//
// 参数 Parameters:
//   - perm (string): 权限标识，如 sys:user:create。
//
// 返回 Returns:
//   - allowed (bool): 是否放行。
func (e *Entry) Allows(perm string) bool {
	if e == nil {
		return false
	}
	if e.IsSuper {
		return true
	}
	return e.Perms[perm]
}

// Service 提供权限快照的读取、缓存与失效能力。
type Service struct {
	// db 权限库会话。
	db *gorm.DB
	// mu 保护 items。
	mu sync.RWMutex
	// items 用户 ID 到权限快照的缓存。
	items map[int64]*Entry
	// maxEntries 缓存容量上限，超出时整体清空。
	maxEntries int
}

// New 创建权限服务。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//
// 返回 Returns:
//   - service (*Service): 权限服务实例。
func New(db *gorm.DB) *Service {
	return &Service{db: db, items: make(map[int64]*Entry, 64), maxEntries: 4096}
}

// Of 返回用户的权限快照，必要时回源查询并写缓存。
//
// 参数 Parameters:
//   - user (*model.SysUser): 当前登录用户。
//
// 返回 Returns:
//   - entry (*Entry): 权限快照；查询失败时降级为「仅保留超管标记」的空快照。
func (s *Service) Of(user *model.SysUser) *Entry {
	if entry := s.cached(user.ID); entry != nil {
		return entry
	}
	entry, err := s.Load(user)
	if err != nil {
		// 权限查询失败时降级为「无权限」，但保留超管标记，避免管理员被锁死。
		return &Entry{Perms: map[string]bool{}, IsSuper: IsSuperUser(user), ExpireAt: time.Now().Add(time.Second)}
	}
	s.store(user.ID, entry)
	return entry
}

// Load 从数据库加载用户的角色、权限标识与数据范围。
//
// 参数 Parameters:
//   - user (*model.SysUser): 当前登录用户。
//
// 返回 Returns:
//   - entry (*Entry): 权限快照。
//   - err (error): 查询失败时返回非 nil。
func (s *Service) Load(user *model.SysUser) (*Entry, error) {
	entry := &Entry{Perms: map[string]bool{}, IsSuper: IsSuperUser(user), ExpireAt: time.Now().Add(cacheTTL)}
	if entry.IsSuper {
		return s.loadSuper(entry, user)
	}
	// 个人菜单（sa_system_user_menu）优先并入：它与角色无关，
	// 必须放在「无角色直接返回」之前，否则被单独授权菜单的无角色用户拿不到任何权限。
	personalSlugs, err := s.userMenuSlugs(user.ID)
	if err != nil {
		return nil, err
	}
	for _, slug := range personalSlugs {
		entry.Perms[slug] = true
	}
	roleIDs, err := s.userRoleIDs(user.ID)
	if err != nil {
		return nil, err
	}
	if len(roleIDs) == 0 {
		entry.Scope = Scope{SelfOnly: true}
		return entry, nil
	}
	roles, err := s.roles(roleIDs)
	if err != nil {
		return nil, err
	}
	for _, role := range roles {
		entry.Roles = append(entry.Roles, role.Code)
	}
	slugs, err := s.roleSlugs(roleIDs)
	if err != nil {
		return nil, err
	}
	// 角色菜单与个人菜单取并集：单独授予某人的菜单，其 slug 也要生效，
	// 否则侧边栏出现该菜单，但页面内的按钮被 v-hasPerm 隐藏。
	for _, slug := range slugs {
		entry.Perms[slug] = true
	}
	scope, err := s.resolveScope(user, roles)
	if err != nil {
		return nil, err
	}
	entry.Scope = scope
	return entry, nil
}

// Invalidate 使指定用户的权限缓存失效。
//
// 参数 Parameters:
//   - userID (int64): 用户主键。
func (s *Service) Invalidate(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, userID)
}

// InvalidateAll 清空全部权限缓存（角色、菜单变更后调用）。
func (s *Service) InvalidateAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make(map[int64]*Entry, 64)
}

// CacheEntry 是权限缓存的一条摘要（仅供缓存管理页展示）。
type CacheEntry struct {
	// UserID 用户主键。
	UserID int64
	// Roles 角色编码集合。
	Roles []string
	// PermCount 权限标识数量。
	PermCount int
	// IsSuper 是否超级管理员。
	IsSuper bool
	// ExpireAt 缓存过期时间。
	ExpireAt time.Time
}

// Snapshot 返回当前权限缓存快照（已过期的条目会被忽略）。
//
// 返回 Returns:
//   - entries ([]CacheEntry): 缓存摘要，按用户 ID 升序。
func (s *Service) Snapshot() []CacheEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	entries := make([]CacheEntry, 0, len(s.items))
	for userID, entry := range s.items {
		if entry == nil || now.After(entry.ExpireAt) {
			continue
		}
		entries = append(entries, CacheEntry{
			UserID:    userID,
			Roles:     entry.Roles,
			PermCount: len(entry.Perms),
			IsSuper:   entry.IsSuper,
			ExpireAt:  entry.ExpireAt,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].UserID < entries[j].UserID })
	return entries
}

// UserDeptIDs 返回用户归属的部门（主部门 + sa_system_user_dept 关联部门）。
//
// 参数 Parameters:
//   - user (*model.SysUser): 当前登录用户。
//
// 返回 Returns:
//   - ids ([]int64): 部门主键列表，可能为空。
//   - err (error): 查询失败时返回非 nil。
func (s *Service) UserDeptIDs(user *model.SysUser) ([]int64, error) {
	ids := make([]int64, 0, 4)
	if user.DeptID != nil && *user.DeptID > 0 {
		ids = append(ids, *user.DeptID)
	}
	var related []int64
	if err := s.db.Model(&model.SysUserDept{}).Where("user_id = ?", user.ID).Pluck("dept_id", &related).Error; err != nil {
		return nil, err
	}
	for _, id := range related {
		duplicated := false
		for _, exist := range ids {
			if exist == id {
				duplicated = true
				break
			}
		}
		if !duplicated {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// ApplyUserScope 把数据范围条件应用到用户查询上（目前仅用户列表需要）。
//
// 参数 Parameters:
//   - db (*gorm.DB): 目标查询会话。
//   - scope (Scope): 当前用户的数据范围。
//   - userID (int64): 当前用户主键，用于「仅本人」场景。
//
// 返回 Returns:
//   - tx (*gorm.DB): 追加了范围条件的会话。
func ApplyUserScope(db *gorm.DB, scope Scope, userID int64) *gorm.DB {
	if scope.All {
		return db
	}
	if len(scope.DeptIDs) > 0 {
		return db.Where("(dept_id IN ? OR id = ?)", scope.DeptIDs, userID)
	}
	return db.Where("id = ?", userID)
}

// cached 读取未过期的权限快照。
func (s *Service) cached(userID int64) *Entry {
	s.mu.RLock()
	entry, ok := s.items[userID]
	s.mu.RUnlock()
	if !ok || entry == nil || time.Now().After(entry.ExpireAt) {
		return nil
	}
	return entry
}

// store 写入权限快照。
func (s *Service) store(userID int64, entry *Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) >= s.maxEntries {
		s.items = make(map[int64]*Entry, 64)
	}
	s.items[userID] = entry
}

// loadSuper 装配超级管理员的权限快照：权限全量、数据范围全开，角色按真实绑定返回。
func (s *Service) loadSuper(entry *Entry, user *model.SysUser) (*Entry, error) {
	var codes []string
	if err := s.db.Table("sa_system_role AS r").
		Select("r.code").
		Joins("JOIN sa_system_user_role AS ur ON ur.role_id = r.id AND ur.status = 1").
		Where("ur.user_id = ? AND r.status = 1 AND r.delete_time IS NULL", user.ID).
		Order("r.sort").
		Scan(&codes).Error; err != nil {
		return nil, err
	}
	entry.Roles = append(entry.Roles, codes...)
	var slugs []string
	if err := s.db.Model(&model.SysMenu{}).
		Where("slug IS NOT NULL AND slug <> '' AND status = 1").
		Pluck("slug", &slugs).Error; err != nil {
		return nil, err
	}
	for _, slug := range slugs {
		entry.Perms[slug] = true
	}
	entry.Scope = Scope{All: true}
	return entry, nil
}

// userMenuSlugs 返回用户个人菜单（sa_system_user_menu）对应的权限标识集合。
//
// 参数 Parameters:
//   - userID (int64): 用户主键。
//
// 返回 Returns:
//   - slugs ([]string): 权限标识列表。
//   - err (error): 查询失败时返回非 nil。
func (s *Service) userMenuSlugs(userID int64) ([]string, error) {
	var menuIDs []int64
	if err := s.db.Model(&model.SysUserMenu{}).Where("user_id = ?", userID).Pluck("menu_id", &menuIDs).Error; err != nil {
		return nil, err
	}
	if len(menuIDs) == 0 {
		return nil, nil
	}
	var slugs []string
	err := s.db.Model(&model.SysMenu{}).
		Where("id IN ? AND slug IS NOT NULL AND slug <> '' AND status = 1", menuIDs).
		Pluck("slug", &slugs).Error
	return slugs, err
}

// IsSuperUser 判断用户是否为超级管理员：is_super = 1，或主键为 1（内置 admin 账号）。
//
// 为什么额外判 id == 1：内置 admin 的 is_super 一旦被误改，管理员会立刻失去全部菜单、
// 且无法自助恢复；主键 1 是种子数据固定的内置账号，作为兜底判定。
//
// 参数 Parameters:
//   - user (*model.SysUser): 用户实体。
//
// 返回 Returns:
//   - isSuper (bool): 超级管理员返回 true。
func IsSuperUser(user *model.SysUser) bool {
	if user == nil {
		return false
	}
	return user.IsSuper == 1 || user.ID == 1
}

// userRoleIDs 返回用户启用的角色主键。
func (s *Service) userRoleIDs(userID int64) ([]int64, error) {
	var roleIDs []int64
	err := s.db.Model(&model.SysUserRole{}).
		Where("user_id = ? AND status = 1", userID).
		Pluck("role_id", &roleIDs).Error
	return roleIDs, err
}

// roles 返回启用的角色实体，按排序输出。
func (s *Service) roles(roleIDs []int64) ([]model.SysRole, error) {
	var roles []model.SysRole
	err := s.db.Where("id IN ? AND status = 1", roleIDs).Order("sort").Find(&roles).Error
	return roles, err
}

// roleSlugs 返回角色已授权菜单的权限标识集合。
func (s *Service) roleSlugs(roleIDs []int64) ([]string, error) {
	var menuIDs []int64
	if err := s.db.Model(&model.SysRoleMenu{}).Where("role_id IN ?", roleIDs).Pluck("menu_id", &menuIDs).Error; err != nil {
		return nil, err
	}
	if len(menuIDs) == 0 {
		return nil, nil
	}
	var slugs []string
	err := s.db.Model(&model.SysMenu{}).
		Where("id IN ? AND slug IS NOT NULL AND slug <> '' AND status = 1", menuIDs).
		Pluck("slug", &slugs).Error
	return slugs, err
}

// resolveScope 依据角色 data_scope 计算用户可见部门集合。
//
// 参数 Parameters:
//   - user (*model.SysUser): 当前登录用户。
//   - roles ([]model.SysRole): 用户拥有的启用角色。
//
// 返回 Returns:
//   - scope (Scope): 数据范围；data_scope=1 时 All=true。
//   - err (error): 部门查询失败时返回非 nil。
func (s *Service) resolveScope(user *model.SysUser, roles []model.SysRole) (Scope, error) {
	scope := Scope{}
	ownDeptIDs, err := s.UserDeptIDs(user)
	if err != nil {
		return scope, err
	}
	type deptLevel struct {
		ID    int64
		Level string
	}
	var depts []deptLevel
	if err := s.db.Model(&model.SysDept{}).Select("id", "level").Where("status = 1").Find(&depts).Error; err != nil {
		return scope, err
	}
	levels := make(map[int64]string, len(depts))
	for _, dept := range depts {
		levels[dept.ID] = dept.Level
	}
	allowed := map[int64]bool{}
	appendWithDescendants := func(ids []int64) {
		for _, id := range ids {
			allowed[id] = true
			if levels[id] == "" {
				continue
			}
			prefix := levels[id] + strconv.FormatInt(id, 10) + ","
			for _, dept := range depts {
				if dept.ID != id && strings.HasPrefix(dept.Level, prefix) {
					allowed[dept.ID] = true
				}
			}
		}
	}
	// needDeptOnly 对应 data_scope=3（仅本部门），needDeptTree 对应 data_scope=2（本部门及下属）。
	needDeptOnly := false
	needDeptTree := false
	for _, role := range roles {
		switch role.DataScope {
		case 1:
			scope.All = true
			return scope, nil
		case 2:
			needDeptTree = true
		case 3:
			needDeptOnly = true
		case 5:
			var roleDeptIDs []int64
			if err := s.db.Model(&model.SysRoleDept{}).Where("role_id = ?", role.ID).Pluck("dept_id", &roleDeptIDs).Error; err != nil {
				return scope, err
			}
			appendWithDescendants(roleDeptIDs)
		}
	}
	if needDeptTree {
		appendWithDescendants(ownDeptIDs)
	}
	if needDeptOnly {
		for _, id := range ownDeptIDs {
			allowed[id] = true
		}
	}
	ids := make([]int64, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		scope.SelfOnly = true
		return scope, nil
	}
	scope.DeptIDs = ids
	return scope, nil
}
