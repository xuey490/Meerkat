package remote

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// Terminal 是一次已建立的 SSH 终端会话（pty 已就绪，录像与命令捕获已开启）。
type Terminal struct {
	// client SSH 连接。
	client *ssh.Client
	// session SSH 会话。
	session *ssh.Session
	// stdin 终端输入管道。
	stdin io.WriteCloser
	// stdout 终端输出流。
	stdout io.Reader
	// recorder 会话录像器。
	recorder *Recorder
	// commands 命令流捕获器。
	commands *CommandCapture
}

// OpenTerminal 建立 SSH 连接、请求 pty 并启动 shell，同时开启录像与命令捕获。
//
// 参数 Parameters:
//   - sessionID (uint): 会话主键（命令流落库使用）。
//   - host (string): 目标地址。
//   - port (int): 端口。
//   - username (string): 登录用户名。
//   - authType (string): password / key。
//   - secret (string): 密码或私钥明文。
//   - passphrase (string): 私钥 passphrase（可空）。
//   - width, height (int): 终端尺寸。
//
// 返回 Returns:
//   - terminal (*Terminal): 就绪的终端会话。
//   - err (error): 连接或 pty 建立失败时返回非 nil。
func (s *Service) OpenTerminal(sessionID uint, host string, port int, username, authType, secret, passphrase string, width, height int) (*Terminal, error) {
	auth, err := sshAuth(authType, secret, passphrase)
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 内网/自签证书场景；如需校验主机密钥可后续增强
		Timeout:         10 * time.Second,
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}
	if err := session.RequestPty("xterm-256color", height, width, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}
	if err := session.Shell(); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}

	recorder, err := NewRecorder(s.RecordingPath(sessionID), width, height)
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}
	commands := NewCommandCapture(func(seq int, command string) {
		_ = s.db.Create(&SSHCommand{SessionID: sessionID, Seq: seq, Command: command, RecordedAt: time.Now()})
	})

	return &Terminal{
		client:   client,
		session:  session,
		stdin:    stdin,
		stdout:   stdout,
		recorder: recorder,
		commands: commands,
	}, nil
}

// Output 返回终端输出流（供转发到 WebSocket）。
func (t *Terminal) Output() io.Reader { return t.stdout }

// RecordOutput 把一段终端输出写入录像（供 WebSocket 转发处旁路调用）。
func (t *Terminal) RecordOutput(data []byte) { _ = t.recorder.Output(data) }

// WriteInput 把客户端输入写入 stdin，并旁路记录录像与命令流。
func (t *Terminal) WriteInput(data []byte) error {
	_, err := t.stdin.Write(data)
	if err == nil {
		_ = t.recorder.Input(data)
		t.commands.Write(data)
	}
	return err
}

// CancelPendingInput 取消远端 shell 当前已输入但未回车执行的命令行。
func (t *Terminal) CancelPendingInput() error {
	// xterm 按键是逐字节转发的：高危命令命中时，危险命令文本可能已经进入远端 pty 行缓冲。
	// 这里发送 Ctrl+C 取消远端当前输入行，再拦截回车，避免 shell 后续把残留命令执行掉。
	_, err := t.stdin.Write([]byte{0x03})
	if err == nil {
		_ = t.recorder.Input([]byte{0x03})
		t.commands.Write([]byte{0x03})
	}
	return err
}

// Resize 调整终端尺寸。
func (t *Terminal) Resize(width, height int) error {
	return t.session.WindowChange(height, width)
}

// Close 结束会话：冲刷命令缓冲、关闭录像与 SSH 连接。
func (t *Terminal) Close() {
	t.commands.Flush()
	_ = t.recorder.Close()
	_ = t.session.Close()
	_ = t.client.Close()
}

// sshAuth 根据认证方式构造 SSH 认证方法。
//
// key 方式解析 PEM / OpenSSH 两种私钥格式；password 方式直接使用密码。
func sshAuth(authType, secret, passphrase string) ([]ssh.AuthMethod, error) {
	if secret == "" {
		return nil, errors.New("缺少密码或私钥")
	}
	if authType == AuthKey {
		var signer ssh.Signer
		var err error
		if passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(secret), []byte(passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(secret))
		}
		if err != nil {
			return nil, fmt.Errorf("解析 SSH 私钥失败: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	return []ssh.AuthMethod{ssh.Password(secret)}, nil
}

// CreateSession 写入一条 SSH 会话记录（初始为 active）。
func (s *Service) CreateSession(row *SSHSession) error {
	return s.db.Create(row).Error
}

// SetSessionRecording 回写会话的录像文件相对路径。
func (s *Service) SetSessionRecording(id uint, relative string) error {
	return s.db.Model(&SSHSession{}).Where("id = ?", id).Update("recording_path", relative).Error
}

// FinishSession 结束会话：写结束时间、状态、断开原因与流量统计。
func (s *Service) FinishSession(id uint, status, reason string, bytesIn, bytesOut int64) error {
	now := time.Now()
	return s.db.Model(&SSHSession{}).Where("id = ?", id).Updates(map[string]any{
		"status":       status,
		"close_reason": reason,
		"bytes_in":     bytesIn,
		"bytes_out":    bytesOut,
		"ended_at":     now,
	}).Error
}
