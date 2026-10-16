package crypto

import (
	"bytes"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	salt, err := GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}
	key := DeriveKey([]byte("test-master-password"), salt, DefaultParams())
	defer Zero(key)

	plain := []byte(`{"hello":"world","secret":"s3cret"}`)
	enc, err := Encrypt(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(enc, plain) {
		t.Fatal("ciphertext should differ from plaintext")
	}
	dec, err := Decrypt(key, enc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dec, plain) {
		t.Fatalf("got %q want %q", dec, plain)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	salt, _ := GenerateSalt()
	key1 := DeriveKey([]byte("password-one"), salt, DefaultParams())
	key2 := DeriveKey([]byte("password-two"), salt, DefaultParams())
	defer Zero(key1)
	defer Zero(key2)

	enc, err := Encrypt(key1, []byte("secret-data"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Decrypt(key2, enc)
	if err != ErrDecryptFailed {
		t.Fatalf("want ErrDecryptFailed, got %v", err)
	}
}

func TestGeneratePassword(t *testing.T) {
	pw, err := GeneratePassword(24, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pw) != 24 {
		t.Fatalf("len=%d", len(pw))
	}
	pw2, _ := GeneratePassword(24, true)
	if pw == pw2 {
		t.Fatal("passwords should be random")
	}
}

func TestGeneratePassphrase(t *testing.T) {
	pp, err := GeneratePassphrase(5, "-")
	if err != nil {
		t.Fatal(err)
	}
	parts := bytes.Split([]byte(pp), []byte("-"))
	if len(parts) != 5 {
		t.Fatalf("got %q", pp)
	}
}

func TestWrapUnwrapKey(t *testing.T) {
	kek, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	dek, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	defer Zero(kek)
	defer Zero(dek)
	wrapped, err := WrapKey(kek, dek)
	if err != nil {
		t.Fatal(err)
	}
	out, err := UnwrapKey(kek, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	defer Zero(out)
	if !Equal(out, dek) {
		t.Fatal("unwrap mismatch")
	}
}

func TestDeriveKeyDeterministic(t *testing.T) {
	salt := make([]byte, SaltSize)
	for i := range salt {
		salt[i] = byte(i)
	}
	p := DefaultParams()
	k1 := DeriveKey([]byte("same"), salt, p)
	k2 := DeriveKey([]byte("same"), salt, p)
	defer Zero(k1)
	defer Zero(k2)
	if !equal(k1, k2) {
		t.Fatal("same inputs should yield same key")
	}
	if len(k1) != KeySize {
		t.Fatalf("key size %d", len(k1))
	}
}

func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
