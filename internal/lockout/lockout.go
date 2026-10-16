// Package lockout 实现失败解锁次数限制与指数退避。
package lockout

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	DefaultMaxAttempts = 5
	// 第 N 次失败后等待 BaseDelay * 2^(n-MaxAttempts) （n>=MaxAttempts）
	BaseDelay = 2 * time.Second
	MaxDelay  = 15 * time.Minute
)

// State 持久化在保险箱旁的 .lockout 文件。
type State struct {
	Failures    int       `json:"failures"`
	LockedUntil time.Time `json:"locked_until"`
}

// PathFor 返回与 vault 同目录的 lockout 文件路径。
func PathFor(vaultPath string) string {
	return vaultPath + ".lockout"
}

func load(path string) State {
	raw, err := os.ReadFile(path)
	if err != nil {
		return State{}
	}
	var s State
	if json.Unmarshal(raw, &s) != nil {
		return State{}
	}
	return s
}

func save(path string, s State) error {
	dir := filepath.Dir(path)
	_ = os.MkdirAll(dir, 0o700)
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Check 若当前处于锁定窗口则返回错误；否则 nil。
func Check(vaultPath string) error {
	s := load(PathFor(vaultPath))
	if s.LockedUntil.IsZero() {
		return nil
	}
	now := time.Now()
	if now.Before(s.LockedUntil) {
		wait := s.LockedUntil.Sub(now).Round(time.Second)
		return fmt.Errorf("解锁被暂时锁定，请等待 %v 后再试（连续失败 %d 次）", wait, s.Failures)
	}
	return nil
}

// RecordFailure 记录一次失败，必要时延长锁定。
func RecordFailure(vaultPath string, maxAttempts int) error {
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	path := PathFor(vaultPath)
	s := load(path)
	s.Failures++
	if s.Failures >= maxAttempts {
		// 指数退避：2s, 4s, 8s, ... 上限 MaxDelay
		exp := s.Failures - maxAttempts
		delay := BaseDelay << exp
		if delay > MaxDelay || delay <= 0 {
			delay = MaxDelay
		}
		s.LockedUntil = time.Now().Add(delay)
	}
	return save(path, s)
}

// Clear 解锁成功后清除计数。
func Clear(vaultPath string) {
	_ = os.Remove(PathFor(vaultPath))
}
