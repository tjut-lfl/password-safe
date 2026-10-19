package client

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestConfigRoundTrip(t *testing.T) {
	d := t.TempDir()
	c, ok, err := LoadConfig(d)
	if err != nil || ok || c.Port != 22 || c.LocalPort != 8787 {
		t.Fatalf("defaults: %+v %v %v", c, ok, err)
	}
	if c.Validate() == nil {
		t.Fatal("empty config should not validate")
	}
	c.Host, c.KeyPath = "203.0.113.10", "/k.pem"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(d, c); err != nil {
		t.Fatal(err)
	}
	c2, ok, err := LoadConfig(d)
	if err != nil || !ok || c2 != c {
		t.Fatalf("roundtrip %+v %+v", c2, c)
	}
	c.LocalPort = 70000
	if c.Validate() == nil {
		t.Fatal("bad port accepted")
	}
}


func TestEndpoints(t *testing.T) {
	c := Config{Host: "10.0.0.1", Port: 22}
	eps := c.endpoints()
	if len(eps) != 1 || eps[0].Host != "10.0.0.1" || eps[0].ViaFallback {
		t.Fatalf("primary only: %+v", eps)
	}
	c.FallbackHost = "203.0.113.10"
	eps = c.endpoints()
	if len(eps) != 2 || !eps[1].ViaFallback || eps[1].Port != 22 || eps[1].Host != "203.0.113.10" {
		t.Fatalf("with fallback: %+v", eps)
	}
	c.FallbackPort = 2222
	eps = c.endpoints()
	if eps[1].Port != 2222 {
		t.Fatalf("custom fallback port: %+v", eps)
	}
	// same as primary → no second endpoint
	c.FallbackHost, c.FallbackPort = "10.0.0.1", 22
	if n := len(c.endpoints()); n != 1 {
		t.Fatalf("duplicate should collapse, got %d", n)
	}
	c.FallbackHost = "  "
	if n := len(c.endpoints()); n != 1 {
		t.Fatalf("blank fallback: %d", n)
	}
}

func TestConfigFallbackRoundTrip(t *testing.T) {
	d := t.TempDir()
	c := DefaultConfig()
	c.Host, c.KeyPath = "10.0.0.1", "/k.pem"
	c.FallbackHost, c.FallbackPort = "203.0.113.10", 22
	if err := SaveConfig(d, c); err != nil {
		t.Fatal(err)
	}
	c2, ok, err := LoadConfig(d)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if c2.FallbackHost != "203.0.113.10" || c2.FallbackPort != 22 {
		t.Fatalf("%+v", c2)
	}
	if err := c2.Validate(); err != nil {
		t.Fatal(err)
	}
	c2.FallbackPort = 70000
	if c2.Validate() == nil {
		t.Fatal("bad fallback port accepted")
	}
}

func TestBackoff(t *testing.T) {
	b := Backoff{Min: time.Second, Max: 8 * time.Second}
	want := []time.Duration{1, 2, 4, 8, 8}
	for i, w := range want {
		if g := b.Next(); g != w*time.Second {
			t.Fatalf("step %d: %v", i, g)
		}
	}
	b.Reset()
	if b.Next() != time.Second {
		t.Fatal("reset")
	}
}

func newKey(t *testing.T) ssh.PublicKey {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestKnownHostsTOFU(t *testing.T) {
	kh := &KnownHosts{Path: t.TempDir() + "/known_hosts"}
	k1, k2 := newKey(t), newKey(t)
	if err := kh.Check("h:22", k1); err != nil {
		t.Fatal("first use should trust", err)
	}
	if err := kh.Check("h:22", k1); err != nil {
		t.Fatal("same key", err)
	}
	var mm *HostKeyMismatchError
	if err := kh.Check("h:22", k2); !errors.As(err, &mm) {
		t.Fatalf("mismatch must be refused, got %v", err)
	}
	if err := kh.Check("other:22", k2); err != nil {
		t.Fatal("other host independent", err)
	}
}

func TestNativeFraming(t *testing.T) {
	var b bytes.Buffer
	if err := WriteNativeMessage(&b, map[string]string{"cmd": "status"}); err != nil {
		t.Fatal(err)
	}
	raw := b.Bytes()
	if n := binary.LittleEndian.Uint32(raw[:4]); int(n) != len(raw)-4 {
		t.Fatalf("length prefix %d vs %d", n, len(raw)-4)
	}
	var v nativeReq
	if err := ReadNativeMessage(&b, &v); err != nil || v.Cmd != "status" {
		t.Fatal(v, err)
	}
	big := []byte{0xff, 0xff, 0xff, 0x7f}
	if ReadNativeMessage(bytes.NewReader(big), &v) == nil {
		t.Fatal("oversized accepted")
	}
}

func TestNativeHostNoClient(t *testing.T) {
	d := t.TempDir()
	var in, out bytes.Buffer
	WriteNativeMessage(&in, map[string]string{"cmd": "status"})
	WriteNativeMessage(&in, map[string]string{"cmd": "rm -rf"})
	if err := RunNativeHost(d, &in, &out); err != nil {
		t.Fatal(err)
	}
	var r1, r2 map[string]any
	ReadNativeMessage(&out, &r1)
	ReadNativeMessage(&out, &r2)
	if r1["ok"] != false || r1["error"] != "client_not_running" || r2["error"] != "unknown_cmd" {
		t.Fatal(r1, r2)
	}
	if !IsNativeHostInvocation([]string{"chrome-extension://" + ExtensionID + "/", "--parent-window=0"}) || IsNativeHostInvocation([]string{"--install"}) {
		t.Fatal("detect")
	}
}
