package remote

import (
	"fmt"
	"io"
	"net"
	"path"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SftpFileInfo 是 SFTP 文件列表项的视图。
type SftpFileInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	ModTime string `json:"modTime"`
	IsDir   bool   `json:"isDir"`
}

// SftpClient 封装一次 SSH + SFTP 连接（每次 API 调用独立建连、用完即关）。
type SftpClient struct {
	client *ssh.Client
	sftp   *sftp.Client
}

// DialSFTP 使用凭据建立 SSH 连接并初始化 SFTP 子系统。
func DialSFTP(host string, port int, username, authType, secret, passphrase string) (*SftpClient, error) {
	auth, err := sshAuth(authType, secret, passphrase)
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	sc, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("sftp init: %w", err)
	}
	return &SftpClient{client: client, sftp: sc}, nil
}

// ListDir 列出远程目录内容。
func (c *SftpClient) ListDir(dir string) ([]SftpFileInfo, error) {
	entries, err := c.sftp.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	result := make([]SftpFileInfo, 0, len(entries))
	for _, e := range entries {
		result = append(result, SftpFileInfo{
			Name:    e.Name(),
			Path:    path.Join(dir, e.Name()),
			Size:    e.Size(),
			Mode:    e.Mode().String(),
			ModTime: e.ModTime().Format("2006-01-02 15:04:05"),
			IsDir:   e.IsDir(),
		})
	}
	return result, nil
}

// UploadFile 将本地数据流上传到远程路径。
func (c *SftpClient) UploadFile(remotePath string, reader io.Reader) error {
	f, err := c.sftp.Create(remotePath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, reader)
	return err
}

// DownloadFile 将远程文件写入到 writer。
func (c *SftpClient) DownloadFile(remotePath string, writer io.Writer) error {
	f, err := c.sftp.Open(remotePath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(writer, f)
	return err
}

// Close 关闭 SFTP 与 SSH 连接。
func (c *SftpClient) Close() {
	_ = c.sftp.Close()
	_ = c.client.Close()
}

// Rename 重命名远程文件或目录。
func (c *SftpClient) Rename(oldPath, newPath string) error {
	return c.sftp.Rename(oldPath, newPath)
}

// Remove 删除远程文件或空目录。
func (c *SftpClient) Remove(remotePath string) error {
	return c.sftp.Remove(remotePath)
}

// SFTPListDir 为指定资产列出远程目录（独立建连、用完即关）。
func (s *Service) SFTPListDir(agentID, dir string) ([]SftpFileInfo, error) {
	host, port, username, authType, secret, passphrase, err := s.SSHCredentials(agentID)
	if err != nil {
		return nil, err
	}
	client, err := DialSFTP(host, port, username, authType, secret, passphrase)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	return client.ListDir(dir)
}

// SFTPRename 为指定资产重命名远程文件或目录。
func (s *Service) SFTPRename(agentID, oldPath, newPath string) error {
	host, port, username, authType, secret, passphrase, err := s.SSHCredentials(agentID)
	if err != nil {
		return err
	}
	client, err := DialSFTP(host, port, username, authType, secret, passphrase)
	if err != nil {
		return err
	}
	defer client.Close()
	return client.Rename(oldPath, newPath)
}

// SFTPRemove 为指定资产删除远程文件或空目录。
func (s *Service) SFTPRemove(agentID, remotePath string) error {
	host, port, username, authType, secret, passphrase, err := s.SSHCredentials(agentID)
	if err != nil {
		return err
	}
	client, err := DialSFTP(host, port, username, authType, secret, passphrase)
	if err != nil {
		return err
	}
	defer client.Close()
	return client.Remove(remotePath)
}
