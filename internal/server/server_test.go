package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/tjut-lfl/password-safe/internal/vault"
)

func TestExportAndBackupAPI(t *testing.T) {
	v := vault.New(filepath.Join(t.TempDir(), "vault.dat"))
	if err := v.Init([]byte("master-pass-1234")); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Add("site", "alice", "s3cret-value", "https://e.org", "", nil, ""); err != nil {
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
	r = post("/api/v1/backup", u.Token, map[string]string{})
	if r.StatusCode != 200 {
		t.Fatalf("backup: %d", r.StatusCode)
	}
	if list, _ := v.ListBackups(); len(list) == 0 {
		t.Fatal("no backup created")
	}
}
