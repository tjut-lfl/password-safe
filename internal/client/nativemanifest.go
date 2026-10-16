package client

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteNativeManifest 写原生消息主机清单，返回路径。
func WriteNativeManifest(dir, exe string) (string, error) {
	m := map[string]any{
		"name":            NativeHostName,
		"description":     "Password Safe desktop client",
		"path":            exe,
		"type":            "stdio",
		"allowed_origins": []string{"chrome-extension://" + ExtensionID + "/"},
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	os.MkdirAll(dir, 0o700)
	p := filepath.Join(dir, NativeHostName+".json")
	return p, os.WriteFile(p, b, 0o644)
}
