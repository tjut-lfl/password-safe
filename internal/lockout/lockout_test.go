package lockout

import (
	"path/filepath"
	"testing"
)

func TestLockoutBackoff(t *testing.T) {
	dir := t.TempDir()
	vp := filepath.Join(dir, "vault.dat")
	if err := Check(vp); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < DefaultMaxAttempts-1; i++ {
		_ = RecordFailure(vp, DefaultMaxAttempts)
		if err := Check(vp); err != nil {
			t.Fatalf("should not lock yet at %d: %v", i+1, err)
		}
	}
	_ = RecordFailure(vp, DefaultMaxAttempts)
	if err := Check(vp); err == nil {
		t.Fatal("expected lockout")
	}
	Clear(vp)
	if err := Check(vp); err != nil {
		t.Fatal(err)
	}
}
