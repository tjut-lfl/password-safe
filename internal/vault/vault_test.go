package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tjut-lfl/password-safe/internal/crypto"
)

func TestVaultLifecycleV2(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.dat")
	v := New(path)

	master := []byte("master-pass-1234")
	if err := v.Init(master); err != nil {
		t.Fatal(err)
	}
	if v.FormatVersion() != FormatVersion {
		t.Fatalf("format=%d", v.FormatVersion())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm=%o want 0600", info.Mode().Perm())
	}

	e, err := v.Add("work/github", "alice", "p@ss", "https://github.com", "note", []string{"dev"}, "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if e.ID == "" || e.Name != "work/github" || e.TOTPSecret == "" {
		t.Fatalf("bad entry: %+v", e)
	}

	v.Lock()
	if v.IsUnlocked() {
		t.Fatal("should be locked")
	}

	v2 := New(path)
	if err := v2.Unlock([]byte("wrong-password!!!!")); err == nil {
		t.Fatal("wrong password should fail")
	}
	if err := v2.Unlock(master); err != nil {
		t.Fatal(err)
	}

	list, err := v2.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if !list[0].HasTOTP {
		t.Fatal("expected HasTOTP")
	}
	got, err := v2.Get("work/github")
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "p@ss" || got.Username != "alice" {
		t.Fatalf("got %+v", got)
	}

	found, err := v2.Search("github")
	if err != nil || len(found) != 1 {
		t.Fatalf("search=%v err=%v", found, err)
	}
	found, _ = v2.Search("nope-xyz")
	if len(found) != 0 {
		t.Fatal("expected empty search")
	}

	lines, err := v2.TreeLines()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "work") {
		t.Fatalf("tree missing work: %s", joined)
	}

	newUser := "bob"
	_, err = v2.Update(e.ID, UpdateOpts{Username: &newUser})
	if err != nil {
		t.Fatal(err)
	}
	got, _ = v2.Get(e.ID)
	if got.Username != "bob" {
		t.Fatalf("username=%s", got.Username)
	}

	newMaster := []byte("new-master-pass99")
	if err := v2.ChangeMasterPassword(newMaster); err != nil {
		t.Fatal(err)
	}
	v2.Lock()
	v3 := New(path)
	if err := v3.Unlock(master); err == nil {
		t.Fatal("old master should fail")
	}
	if err := v3.Unlock(newMaster); err != nil {
		t.Fatal(err)
	}
	got, err = v3.Get("work/github")
	if err != nil || got.Password != "p@ss" {
		t.Fatalf("after passwd: %+v err=%v", got, err)
	}

	if err := v3.Verify(newMaster); err != nil {
		t.Fatal(err)
	}

	csvData := "name,username,password,url,notes,tags\nmail,a@b.com,secret,https://mail,n,work\n"
	n, err := v3.ImportCSV(strings.NewReader(csvData))
	if err != nil || n != 1 {
		t.Fatalf("import n=%d err=%v", n, err)
	}

	if err := v3.Delete(e.ID); err != nil {
		t.Fatal(err)
	}

	exportPath := filepath.Join(dir, "export.dat")
	if err := v3.ExportEncrypted(exportPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(exportPath); err != nil {
		t.Fatal(err)
	}
}

func TestInitTwice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.dat")
	v := New(path)
	_ = v.Init([]byte("abcdefgh"))
	v.Lock()
	if err := v.Init([]byte("abcdefgh")); err != ErrAlreadyInit {
		t.Fatalf("got %v", err)
	}
}

func TestMigrateV1ToV2(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.dat")
	master := []byte("migrate-master1")
	now := time.Now().UTC()
	if err := writeTestV1(path, master, []Entry{{
		ID: "abc", Name: "old", Username: "u", Password: "p",
		CreatedAt: now, UpdatedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}
	v2 := New(path)
	if err := v2.Unlock(master); err != nil {
		t.Fatal(err)
	}
	if v2.FormatVersion() != 1 {
		t.Fatalf("want v1 got %d", v2.FormatVersion())
	}
	got, err := v2.Get("old")
	if err != nil || got.Password != "p" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := v2.MigrateToV2(master); err != nil {
		t.Fatal(err)
	}
	if v2.FormatVersion() != 2 {
		t.Fatalf("want v2 got %d", v2.FormatVersion())
	}
	v2.Lock()
	v3 := New(path)
	if err := v3.Unlock(master); err != nil {
		t.Fatal(err)
	}
	got, err = v3.Get("old")
	if err != nil || got.Password != "p" {
		t.Fatalf("after migrate %+v %v", got, err)
	}
}

func writeTestV1(path string, master []byte, entries []Entry) error {
	v := New(path)
	salt, err := crypto.GenerateSalt()
	if err != nil {
		return err
	}
	params := crypto.DefaultParams()
	key := crypto.DeriveKey(master, salt, params)
	data := plainData{Version: 1, Entries: entries}
	return v.writeEncryptedV1(key, salt, params, data)
}
