// Package captcha 提供进程内的登录验证码仓储与位图渲染。
//
// 验证码只保存在本进程内存中，重启即失效，因此多实例部署时需在负载均衡上
// 开启会话保持，或改为集中式存储（Redis）。仓储所有方法并发安全。
package captcha

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// 验证码相关错误。均为哨兵错误，调用方用 errors.Is 判定并转换成对应响应码。
var (
	// ErrRequired 表示请求未携带 captchaId / captchaCode。
	ErrRequired = errors.New("请输入验证码")
	// ErrInvalid 表示 captchaId 不存在或填写的验证码不正确。
	ErrInvalid = errors.New("验证码不正确")
	// ErrExpired 表示验证码已超过有效期，需要重新获取。
	ErrExpired = errors.New("验证码已过期，请点击图片刷新")
)

// charset 是验证码可用字符集：已剔除 0/O、1/I/L 等字形易混淆的字符。
const charset = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// item 是一条尚未使用的验证码记录。
type item struct {
	// Text 期望用户输入的文本，统一为大写。
	Text string
	// ExpiresAt 过期时刻。
	ExpiresAt time.Time
}

// Store 是进程内的验证码仓储，负责生成、一次性校验与过期清理。
type Store struct {
	// mu 保护 items。
	mu sync.Mutex
	// items 验证码 ID 到记录的映射。
	items map[string]item
	// ttl 单条验证码的有效期。
	ttl time.Duration
	// maxItems 同时存在的验证码上限，防止被刷爆内存。
	maxItems int
	// length 验证码字符数。
	length int
	// stop 关闭清理协程的信号。
	stop chan struct{}
	// stopped 清理协程已退出的信号。
	stopped chan struct{}
}

// New 创建验证码仓储并启动过期清理协程。
//
// 参数 Parameters:
//   - length (int): 验证码字符数，<= 0 时按默认值 4 处理。
//   - ttl (time.Duration): 单条验证码有效期，<= 0 时按默认值 3 分钟处理。
//   - maxItems (int): 同时存在的验证码上限，<= 0 时按默认值 1000 处理。
//
// 返回 Returns:
//   - store (*Store): 已启动清理协程的仓储；不再使用时应调用 Close。
//
// 注意 Side Effects：会启动一个后台 goroutine，必须配对调用 Close。
func New(length int, ttl time.Duration, maxItems int) *Store {
	if length <= 0 {
		length = 4
	}
	if ttl <= 0 {
		ttl = 3 * time.Minute
	}
	if maxItems <= 0 {
		maxItems = 1000
	}
	s := &Store{
		items:    make(map[string]item, 64),
		ttl:      ttl,
		maxItems: maxItems,
		length:   length,
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
	go s.janitor()
	return s
}

// Close 停止过期清理协程并等待其退出（幂等）。
func (s *Store) Close() {
	select {
	case <-s.stop:
		<-s.stopped
		return
	default:
	}
	close(s.stop)
	<-s.stopped
}

// Entry 是一条验证码缓存摘要（仅供缓存管理页展示与清理）。
type Entry struct {
	// ID 验证码 ID。
	ID string
	// Text 期望用户输入的文本。
	Text string
	// ExpiresAt 过期时刻。
	ExpiresAt time.Time
}

// Snapshot 返回当前未过期的验证码快照。
//
// 返回 Returns:
//   - entries ([]Entry): 验证码摘要。
func (s *Store) Snapshot() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	entries := make([]Entry, 0, len(s.items))
	for id, record := range s.items {
		if now.After(record.ExpiresAt) {
			continue
		}
		entries = append(entries, Entry{ID: id, Text: record.Text, ExpiresAt: record.ExpiresAt})
	}
	return entries
}

// Delete 按 ID 删除验证码（缓存管理清理用），返回实际删除条数。
//
// 参数 Parameters:
//   - ids ([]string): 验证码 ID 列表。
//
// 返回 Returns:
//   - deleted (int): 实际删除条数。
func (s *Store) Delete(ids []string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := 0
	for _, id := range ids {
		if _, exists := s.items[id]; exists {
			delete(s.items, id)
			deleted++
		}
	}
	return deleted
}

// janitor 每分钟清理一次过期验证码，直到 Close 被调用。
func (s *Store) janitor() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	defer close(s.stopped)
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.purge(time.Now())
		}
	}
}

// Generate 生成一条新验证码，返回其 ID 与待展示的文本。
//
// 返回 Returns:
//   - id (string): 验证码 ID，登录时需随表单回传。
//   - text (string): 大写验证码文本，调用方需据此渲染图片。
//
// 注意 Side Effects：会在仓储中写入一条记录；超出 maxItems 时先清理过期项，
// 仍超限则淘汰部分记录，保证内存占用可控。
func (s *Store) Generate() (id, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) >= s.maxItems {
		s.purgeLocked(time.Now())
	}
	if len(s.items) >= s.maxItems {
		for key := range s.items {
			delete(s.items, key)
			if len(s.items) < s.maxItems {
				break
			}
		}
	}
	id = uuid.NewString()
	text = randomText(s.length)
	s.items[id] = item{Text: text, ExpiresAt: time.Now().Add(s.ttl)}
	return id, text
}

// Verify 校验用户填写的验证码。
//
// 校验是**一次性**的：无论成功或失败，该 ID 都会被立即删除，防止同一个验证码被反复尝试。
//
// 参数 Parameters:
//   - id (string): 获取验证码时返回的 ID；空串返回 ErrRequired。
//   - code (string): 用户填写的验证码，大小写不敏感。
//
// 返回 Returns:
//   - err (error): 校验通过返回 nil；否则返回 ErrRequired / ErrInvalid / ErrExpired。
func (s *Store) Verify(id, code string) error {
	if id == "" || code == "" {
		return ErrRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.items[id]
	if !ok {
		return ErrInvalid
	}
	// 一次性使用：先删除再比较，杜绝并发重试。
	delete(s.items, id)
	if !record.ExpiresAt.After(time.Now()) {
		return ErrExpired
	}
	if !strings.EqualFold(record.Text, strings.TrimSpace(code)) {
		return ErrInvalid
	}
	return nil
}

// purge 删除所有已过期的验证码。
//
// 参数 Parameters:
//   - now (time.Time): 当前时刻，便于测试注入固定时间。
//
// 返回 Returns:
//   - removed (int): 被删除的过期记录数。
func (s *Store) purge(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.purgeLocked(now)
}

// purgeLocked 执行实际的过期清理。调用前必须已持有 s.mu。
func (s *Store) purgeLocked(now time.Time) int {
	removed := 0
	for id, record := range s.items {
		if !record.ExpiresAt.After(now) {
			delete(s.items, id)
			removed++
		}
	}
	return removed
}

// randomText 生成指定长度的随机验证码文本（字符集见 charset）。
//
// 参数 Parameters:
//   - length (int): 字符数；<= 0 时返回空串。
//
// 返回 Returns:
//   - text (string): 大写随机文本。
func randomText(length int) string {
	if length <= 0 {
		return ""
	}
	var builder strings.Builder
	builder.Grow(length)
	for i := 0; i < length; i++ {
		builder.WriteByte(charset[randInt(len(charset))])
	}
	return builder.String()
}

// randInt 返回 [0, max) 区间内的加密安全随机整数。
//
// 参数 Parameters:
//   - max (int): 上界（不含）；<= 1 时直接返回 0。
//
// 返回 Returns:
//   - value (int): 落在 [0, max) 的随机整数；读取随机源失败时返回 0。
func randInt(max int) int {
	if max <= 1 {
		return 0
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0
	}
	return int(value.Int64())
}
