package router_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/company/monitor-webserver/api/middleware"
	"github.com/company/monitor-webserver/api/router"
	v1 "github.com/company/monitor-webserver/api/v1"
	"github.com/company/monitor-webserver/internal/app"
	"github.com/company/monitor-webserver/internal/iploc"
	"github.com/company/monitor-webserver/internal/logic"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/monitor"
	"github.com/company/monitor-webserver/internal/permission"
	"github.com/company/monitor-webserver/internal/service"
	"github.com/company/monitor-webserver/internal/session"
	"github.com/company/monitor-webserver/internal/util"
)

// testEnv 是集成测试的运行环境。
type testEnv struct {
	// engine 完整装配后的 Gin 引擎。
	engine *gin.Engine
	// db 权限库会话。
	db *gorm.DB
}

// newEnv 装配一套基于临时 SQLite 的完整路由（监控库同样指向 SQLite，仅用于验证路由接线）。
func newEnv(t *testing.T) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "webserver.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := app.Migrate(db, ""); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	// Windows 下 SQLite 文件会保持占用，测试结束前显式关闭连接。
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	perms := permission.New(db)
	// 测试环境不需要归属地查询与跨实例强退登记：前者用禁用态（直接返回兜底标签），
	// 后者用无 Redis 的进程内实现。
	locator := iploc.New(iploc.Config{Enabled: false}, nil)
	sessionGuard := session.NewGuard(nil)
	authService := service.NewAuthService(logic.NewAuthLogic(db), perms, service.TokenConfig{
		Secret: "test-secret", AccessSeconds: 3600, RefreshDays: 1,
	}, locator, sessionGuard)
	userLogic := logic.NewUserLogic(db)
	roleLogic := logic.NewRoleLogic(db)
	deptLogic := logic.NewDeptLogic(db)
	menuLogic := logic.NewMenuLogic(db)

	engine := router.New(router.Deps{
		Auth:        v1.NewAuthAPI(authService, nil, v1.CaptchaConfig{Enabled: false}),
		Users:       v1.NewUserAPI(service.NewUserService(userLogic, deptLogic, perms)),
		Roles:       v1.NewRoleAPI(service.NewRoleService(roleLogic, perms)),
		Depts:       v1.NewDeptAPI(service.NewDeptService(deptLogic)),
		Menus:       v1.NewMenuAPI(service.NewMenuService(menuLogic, roleLogic, userLogic, perms)),
		Posts:       v1.NewPostAPI(service.NewPostService(logic.NewPostLogic(db))),
		Dicts:       v1.NewDictAPI(service.NewDictService(logic.NewDictLogic(db), nil, nil)),
		Configs:     v1.NewConfigAPI(service.NewConfigService(logic.NewConfigLogic(db), nil)),
		Notices:     v1.NewNoticeAPI(service.NewNoticeService(logic.NewNoticeLogic(db))),
		Logs:        v1.NewLogAPI(service.NewLogService(logic.NewLogLogic(db))),
		Monitor:     v1.NewMonitorAPI(monitor.New(db, nil, "", ""), nil),
		AuthService: authService,
		Perms:       perms,
		OperateLog:  middleware.OperateLog(db, locator),
	})
	return &testEnv{engine: engine, db: db}
}

// do 发起一次请求并返回响应体（已解码为 map）。
func (env *testEnv) do(t *testing.T, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	env.engine.ServeHTTP(recorder, req)
	decoded := map[string]any{}
	_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
	return recorder.Code, decoded
}

// login 登录并返回访问令牌。
func (env *testEnv) login(t *testing.T, username, password string) (int, string) {
	t.Helper()
	status, body := env.do(t, http.MethodPost, "/api/v1/auth/login", "",
		`{"username":"`+username+`","password":"`+password+`"}`)
	data, _ := body["data"].(map[string]any)
	token, _ := data["accessToken"].(string)
	return status, token
}

// TestAuthAndHealthz 覆盖探活、错误凭据与管理员登录。
func TestAuthAndHealthz(t *testing.T) {
	env := newEnv(t)

	status, body := env.do(t, http.MethodGet, "/healthz", "", "")
	if status != http.StatusOK || body["code"] != "00000" {
		t.Fatalf("探活失败: status=%d body=%v", status, body)
	}

	status, body = env.do(t, http.MethodPost, "/api/v1/auth/login", "",
		`{"username":"admin","password":"wrong-password"}`)
	if status != http.StatusUnauthorized || body["code"] != "A0210" {
		t.Fatalf("错误密码应返回 401/A0210，实际 status=%d body=%v", status, body)
	}

	status, token := env.login(t, "admin", "Monitor123!")
	if status != http.StatusOK || token == "" {
		t.Fatalf("管理员登录失败: status=%d token=%q", status, token)
	}
}

