package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// NativeHostName 是在 Chrome/Edge 注册的原生消息主机名。
const NativeHostName = "com.tjut_lfl.password_safe"

// ExtensionID 由扩展 manifest.json 里的公钥 key 推导，固定不变。
const ExtensionID = "eaclmdmcljfnhljgbdlcejepnpnfahie"

// ControlInfo 写在配置目录 control.json（权限 600）：托盘实例的本地控制端口与随机令牌。
type ControlInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

func controlPath(dir string) string { return filepath.Join(dir, "control.json") }

func WriteControlInfo(dir string, ci ControlInfo) error {
	b, _ := json.Marshal(ci)
	os.MkdirAll(dir, 0o700)
	return os.WriteFile(controlPath(dir), b, 0o600)
}

func ReadControlInfo(dir string) (ControlInfo, error) {
	var ci ControlInfo
	b, err := os.ReadFile(controlPath(dir))
	if err != nil {
		return ci, err
	}
	return ci, json.Unmarshal(b, &ci)
}

// CallControl 调用正在运行的托盘实例（cmd: status | reconnect | guide | setup）。
func CallControl(dir, cmd string) (map[string]any, error) {
	ci, err := ReadControlInfo(dir)
	if err != nil {
		return nil, errors.New("client_not_running")
	}
	hc := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/ctl/%s", ci.Port, cmd), bytes.NewReader(nil))
	req.Header.Set("X-PS-Token", ci.Token)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, errors.New("client_not_running")
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := decodeJSON(resp.Body, &out); err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return out, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return out, nil
}
