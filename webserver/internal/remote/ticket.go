package remote

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// ticketTTL 是一次性连接票据的有效期。
const ticketTTL = 30 * time.Second

// Ticket 是 WebSocket 握手使用的一次性授权票据所携带的信息。
type Ticket struct {
	// AgentID 目标资产主键。
	AgentID string
	// Operator 操作人账号。
	Operator string
	// Protocol 协议（ssh / rdp）。
	Protocol string
}

type ticketEntry struct {
	ticket    Ticket
	expiresAt time.Time
}

// TicketStore 是进程内的一次性票据仓库。
//
// 为什么需要它：浏览器发起 WebSocket 握手时无法自定义 Authorization 头，
// 所以先通过受鉴权的 REST 接口换取一次性票据，再用 `?ticket=` 参数建立 WS；
// 票据取用即焚，且 30 秒过期，避免裸 URL 被长期复用。
type TicketStore struct {
	mu      sync.Mutex
	entries map[string]ticketEntry
}

// NewTicketStore 创建票据仓库。
func NewTicketStore() *TicketStore {
	return &TicketStore{entries: make(map[string]ticketEntry)}
}

// Issue 签发一张票据。
//
// 参数 Parameters:
//   - agentID (string): 目标资产主键。
//   - operator (string): 操作人账号。
//   - protocol (string): 协议。
//
// 返回 Returns:
//   - token (string): 一次性票据（hex）。
func (s *TicketStore) Issue(agentID, operator, protocol string) string {
	token := randomToken()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	s.entries[token] = ticketEntry{
		ticket:    Ticket{AgentID: agentID, Operator: operator, Protocol: protocol},
		expiresAt: time.Now().Add(ticketTTL),
	}
	return token
}

// Consume 取用并销毁一张票据（一次性）。
//
// 参数 Parameters:
//   - token (string): 票据。
//
// 返回 Returns:
//   - ticket (Ticket): 票据信息。
//   - ok (bool): 有效返回 true。
func (s *TicketStore) Consume(token string) (Ticket, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	entry, ok := s.entries[token]
	if !ok {
		return Ticket{}, false
	}
	delete(s.entries, token)
	return entry.ticket, true
}

func (s *TicketStore) purgeLocked() {
	now := time.Now()
	for token, entry := range s.entries {
		if entry.expiresAt.Before(now) {
			delete(s.entries, token)
		}
	}
}

// randomToken 生成 32 字节随机票据的 hex 表示。
func randomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败属于极端情况；降级为时间戳兜底，保证函数总能返回。
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf)
}