// TestMeAndDynamicRoutes 覆盖当前用户信息与数据库驱动的动态菜单。
func TestMeAndDynamicRoutes(t *testing.T) {
	env := newEnv(t)
	_, token := env.login(t, "admin", "Monitor123!")
	if token == "" {
		t.Fatal("登录失败，无法继续")
	}

	status, body := env.do(t, http.MethodGet, "/api/v1/users/me", token, "")
	if status != http.StatusOK {
		t.Fatalf("查询当前用户失败: status=%d body=%v", status, body)
	}
	data, _ := body["data"].(map[string]any)
	if data["isSuperAdmin"] != true {
		t.Fatalf("管理员应标记为超管: %v", data)
	}
	perms, _ := data["perms"].([]any)
	if len(perms) == 0 {
		t.Fatalf("管理员权限集合为空: %v", data)
	}

	status, body = env.do(t, http.MethodGet, "/api/v1/menus/routes", token, "")
	if status != http.StatusOK {
		t.Fatalf("查询动态菜单失败: status=%d body=%v", status, body)
	}
	routes, _ := body["data"].([]any)
	paths := make([]string, 0, len(routes))
	for _, item := range routes {
		node, _ := item.(map[string]any)
		path, _ := node["path"].(string)
		paths = append(paths, path)
	}
	if !contains(paths, "/monitor") || !contains(paths, "/system") {
		t.Fatalf("动态菜单缺少监控中心或系统管理: %v", paths)
	}
}

// TestAuthorizationAndAudit 覆盖未登录 401、越权 403 与操作日志落库。
func TestAuthorizationAndAudit(t *testing.T) {
	env := newEnv(t)

	status, body := env.do(t, http.MethodGet, "/api/v1/users", "", "")
	if status != http.StatusUnauthorized || body["code"] != "A0230" {
		t.Fatalf("未登录访问应返回 401/A0230，实际 status=%d body=%v", status, body)
	}

	// 造一个无任何权限的普通用户（无角色），验证写操作被拒绝。
	hash, err := bcrypt.GenerateFromPassword([]byte("viewer123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("生成密码失败: %v", err)
	}
	viewer := model.SysUser{Username: "viewer", Password: string(hash), Realname: "只读用户", Status: 1}
	if err := env.db.Create(&viewer).Error; err != nil {
		t.Fatalf("创建只读用户失败: %v", err)
	}
	_, viewerToken := env.login(t, "viewer", "viewer123")
	if viewerToken == "" {
		t.Fatal("只读用户登录失败")
	}
	status, body = env.do(t, http.MethodPost, "/api/v1/posts", viewerToken,
		`{"name":"越权岗位","code":"nope","sort":1,"status":1}`)
	if status != http.StatusForbidden || body["code"] != "A0301" {
		t.Fatalf("越权写操作应返回 403/A0301，实际 status=%d body=%v", status, body)
	}

	// 管理员写操作应成功，并写入操作日志。
	_, adminToken := env.login(t, "admin", "Monitor123!")
	status, body = env.do(t, http.MethodPost, "/api/v1/posts", adminToken,
		`{"name":"夜班岗","code":"night","sort":1,"status":1,"password":"should-be-masked"}`)
	if status != http.StatusCreated {
		t.Fatalf("管理员新增岗位失败: status=%d body=%v", status, body)
	}
	// 越权请求也会写日志，这里取最近一条（管理员成功写入的那条）。
	var operLog model.SysOperLog
	if err := env.db.Where("router = ? AND method = ?", "/api/v1/posts", "POST").
		Order("id desc").First(&operLog).Error; err != nil {
		t.Fatalf("操作日志未落库: %v", err)
	}
	if operLog.ActionType == nil || *operLog.ActionType != "create" {
		t.Fatalf("操作日志 action_type 异常: %v", operLog.ActionType)
	}
	if operLog.App == nil || *operLog.App != "posts" {
		t.Fatalf("操作日志 module 异常: %v", operLog.App)
	}
	if operLog.RequestData != nil && bytes.Contains([]byte(*operLog.RequestData), []byte("should-be-masked")) {
		t.Fatalf("操作日志未对密码脱敏: %v", *operLog.RequestData)
	}
	var admin model.SysUser
	if err := env.db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatalf("查询管理员失败: %v", err)
	}
	if operLog.OperatorID == nil || *operLog.OperatorID != admin.ID {
		t.Fatalf("操作日志操作人应为 %d，实际 %v", admin.ID, operLog.OperatorID)
	}
}

