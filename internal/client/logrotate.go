package client

import (
	"fmt"
	"os"
	"sync"
)

// RotatingFile：超过 MaxBytes 时滚动为 .1 .. .N。
type RotatingFile struct {
	Path     string
	MaxBytes int64
	Keep     int
	mu       sync.Mutex
	f        *os.File
	size     int64
}

func (r *RotatingFile) open() error {
	f, err := os.OpenFile(r.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	st, _ := f.Stat()
	r.f, r.size = f, st.Size()
	return nil
}

func (r *RotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	if r.size+int64(len(p)) > r.MaxBytes {
		r.f.Close()
		for i := r.Keep - 1; i >= 1; i-- {
			os.Rename(fmt.Sprintf("%s.%d", r.Path, i), fmt.Sprintf("%s.%d", r.Path, i+1))
		}
		os.Rename(r.Path, r.Path+".1")
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}
