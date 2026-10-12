package remote

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Guacamole 协议说明
//
// 这是一套「长度前缀 + 逗号分隔 + 分号结尾」的文本指令流：
//
//	6.select,3.rdp;            一条指令：opcode=select，参数=[rdp]
//	5.error,20.由于某原因被断开;  参数里的数字是「该字段的字节数」，不是字符数
//
// 我们的代理只在【握手期】解析指令，一旦网关返回 ready 就退化为纯字节中继：
// RDP 画面由海量 blob/img 指令组成，全程解析会给每一帧带来无谓的分配与拷贝。

// 握手期会用到的指令名。
const (
	// opSelect 客户端选择协议（握手第一条）。
	opSelect = "select"
	// opSize 客户端上报显示尺寸。
	opSize = "size"
	// opAudio 客户端上报音频能力。
	opAudio = "audio"
	// opImage 客户端上报图片编码能力。
	opImage = "image"
	// opVideo 客户端上报视频编码能力。guacd 握手期认这个 opcode，没有能力就发空参数。
	opVideo = "video"
	// opTimezone 客户端时区。guacd 1.1 起在握手期处理，不认识的版本会忽略。
	opTimezone = "timezone"
	// opConnect 发起连接。参数不是「名,值」对，而是协议版本加上与 args 同序的值。
	opConnect = "connect"
	// opNop 保活。guacd 会忽略，浏览器也会定期发。
	opNop = "nop"
	// opArgs 网关回传可用参数集合。
	opArgs = "args"
	// opReady 网关就绪：收到它即代表握手成功，可以开始中继。
	opReady = "ready"
	// opError 网关报错：握手失败，参数里是人类可读的原因。
	opError = "error"
)

const (
	// maxFieldLength 单个字段的最大字节数。
	//
	// 画面帧以 base64 放在 blob/img 指令里，正常几十 KB；给 32 MiB 足够容纳极端
	// 分辨率的关键帧，同时挡住畸形输入导致的内存膨胀。
	maxFieldLength = 32 << 20
	// maxLengthDigits 长度前缀的位数上限（32 MiB 是 8 位十进制数）。
	maxLengthDigits = 9
	// maxArgsPerInstruction 单条指令的参数个数上限（防御畸形输入）。
	maxArgsPerInstruction = 4096
	// maxInstructionLength 单条指令的累计字节数上限。
	//
	// 单字段已有 32 MiB 上限，但一条指令可以带几千个字段——只看单字段，畸形输入
	// 仍能撑出很大的内存（4096 × 32 MiB）。加这道累计上限才真正封住。
	maxInstructionLength = 64 << 20
)

// writeInstruction 按 Guacamole 协议写出一条指令。
//
// 每个字段（含 opcode）都写成「十进制长度 + '.' + 内容」，字段之间用 ',' 分隔，
// 整条以 ';' 结尾。长度取字节数（UTF-8），与协议规定一致。
//
// 参数 Parameters:
//   - w (io.Writer): 目标写入端。
//   - opcode (string): 指令名。
//   - args (...string): 指令参数。
//
// 返回 Returns:
//   - err (error): 写入失败时返回非 nil。
func writeInstruction(w io.Writer, opcode string, args ...string) error {
	if err := writeField(w, opcode); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := io.WriteString(w, ","); err != nil {
			return err
		}
		if err := writeField(w, arg); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, ";")
	return err
}

// writeField 写一个带长度前缀的字段。
func writeField(w io.Writer, value string) error {
	if _, err := fmt.Fprintf(w, "%d.", len(value)); err != nil {
		return err
	}
	_, err := io.WriteString(w, value)
	return err
}

// readInstruction 读出一条完整的 Guacamole 指令。
//
// 对长度前缀设了双重上限（位数与数值），避免畸形或恶意输入触发超大内存分配。
//
// 参数 Parameters:
//   - r (*bufio.Reader): 指令流读取端。
//
// 返回 Returns:
//   - opcode (string): 指令名。
//   - args ([]string): 指令参数。
//   - err (error): 读到流末尾或格式非法时返回非 nil。
func readInstruction(r *bufio.Reader) (string, []string, error) {
	fields := make([]string, 0, 8)
	totalLength := 0
	for {
		length, err := readLengthPrefix(r)
		if err != nil {
			return "", nil, err
		}
		totalLength += length
		if totalLength > maxInstructionLength {
			return "", nil, fmt.Errorf("guacamole 指令总长超出上限 %d", maxInstructionLength)
		}
		value := make([]byte, length)
		if _, err := io.ReadFull(r, value); err != nil {
			return "", nil, err
		}
		fields = append(fields, string(value))
		if len(fields) > maxArgsPerInstruction {
			return "", nil, errors.New("guacamole 指令参数过多")
		}

		separator, err := r.ReadByte()
		if err != nil {
			return "", nil, err
		}
		switch separator {
		case ';':
			return fields[0], fields[1:], nil
		case ',':
			// 继续读下一个字段。
		default:
			return "", nil, fmt.Errorf("guacamole 指令分隔符非法: %q", separator)
		}
	}
}

// readLengthPrefix 读出一个长度前缀（到 '.' 为止的十进制数）。
func readLengthPrefix(r *bufio.Reader) (int, error) {
	length := 0
	digits := 0
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		if b == '.' {
			if digits == 0 {
				return 0, errors.New("guacamole 长度前缀为空")
			}
			return length, nil
		}
		if b < '0' || b > '9' {
			return 0, fmt.Errorf("guacamole 长度前缀含非法字符: %q", b)
		}
		digits++
		if digits > maxLengthDigits {
			return 0, errors.New("guacamole 长度前缀过长")
		}
		length = length*10 + int(b-'0')
		if length > maxFieldLength {
			return 0, fmt.Errorf("guacamole 字段长度 %d 超出上限 %d", length, maxFieldLength)
		}
	}
}

// connectArgsFromNames 按 guacd 在 args 指令里给出的名字，生成等长的 connect 值列表。
//
// 必须与 args 的参数个数完全一致（含第一条协议版本）。少一个或多一个，
// guacd 都会在回完 ready 之后报 "expected number of arguments" 并断开，
// 浏览器拿不到画面，只会一直转圈。
//
// 第一条若是 VERSION_*，原样回写；其余按名字从 authority / inject 取值，没有就空串。
func connectArgsFromNames(argNames []string, authority, inject map[string]string) []string {
	values := make([]string, len(argNames))
	for i, name := range argNames {
		if i == 0 && strings.HasPrefix(name, "VERSION_") {
			values[i] = name
			continue
		}
		if value, ok := authority[name]; ok {
			values[i] = value
			continue
		}
		if value, ok := inject[name]; ok {
			values[i] = value
		}
	}
	return values
}
