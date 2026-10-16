// Package client 实现 Windows 托盘客户端：SSH 隧道、配置、原生消息、插件安装引导。
// Package client implements the desktop tray client: SSH tunnel, config, native messaging, extension guide.
package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Config 保存在 %APPDATA%\password-safe-client\config.json。不包含任何密码/口令。
type Config struct {
	Host       string `json:"server_host"`
	Port       int    `json:"server_port"`
	User       string `json:"user"`
	KeyPath    string `json:"key_path"`
	LocalPort  int    `json:"local_port"`
	RemotePort int    `json:"remote_port"`
	Autostart  bool   `json:"autostart"`
}

func DefaultConfig() Config {
	return Config{Port: 22, User: "ubuntu", LocalPort: 8787, RemotePort: 8787}
}

func (c Config) Validate() error {
	var errs []string
	if strings.TrimSpace(c.Host) == "" {
		errs = append(errs, "server_host 不能为空 / server_host is required")
	}
	if strings.TrimSpace(c.User) == "" {
		errs = append(errs, "user 不能为空 / user is required")
	}
	if strings.TrimSpace(c.KeyPath) == "" {
		errs = append(errs, "key_path 不能为空 / key_path is required")
	}
	for name, p := range map[string]int{"server_port": c.Port, "local_port": c.LocalPort, "remote_port": c.RemotePort} {
		if p < 1 || p > 65535 {
			errs = append(errs, fmt.Sprintf("%s 端口无效 / invalid port: %d", name, p))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// ConfigDir: Windows %APPDATA%\password-safe-client；其它系统 ~/.config/password-safe-client。
// 可用 PASSWORD_SAFE_CLIENT_DIR 覆盖（测试用）。
func ConfigDir() string {
	if d := os.Getenv("PASSWORD_SAFE_CLIENT_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if a := os.Getenv("APPDATA"); a != "" {
			return filepath.Join(a, "password-safe-client")
		}
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "password-safe-client")
}

// DataDir：解压插件的位置。Windows %LOCALAPPDATA%\password-safe-client。
func DataDir() string {
	if d := os.Getenv("PASSWORD_SAFE_CLIENT_DIR"); d != "" {
		return filepath.Join(d, "data")
	}
	if runtime.GOOS == "windows" {
		if a := os.Getenv("LOCALAPPDATA"); a != "" {
			return filepath.Join(a, "password-safe-client")
		}
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "password-safe-client")
	}
	return filepath.Join(ConfigDir(), "data")
}

func ConfigPath(dir string) string { return filepath.Join(dir, "config.json") }

// LoadConfig 返回 (cfg, exists, err)。
func LoadConfig(dir string) (Config, bool, error) {
	cfg := DefaultConfig()
	b, err := os.ReadFile(ConfigPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, true, fmt.Errorf("配置文件格式错误 / bad config: %w", err)
	}
	return cfg, true, nil
}

func SaveConfig(dir string, c Config) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := ConfigPath(dir) + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, ConfigPath(dir))
}
