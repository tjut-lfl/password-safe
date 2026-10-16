package vault

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tjut-lfl/password-safe/internal/crypto"
)

var testMaster = []byte("master-pass-1234")

func sampleEntries() []Entry {
	t0 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return []Entry{
		{ID: "a1", Name: "work/github", Username: "alice", Password: "p,@\"ss", URL: "https://github.com/login", Notes: "line1\nline2", Tags: []string{"dev", "work"}, TOTPSecret: "JBSWY3DPEHPK3PXP", CreatedAt: t0, UpdatedAt: t0},
		{ID: "b2", Name: "mail", Username: "bob", Password: "汉字密码", URL: "https://mail.example.org", CreatedAt: t0, UpdatedAt: t0},
	}
}

func newTestVault(t *testing.T) *Vault {
	t.Helper()
	v := New(filepath.Join(t.TempDir(), "vault.dat"))
	v.AppVersion = "v0.2.0-test"
	if err := v.Init(testMaster); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ImportEntries(sampleEntries(), false); err != nil {
		t.Fatal(err)
	}
	return v
}

func writeV1Fixture(t *testing.T, path string, entries []Entry) {
	t.Helper()
	v := New(path)
	salt, _ := crypto.GenerateSalt()
	p := crypto.Params{Time: 1, Memory: 8 * 1024, Threads: 1}
	key := crypto.DeriveKey(testMaster, salt, p)
	if err := v.writeEncryptedV1(key, salt, p, plainData{Version: 1, Entries: entries}); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateV1Fixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.dat")
	writeV1Fixture(t, path, sampleEntries())
	v := New(path)
	v.AppVersion = "v0.2.0-test"
	if err := v.Unlock(testMaster); err != nil {
		t.Fatal(err)
	}
	if v.FormatVersion() != 1 {
		t.Fatalf("want v1, got %d", v.FormatVersion())
	}
	if err := v.MigrateToV2(testMaster); err != nil {
		t.Fatal(err)
	}
	bs, _ := v.ListBackups()
	if len(bs) != 1 || bs[0].Kind != BackupPreMigrate {
		t.Fatalf("expected pre-migrate backup, got %+v", bs)
	}
	if n, ver, err := VerifyFile(bs[0].Path, testMaster); err != nil || ver != 1 || n != 2 {
		t.Fatalf("backup should be the v1 original: n=%d ver=%d err=%v", n, ver, err)
	}
	v.Lock()
	v2 := New(path)
	if err := v2.Unlock(testMaster); err != nil {
		t.Fatal(err)
	}
	if v2.FormatVersion() != FormatVersion {
		t.Fatalf("format=%d", v2.FormatVersion())
	}
	assertSame(t, sampleEntries(), mustEntries(t, v2))
}

func mustEntries(t *testing.T, v *Vault) []Entry {
	t.Helper()
	e, err := v.Entries()
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func assertSame(t *testing.T, want, got []Entry) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("len want %d got %d", len(want), len(got))
	}
	for i := range want {
		w, g := want[i], got[i]
		if w.Name != g.Name || w.Username != g.Username || w.Password != g.Password || w.URL != g.URL ||
			w.Notes != g.Notes || strings.Join(w.Tags, ",") != strings.Join(g.Tags, ",") || w.TOTPSecret != g.TOTPSecret {
			t.Fatalf("entry %d mismatch:\nwant %+v\n got %+v", i, w, g)
		}
	}
}

func TestExportImportRoundTrips(t *testing.T) {
	src := newTestVault(t)
	entries := mustEntries(t, src)
	pass := []byte("export-passphrase")

	bundle, err := ExportBundle(entries, pass, "v0.2.0-test", "passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bundle, []byte("alice")) || bytes.Contains(bundle, []byte("JBSWY3DP")) {
		t.Fatal("bundle leaks plaintext")
	}
	plainJSON, _ := ExportPlainJSON(entries, "v")
	csvFull, _ := ExportCSV(entries, true)
	csvBasic, _ := ExportCSV(entries, false)
	bw, _ := ExportBitwarden(entries)
	if !strings.HasPrefix(string(csvBasic), "name,url,username,password,note\n") {
		t.Fatalf("bad csv header: %q", string(csvBasic)[:40])
	}
	encCopy := filepath.Join(t.TempDir(), "copy.dat")
	if err := src.ExportEncrypted(encCopy); err != nil {
		t.Fatal(err)
	}
	encRaw, _ := os.ReadFile(encCopy)

	cases := []struct {
		name, format string
		raw          []byte
		pass         []byte
		full         bool
	}{
		{"json-encrypted", FormatJSONEncrypted, bundle, pass, true},
		{"json", FormatJSON, plainJSON, nil, true},
		{"csv-full", FormatCSV, csvFull, nil, true},
		{"csv", FormatCSV, csvBasic, nil, false},
		{"bitwarden", FormatBitwarden, bw, nil, false},
		{"encrypted", FormatEncrypted, encRaw, testMaster, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DetectFormat(c.raw); got != c.format {
				t.Fatalf("detect=%q want %q", got, c.format)
			}
			parsed, _, err := ParseImport(c.raw, "", c.pass)
			if err != nil {
				t.Fatal(err)
			}
			dst := New(filepath.Join(t.TempDir(), "vault.dat"))
			if err := dst.Init([]byte("another-master-99")); err != nil {
				t.Fatal(err)
			}
			res, err := dst.ImportEntries(parsed, true)
			if err != nil || res.Added != 2 {
				t.Fatalf("res=%+v err=%v", res, err)
			}
			got := mustEntries(t, dst)
			want := sampleEntries()
			if !c.full {
				for i := range want {
					if c.name == "csv" {
						want[i].Tags, want[i].TOTPSecret = nil, ""
					}
					if c.name == "bitwarden" && len(want[i].Tags) > 1 {
						want[i].Tags = want[i].Tags[:1]
					}
				}
			}
			assertSame(t, want, got)
			// 再导入一次：全部按 host+username 去重
			res, err = dst.ImportEntries(parsed, true)
			if err != nil || res.Added != 0 || res.Skipped != 2 {
				t.Fatalf("dedupe res=%+v err=%v", res, err)
			}
		})
	}
	if _, err := DecodeBundle(bundle, []byte("wrong-passphrase")); err == nil {
		t.Fatal("wrong passphrase should fail")
	}
	if _, _, err := ParseImport(bundle, "", nil); err != ErrNeedPassphrase {
		t.Fatalf("want ErrNeedPassphrase, got %v", err)
	}
}

