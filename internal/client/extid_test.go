package client

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// 扩展 ID = sha256(公钥 DER) 前 16 字节，十六进制 0-f 映射到 a-p。
func TestExtensionIDMatchesManifestKey(t *testing.T) {
	b, err := os.ReadFile("../../extension/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Key string `json:"key"`
	}
	json.Unmarshal(b, &m)
	der, err := base64.StdEncoding.DecodeString(m.Key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	h := []byte(hex.EncodeToString(sum[:16]))
	for i, c := range h {
		if c <= '9' {
			h[i] = 'a' + (c - '0')
		} else {
			h[i] = 'a' + 10 + (c - 'a')
		}
	}
	if string(h) != ExtensionID {
		t.Fatalf("manifest key → %s, const %s", h, ExtensionID)
	}
}
