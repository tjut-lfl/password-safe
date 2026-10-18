package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tjut-lfl/password-safe/internal/vault"
)

func TestFormatTTL(t *testing.T) {
	if FormatTTL(6*time.Hour) != "6h" {
		t.Fatalf("got %q", FormatTTL(6*time.Hour))
	}
	if FormatTTL(30*time.Minute) != "30m" {
		t.Fatalf("got %q", FormatTTL(30*time.Minute))
	}
}

func TestDefaultSessionTTL(t *testing.T) {
	t.Setenv("PASSWORD_SAFE_SESSION_TTL", "")
	_ = os.Unsetenv("PASSWORD_SAFE_SESSION_TTL")
	if DefaultSessionTTL() != 6*time.Hour {
		t.Fatalf("default want 6h, got %v", DefaultSessionTTL())
	}
	t.Setenv("PASSWORD_SAFE_SESSION_TTL", "2h")
	if DefaultSessionTTL() != 2*time.Hour {
		t.Fatalf("env want 2h, got %v", DefaultSessionTTL())
	}
	t.Setenv("PASSWORD_SAFE_SESSION_TTL", "bogus")
	if DefaultSessionTTL() != 6*time.Hour {
		t.Fatalf("bogus want fallback 6h")
	}
}

func TestUnlockExpiresIn(t *testing.T) {
	v := vault.New(filepath.Join(t.TempDir(), "vault.dat"))
	if err := v.Init([]byte("master-pass-1234")); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	srv := New(v, "127.0.0.1:0")
	srv.SetTTL(6 * time.Hour)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	b, _ := json.Marshal(map[string]string{"password": "master-pass-1234"})
	res, err := http.Post(ts.URL+"/api/v1/unlock", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var u struct {
		Token        string `json:"token"`
		ExpiresIn    string `json:"expires_in"`
		ExpiresInSec int    `json:"expires_in_sec"`
	}
	_ = json.NewDecoder(res.Body).Decode(&u)
	if u.Token == "" || u.ExpiresIn != "6h" || u.ExpiresInSec != 6*3600 {
		t.Fatalf("unlock resp: %+v", u)
	}
}

func TestExportAndBackupAPI(t *testing.T) {
	v := vault.New(filepath.Join(t.TempDir(), "vault.dat"))
	if err := v.Init([]byte("master-pass-1234")); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Add("site", "alice", "s3cret-value", "https://e.org", "note", nil, ""); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	ts := httptest.NewServer(New(v, "127.0.0.1:0").Handler())
	defer ts.Close()

	post := func(path, tok string, body any) *http.Response {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", ts.URL+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if r := post("/api/v1/export", "", map[string]string{"passphrase": "pppppppp"}); r.StatusCode != 401 {
		t.Fatalf("export without token: %d", r.StatusCode)
	}
	r := post("/api/v1/unlock", "", map[string]string{"password": "master-pass-1234"})
	var u struct{ Token string }
	_ = json.NewDecoder(r.Body).Decode(&u)
	if u.Token == "" {
		t.Fatal("no token")
	}
	if r := post("/api/v1/export", u.Token, map[string]string{"passphrase": "short"}); r.StatusCode != 400 {
		t.Fatalf("short passphrase: %d", r.StatusCode)
	}
	r = post("/api/v1/export", u.Token, map[string]string{"passphrase": "bundle-pass-1"})
	if r.StatusCode != 200 {
		t.Fatalf("export: %d", r.StatusCode)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r.Body)
	if bytes.Contains(buf.Bytes(), []byte("s3cret-value")) {
		t.Fatal("API export leaked plaintext")
	}
	es, err := vault.DecodeBundle(buf.Bytes(), []byte("bundle-pass-1"))
	if err != nil || len(es) != 1 || es[0].Password != "s3cret-value" {
		t.Fatalf("decode: %v %+v", err, es)
	}

	// CSV without confirm → 400
	if r := post("/api/v1/export", u.Token, map[string]string{"format": "csv"}); r.StatusCode != 400 {
		t.Fatalf("csv without confirm: %d", r.StatusCode)
	}
	r = post("/api/v1/export", u.Token, map[string]string{"format": "csv", "confirm": "EXPORT"})
	if r.StatusCode != 200 {
		t.Fatalf("csv export: %d", r.StatusCode)
	}
	ct := r.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/csv") {
		t.Fatalf("content-type: %s", ct)
	}
	var csvBuf bytes.Buffer
	_, _ = csvBuf.ReadFrom(r.Body)
	csv := csvBuf.String()
	if !strings.HasPrefix(csv, "name,url,username,password,note\n") {
		t.Fatalf("csv header: %q", csv[:min(60, len(csv))])
	}
	if !strings.Contains(csv, "s3cret-value") || !strings.Contains(csv, "alice") {
		t.Fatalf("csv missing fields: %q", csv)
	}

	r = post("/api/v1/backup", u.Token, map[string]string{})
	if r.StatusCode != 200 {
		t.Fatalf("backup: %d", r.StatusCode)
	}
	if list, _ := v.ListBackups(); len(list) == 0 {
		t.Fatal("no backup created")
	}
}
