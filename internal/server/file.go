package server

import (
	"os"

	"github.com/tjut-lfl/password-safe/internal/vault"
)

func openRead(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, vault.ErrNotInitialized
		}
		return nil, err
	}
	return b, nil
}
