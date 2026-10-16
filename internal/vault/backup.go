package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// DefaultKeepBackups 每类自动备份默认保留份数。
const DefaultKeepBackups = 10

// 备份种类
const (
	BackupManual     = "manual"      // backup 命令 / API
	BackupDaily      = "daily"       // 每天第一次写入前
	BackupUpgrade    = "upgrade"     // 新版本程序首次运行
	BackupPreMigrate = "pre-migrate" // migrate 之前
	BackupPreRestore = "pre-restore" // restore 覆盖之前
	BackupPreImport  = "pre-import"  // 预留
)

// BackupInfo 一份备份文件。
type BackupInfo struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Kind    string    `json:"kind"`
	Version string    `json:"version"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

type backupState struct {
	LastVersion string `json:"last_version"`
	LastDaily   string `json:"last_daily"` // YYYY-MM-DD（本地时区）
}

var backupNameRe = regexp.MustCompile(`^vault-(\d{8}-\d{6})(?:-\d+)?-(.+)-(manual|daily|upgrade|pre-migrate|pre-restore|pre-import)\.dat$`)

// BackupDir 返回备份目录（保险箱同级 backups/）。
func (v *Vault) BackupDir() string { return filepath.Join(filepath.Dir(v.path), "backups") }

func (v *Vault) statePath() string { return filepath.Join(v.BackupDir(), "state.json") }

func (v *Vault) keep() int {
	if v.KeepBackups > 0 {
		return v.KeepBackups
	}
	return DefaultKeepBackups
}

func sanitizeVersion(s string) string {
	if s == "" {
		s = "dev"
	}
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '+' || r == '-' {
			return r
		}
		return '_'
	}, s)
}

func (v *Vault) loadState() backupState {
	var st backupState
	if b, err := os.ReadFile(v.statePath()); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

func (v *Vault) saveState(st backupState) error {
	if err := os.MkdirAll(v.BackupDir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	return writeFileAtomic(v.statePath(), b)
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0o600)
}

// createBackupLocked 复制当前磁盘上的保险箱文件（加密态）到备份目录。调用方需持有锁或保证无并发写。
// 保险箱文件不存在时返回 ("", nil)。
func (v *Vault) createBackupLocked(kind string) (string, error) {
	raw, err := os.ReadFile(v.path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	dir := v.BackupDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(dir, 0o700)
	ts := time.Now().Format("20060102-150405")
	ver := sanitizeVersion(v.AppVersion)
	name := fmt.Sprintf("vault-%s-%s-%s.dat", ts, ver, kind)
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			break
		}
		name = fmt.Sprintf("vault-%s-%d-%s-%s.dat", ts, i, ver, kind)
	}
	dst := filepath.Join(dir, name)
	if err := writeFileAtomic(dst, raw); err != nil {
		return "", err
	}
	v.pruneBackups(kind)
	return dst, nil
}

// CreateBackup 立即创建一份备份（kind 为空时为 manual）。
func (v *Vault) CreateBackup(kind string) (string, error) {
	if kind == "" {
		kind = BackupManual
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	p, err := v.createBackupLocked(kind)
	if err == nil && p == "" {
		return "", ErrNotInitialized
	}
	return p, err
}

func (v *Vault) pruneBackups(kind string) {
	list, err := v.ListBackups()
	if err != nil {
		return
	}
	n := 0
	for _, b := range list { // 新 -> 旧
		if b.Kind != kind {
			continue
		}
		n++
		if n > v.keep() {
			_ = os.Remove(b.Path)
		}
	}
}

// ListBackups 列出备份，最新在前。
func (v *Vault) ListBackups() ([]BackupInfo, error) {
	ents, err := os.ReadDir(v.BackupDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []BackupInfo
	for _, e := range ents {
		m := backupNameRe.FindStringSubmatch(e.Name())
		if m == nil || e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupInfo{
			Path: filepath.Join(v.BackupDir(), e.Name()), Name: e.Name(),
			Kind: m[3], Version: m[2], Size: info.Size(), ModTime: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// autoBackupBeforeWrite 每天第一次写入前做一次 daily 备份（调用方持有写锁）。
func (v *Vault) autoBackupBeforeWrite() error {
	if v.DisableAutoBackup {
		return nil
	}
	today := time.Now().Format("2006-01-02")
	st := v.loadState()
	if st.LastDaily == today {
		return nil
	}
	p, err := v.createBackupLocked(BackupDaily)
	if err != nil {
		return err
	}
	if p == "" {
		return nil
	}
	st.LastDaily = today
	return v.saveState(st)
}

// EnsureVersionBackup 新版本程序首次运行时备份一次（版本号记录在 backups/state.json）。
// 返回创建的备份路径；无需备份时返回 ""。
func (v *Vault) EnsureVersionBackup() (string, error) {
	if v.DisableAutoBackup || !v.Exists() {
		return "", nil
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	st := v.loadState()
	cur := sanitizeVersion(v.AppVersion)
	if st.LastVersion == cur {
		return "", nil
	}
	p, err := v.createBackupLocked(BackupUpgrade)
	if err != nil {
		return "", err
	}
	st.LastVersion = cur
	if err := v.saveState(st); err != nil {
		return p, err
	}
	return p, nil
}

// VerifyFile 用主密码检查一个保险箱文件（备份）能否解密，返回条目数与格式版本。
func VerifyFile(path string, masterPassword []byte) (entries int, format int, err error) {
	t := New(path)
	if err := t.Unlock(masterPassword); err != nil {
		return 0, 0, err
	}
	defer t.Lock()
	return t.EntryCount(), t.FormatVersion(), nil
}

// Restore 从备份文件恢复：先用主密码验证可解密，再把当前保险箱备份为 pre-restore，最后原子覆盖。
// 恢复后当前句柄会被锁定，需要重新解锁。返回 pre-restore 备份路径。
func (v *Vault) Restore(src string, masterPassword []byte) (string, error) {
	if _, _, err := VerifyFile(src, masterPassword); err != nil {
		return "", fmt.Errorf("备份文件校验失败，未做任何修改: %w", err)
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	v.Lock()
	v.mu.Lock()
	defer v.mu.Unlock()
	pre, err := v.createBackupLocked(BackupPreRestore)
	if err != nil {
		return "", fmt.Errorf("恢复前备份当前保险箱失败，已中止: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(v.path), 0o700); err != nil {
		return pre, err
	}
	if err := writeFileAtomic(v.path, raw); err != nil {
		return pre, err
	}
	v.format = 0
	v.diskSum = [32]byte{}
	return pre, nil
}