// TestCreateUserWithFrontendPayload 用前端真实提交体验证新增用户不再报「请求参数格式错误」。
//
// 前端契约的两个坑：字典下拉（性别）提交的是字符串 "1"，而 roleIds 是数字数组 [x]。
// 二者都必须能被后端成功绑定并落库。
func TestCreateUserWithFrontendPayload(t *testing.T) {
	env := newEnv(t)
	_, adminToken := env.login(t, "admin", "Monitor123!")

	var role model.SysRole
	if err := env.db.Where("code <> ?", "super_admin").Order("id").First(&role).Error; err != nil {
		t.Fatalf("查询普通角色失败: %v", err)
	}
	var dept model.SysDept
	if err := env.db.Order("id").First(&dept).Error; err != nil {
		t.Fatalf("查询部门失败: %v", err)
	}
	payload := fmt.Sprintf(
		`{"username":"tester","nickname":"测试管理员","deptId":%q,"gender":"1","roleIds":[%d],"postIds":[1],"status":1,"mobile":"13800000000","email":"tester@example.com"}`,
		util.TextID(dept.ID), role.ID)
	status, body := env.do(t, http.MethodPost, "/api/v1/users", adminToken, payload)
	if status != http.StatusCreated {
		t.Fatalf("新增用户失败: status=%d body=%v", status, body)
	}

	var created model.SysUser
	if err := env.db.Where("username = ?", "tester").First(&created).Error; err != nil {
		t.Fatalf("新增用户未落库: %v", err)
	}
	if created.Gender != "1" {
		t.Fatalf("性别应存为字符串 1，实际 %q", created.Gender)
	}
	if created.Status != 1 {
		t.Fatalf("状态应为 1，实际 %d", created.Status)
	}
	if created.DeptID == nil || *created.DeptID != dept.ID {
		t.Fatalf("部门未正确保存: %v", created.DeptID)
	}
	var roleLinks int64
	if err := env.db.Model(&model.SysUserRole{}).Where("user_id = ?", created.ID).Count(&roleLinks).Error; err != nil {
		t.Fatalf("统计用户角色失败: %v", err)
	}
	if roleLinks != 1 {
		t.Fatalf("角色关联应写入 1 条，实际 %d", roleLinks)
	}

	// 表单回显 + 编辑提交（前端会把整份表单回传，字段类型同新增）。
	status, body = env.do(t, http.MethodGet, "/api/v1/users/"+util.TextID(created.ID)+"/form", adminToken, "")
	if status != http.StatusOK {
		t.Fatalf("查询用户表单失败: status=%d body=%v", status, body)
	}
	updatePayload := fmt.Sprintf(
		`{"username":"tester","nickname":"测试管理员2","deptId":%q,"gender":"2","roleIds":[%d],"status":0}`,
		util.TextID(dept.ID), role.ID)
	status, body = env.do(t, http.MethodPut, "/api/v1/users/"+util.TextID(created.ID), adminToken, updatePayload)
	if status != http.StatusOK {
		t.Fatalf("更新用户失败: status=%d body=%v", status, body)
	}
	var updated model.SysUser
	if err := env.db.First(&updated, "id = ?", created.ID).Error; err != nil {
		t.Fatalf("查询更新后的用户失败: %v", err)
	}
	if updated.Gender != "2" || updated.Status != 0 {
		t.Fatalf("更新未生效: gender=%q status=%d", updated.Gender, updated.Status)
	}
}

// TestMonitorRoutesWired 验证监控路由已接线：监控库不可用时返回 502/B0200（与迁移前一致）。
func TestMonitorRoutesWired(t *testing.T) {
	env := newEnv(t)
	_, token := env.login(t, "admin", "Monitor123!")

	status, body := env.do(t, http.MethodGet, "/api/v1/monitor/overview", token, "")
	if status != http.StatusBadGateway || body["code"] != "B0200" {
		t.Fatalf("监控库不可用时应返回 502/B0200，实际 status=%d body=%v", status, body)
	}

	status, body = env.do(t, http.MethodGet, "/api/v1/monitor/agents/undefined", token, "")
	if status != http.StatusBadRequest || body["code"] != "A0001" {
		t.Fatalf("非法服务器 ID 应返回 400/A0001，实际 status=%d body=%v", status, body)
	}
}

// contains 判断字符串切片是否包含目标值。
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