func TestGoogleCSVImport(t *testing.T) {
	v := newTestVault(t)
	csv := "name,url,username,password,note\ngithub.com,https://github.com/,alice,x,\nexample.com,https://example.com/,carol,y,n\n"
	parsed, f, err := ParseImport([]byte(csv), "", nil)
	if err != nil || f != FormatCSV {
		t.Fatal(err, f)
	}
	res, err := v.ImportEntries(parsed, true)
	if err != nil || res.Added != 1 || res.Skipped != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestBackupRestoreRoundTrip(t *testing.T) {
	v := newTestVault(t)
	before := mustEntries(t, v)
	b, err := v.CreateBackup("")
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(b); fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup perm %o", fi.Mode().Perm())
	}
	if _, err := v.Add("extra", "x", "y", "", "", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := v.Delete("mail"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Restore(b, []byte("wrong-master-xx")); err == nil {
		t.Fatal("restore with wrong password must fail")
	}
	pre, err := v.Restore(b, testMaster)
	if err != nil {
		t.Fatal(err)
	}
	if v.IsUnlocked() {
		t.Fatal("restore should lock")
	}
	if n, _, err := VerifyFile(pre, testMaster); err != nil || n != 2 {
		t.Fatalf("pre-restore backup n=%d err=%v", n, err)
	}
	if err := v.Unlock(testMaster); err != nil {
		t.Fatal(err)
	}
	assertSame(t, before, mustEntries(t, v))
}

func TestStaleHandleRefusesWriteAfterRestore(t *testing.T) {
	v := newTestVault(t)
	b, _ := v.CreateBackup("")
	other := New(v.Path())
	if err := other.Unlock(testMaster); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Add("x", "", "", "", "", nil, ""); err != nil {
		t.Fatal(err)
	}
	// v 仍持有旧内存状态，磁盘已被 other 修改
	if _, err := v.Add("y", "", "", "", "", nil, ""); err == nil {
		t.Fatal("stale handle must refuse to overwrite")
	}
	_ = b
}

func TestBackupPruneAndAuto(t *testing.T) {
	v := newTestVault(t)
	v.KeepBackups = 3
	for i := 0; i < 5; i++ {
		if _, err := v.CreateBackup(BackupManual); err != nil {
			t.Fatal(err)
		}
	}
	count := func(kind string) int {
		list, _ := v.ListBackups()
		n := 0
		for _, b := range list {
			if b.Kind == kind {
				n++
			}
		}
		return n
	}
	if count(BackupManual) != 3 {
		t.Fatalf("manual=%d want 3", count(BackupManual))
	}
	if count(BackupDaily) != 1 { // newTestVault 的第一次写入（导入）触发
		t.Fatalf("daily=%d want 1", count(BackupDaily))
	}
	if _, err := v.Add("z", "", "", "", "", nil, ""); err != nil {
		t.Fatal(err)
	}
	if count(BackupDaily) != 1 {
		t.Fatal("daily backup should happen only once per day")
	}
	// 新版本首次运行
	v.AppVersion = "v9.9.9"
	p, err := v.EnsureVersionBackup()
	if err != nil || p == "" || !strings.Contains(p, "v9.9.9-upgrade") {
		t.Fatalf("p=%q err=%v", p, err)
	}
	if p2, _ := v.EnsureVersionBackup(); p2 != "" {
		t.Fatal("version backup should only run once")
	}
	fi, _ := os.Stat(v.BackupDir())
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir perm %o", fi.Mode().Perm())
	}
}

func TestNewerFormatIsReadOnly(t *testing.T) {
	v := newTestVault(t)
	v.Lock()
	raw, _ := os.ReadFile(v.Path())
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["version"] = FormatVersion + 1
	m["future_field"] = "x"
	out, _ := json.Marshal(m)
	_ = os.WriteFile(v.Path(), out, 0o600)

	r := New(v.Path())
	if err := r.Unlock(testMaster); err != nil {
		t.Fatal(err)
	}
	if len(mustEntries(t, r)) != 2 {
		t.Fatal("should still read entries")
	}
	if _, err := r.Add("x", "", "", "", "", nil, ""); err != ErrNewerFormat {
		t.Fatalf("want ErrNewerFormat, got %v", err)
	}
	after, _ := os.ReadFile(v.Path())
	if !bytes.Equal(after, out) {
		t.Fatal("file must be untouched")
	}
}

func TestUnknownFieldsPreserved(t *testing.T) {
	raw := []byte(`{"version":2,"future":{"a":1},"entries":[{"id":"1","name":"n","username":"u","password":"p","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","passkey":"xyz"}]}`)
	var d plainData
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(d)
	if !bytes.Contains(out, []byte(`"passkey":"xyz"`)) || !bytes.Contains(out, []byte(`"future":{"a":1}`)) {
		t.Fatalf("unknown fields lost: %s", out)
	}
}
