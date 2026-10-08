package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/company/monitor-webserver/internal/apperr"
)

// GuacamoleClient 是 Apache Guacamole REST API 的最小客户端，用于换取 RDP 的一次性嵌入 token。
//
// Guacamole 架构：guacd（协议代理守护进程）+ guacamole-client（Java Web 应用）。
// 我们只与 guacamole-client 的 REST API 交互：登录拿 authToken → 创建 RDP 连接 →
// 生成一次性 child token → 前端 iframe 打开 `{base}/#/client/{childToken}`。
type GuacamoleClient struct {
	// baseURL guacamole-client 地址，形如 http://host:8080/guacamole（不含末尾斜杠）。
	baseURL string
	// username 管理 API 账号。
	username string
	// password 管理 API 密码。
	password string
	// dataSource 数据源名（官方 docker 默认 mysql）。
	dataSource string
	// http HTTP 客户端。
	http *http.Client
}

// NewGuacamoleClient 创建 Guacamole 客户端。
//
// 参数 Parameters:
//   - baseURL (string): 地址。
//   - username, password (string): 管理账号。
//   - dataSource (string): 数据源名，空则用 mysql。
func NewGuacamoleClient(baseURL, username, password, dataSource string) *GuacamoleClient {
	if dataSource == "" {
		dataSource = "mysql"
	}
	return &GuacamoleClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		username:   username,
		password:   password,
		dataSource: dataSource,
		http:       &http.Client{Timeout: 15 * time.Second},
	}
}

type guacTokenResponse struct {
	AuthToken string `json:"authToken"`
	Username  string `json:"username"`
	DataScrc  string `json:"dataSource"`
}

// IssueRDPToken 为资产换取 RDP 一次性嵌入 token 的完整 iframe 地址，并记录连接日志。
//
// 参数 Parameters:
//   - s (*Service): 服务（用于解密凭据与落库）。
//   - agentID (string): 资产主键。
//   - operator (string): 操作人账号。
//   - endpoint (*EndpointView): 已校验的连接配置视图。
//
// 返回 Returns:
//   - iframeURL (string): 前端 iframe 的 src。
//   - err (error): 任一步骤失败时返回非 nil。
func (g *GuacamoleClient) IssueRDPToken(s *Service, agentID, operator string, endpoint *EndpointView) (string, error) {
	// 解密 RDP 密码。
	var row RemoteEndpoint
	if err := s.db.Where("agent_id = ?", agentID).First(&row).Error; err != nil {
		return "", err
	}
	password, err := DecryptSecret(s.key, row.SecretCipher)
	if err != nil {
		return "", err
	}

	authToken, err := g.login()
	if err != nil {
		return "", apperr.Unavailable("连接 Guacamole 服务失败", err)
	}
	connectionID, err := g.createConnection(authToken, endpoint, password)
	if err != nil {
		return "", apperr.Unavailable("创建 Guacamole 连接失败", err)
	}
	childToken, err := g.childToken(authToken, connectionID)
	if err != nil {
		return "", apperr.Unavailable("生成 Guacamole 会话失败", err)
	}

	// 记录 RDP 连接日志（Guacamole 为独立服务，会话结束由前端关闭 iframe，暂不跟踪 ended_at）。
	_ = s.db.Create(&RDPSession{
		AgentID:          agentID,
		Operator:         operator,
		Username:         endpoint.Username,
		GuacConnectionID: connectionID,
		Status:           SessionActive,
		StartedAt:        time.Now(),
	})

	return fmt.Sprintf("%s/#/client/%s", g.baseURL, childToken), nil
}

// login 用管理账号登录，返回 authToken。
func (g *GuacamoleClient) login() (string, error) {
	body, err := json.Marshal(map[string]string{"username": g.username, "password": g.password})
	if err != nil {
		return "", err
	}
	var resp guacTokenResponse
	if err := g.post("/api/tokens", body, "", &resp); err != nil {
		return "", err
	}
	if resp.AuthToken == "" {
		return "", errors.New("guacamole 未返回 authToken")
	}
	return resp.AuthToken, nil
}

// createConnection 创建一条 RDP 连接并返回连接标识。
func (g *GuacamoleClient) createConnection(authToken string, endpoint *EndpointView, password string) (string, error) {
	payload := map[string]any{
		"parentIdentifier": "ROOT",
		"name":             fmt.Sprintf("monitor-%s-%d", endpoint.AgentID, time.Now().UnixNano()),
		"protocol":         "rdp",
		"parameters": map[string]string{
			"hostname":    endpoint.Host,
			"port":        fmt.Sprintf("%d", endpoint.Port),
			"username":    endpoint.Username,
			"password":    password,
			"security":    "any",
			"ignore-cert": "true",
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	var result map[string]any
	url := fmt.Sprintf("/api/session/data/%s/connections", g.dataSource)
	if err := g.post(url, body, authToken, &result); err != nil {
		return "", err
	}
	identifier, _ := result["identifier"].(string)
	if identifier == "" {
		return "", errors.New("guacamole 未返回 connection identifier")
	}
	return identifier, nil
}

// childToken 为连接生成一次性子 token（供 iframe 客户端使用）。
func (g *GuacamoleClient) childToken(authToken, connectionID string) (string, error) {
	var resp guacTokenResponse
	url := fmt.Sprintf("/api/tokens/%s/c/%s", authToken, connectionID)
	if err := g.post(url, []byte(`{}`), authToken, &resp); err != nil {
		return "", err
	}
	if resp.AuthToken == "" {
		return "", errors.New("guacamole 未返回 child token")
	}
	return resp.AuthToken, nil
}

// post 发送 POST 请求并解析 JSON 响应。
func (g *GuacamoleClient) post(path string, body []byte, authToken string, out any) error {
	req, err := http.NewRequest(http.MethodPost, g.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Guacamole-Token", authToken)
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(buf.String()))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
