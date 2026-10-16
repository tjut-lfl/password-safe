package client

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
)

func decodeJSON(r io.Reader, v any) error { return json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(v) }

func RandomToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func fileExists(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }
