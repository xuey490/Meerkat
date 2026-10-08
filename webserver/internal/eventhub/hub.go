// Package eventhub 提供进程内的 SSE 广播中心：订阅者各自持有一个带缓冲的通道，
// 发布者把消息投递到所有订阅者，通道写满时丢弃该条消息而不是阻塞发布方。
//
// 该实现供监控状态推送（monitor-agent-status）与字典缓存失效（dict）共用，
// 与 gin 解耦，便于 service 层直接调用。
package eventhub

import (
	"encoding/json"
	"fmt"
	"sync"
)

// 主题名：与前端 src/enums/sse.ts 的 SseTopics 保持一致。
const (
	// TopicMonitorAgentStatus 监控状态推送主题。
	TopicMonitorAgentStatus = "monitor-agent-status"
	// TopicDict 字典变更主题。
	TopicDict = "dict"
)

// Hub 是 SSE 广播中心。
type Hub struct {
	// mu 保护 clients。
	mu sync.Mutex
	// clients 当前订阅者通道集合。
	clients map[chan []byte]struct{}
}

// New 创建广播中心。
//
// 返回 Returns:
//   - hub (*Hub): 空的广播中心。
func New() *Hub {
	return &Hub{clients: make(map[chan []byte]struct{})}
}

// Publish 向所有订阅者广播一条 SSE 消息（event + data 两行）。
//
// 参数 Parameters:
//   - topic (string): 事件名，前端通过 addEventListener 订阅。
//   - data (any): 事件负载，序列化为 JSON。
func (h *Hub) Publish(topic string, data any) {
	payloadData, err := json.Marshal(data)
	if err != nil {
		return
	}
	payload := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", topic, payloadData))
	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		select {
		case client <- payload:
		default:
			// 订阅者消费过慢时丢弃本条消息，避免阻塞发布方。
		}
	}
}

// Subscribe 注册一个订阅者并返回其通道。
//
// 返回 Returns:
//   - channel (chan []byte): 接收 SSE 消息的带缓冲通道。
func (h *Hub) Subscribe() chan []byte {
	ch := make(chan []byte, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

// Unsubscribe 移除订阅者并关闭其通道。
//
// 参数 Parameters:
//   - ch (chan []byte): Subscribe 返回的通道。
func (h *Hub) Unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	close(ch)
	h.mu.Unlock()
}

// Subscribers 返回当前订阅者数量，用于诊断与测试。
//
// 返回 Returns:
//   - count (int): 订阅者数量。
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}
