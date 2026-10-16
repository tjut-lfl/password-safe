package client

import (
	"os"
	"strings"
)

// Lang: "zh" 或 "en"。PASSWORD_SAFE_LANG 可覆盖。
func Lang() string {
	if l := os.Getenv("PASSWORD_SAFE_LANG"); l != "" {
		return norm(l)
	}
	if l := platformLang(); l != "" {
		return norm(l)
	}
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			return norm(v)
		}
	}
	return "zh"
}

func norm(l string) string {
	if strings.HasPrefix(strings.ToLower(l), "zh") {
		return "zh"
	}
	return "en"
}

var dict = map[string][2]string{
	"status":       {"状态", "Status"},
	"reconnect":    {"重新连接", "Reconnect"},
	"guide":        {"打开插件安装引导", "Extension install guide"},
	"setup":        {"连接设置", "Connection settings"},
	"backup":       {"立即服务端备份", "Back up server now"},
	"config":       {"打开配置", "Open config"},
	"logs":         {"打开日志", "Open log"},
	"autostart":    {"开机自启", "Start at login"},
	"quit":         {"退出", "Quit"},
	"connected":    {"已连接", "Connected"},
	"connecting":   {"连接中…", "Connecting…"},
	"disconnected": {"未连接", "Disconnected"},
	"api_down":     {"隧道已连，服务端未响应", "Tunnel up, server not responding"},
	"locked":       {"保险箱已锁定", "vault locked"},
	"unlocked":     {"保险箱已解锁", "vault unlocked"},
	"backup_tip":   {"服务端备份需要登录令牌：请在浏览器插件中解锁后点「服务端备份」", "Server backup needs a session token: unlock in the extension and click “Server backup”"},
	"backup_lock":  {"保险箱已锁定：请先在插件中解锁，再点插件里的「服务端备份」", "Vault is locked: unlock in the extension, then click “Server backup” there"},
	"not_conn":     {"隧道未连接，无法备份", "Tunnel not connected"},
}

func T(key string) string {
	v, ok := dict[key]
	if !ok {
		return key
	}
	if Lang() == "zh" {
		return v[0]
	}
	return v[1]
}
