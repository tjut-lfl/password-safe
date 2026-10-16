// Package vault 管理加密保险箱的存储与条目 CRUD（v2: KEK/DEK）。
package vault

import (
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tjut-lfl/password-safe/internal/crypto"
	"github.com/tjut-lfl/password-safe/internal/lockout"
)

const (
	// FormatVersion 当前磁盘信封版本（v2 = KEK/DEK）。
	FormatVersion = 2
	// DataVersion 明文数据内版本。
	DataVersion = 2
)

var (
	ErrNotInitialized = errors.New("保险箱尚未初始化，请先运行 init")
	ErrAlreadyInit    = errors.New("保险箱已存在")
	ErrLocked         = errors.New("保险箱已锁定，请先 unlock")
	ErrNotFound       = errors.New("条目不存在")
	ErrInvalidName    = errors.New("条目名称不能为空")
	ErrNeedsMigrate   = errors.New("保险箱为旧格式，请运行 migrate")
)

// Entry 一条账号凭据（解密后内存形态）。
type Entry struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Username   string    `json:"username"`
	Password   string    `json:"password"`
	URL        string    `json:"url,omitempty"`
	Notes      string    `json:"notes,omitempty"`
	Tags       []string  `json:"tags,omitempty"`
	TOTPSecret string    `json:"totp_secret,omitempty"` // base32 密钥，用于 2FA
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// EntryMeta 列表用元数据（不含密码与 TOTP 密钥）。
type EntryMeta struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Username  string    `json:"username"`
	URL       string    `json:"url,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	HasTOTP   bool      `json:"has_totp,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// plainData 解密后的保险箱内容。
type plainData struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// fileEnvelope 磁盘上的加密信封。
// v1: salt + data（data 用派生密钥直接加密）
// v2: salt + wrapped_dek + data（派生 KEK 包装 DEK，DEK 加密 data）
type fileEnvelope struct {
	Version       int    `json:"version"`
	KDF           string `json:"kdf"` // "argon2id"
	Time          uint32 `json:"time"`
	Memory        uint32 `json:"memory"`
	Threads       uint8  `json:"threads"`
	SaltB64       string `json:"salt"`
	WrappedDEKB64 string `json:"wrapped_dek,omitempty"` // v2
	DataB64       string `json:"data"`                  // AES-GCM(nonce||ciphertext)
}

// Vault 线程安全的保险箱。
type Vault struct {
	mu       sync.RWMutex
	path     string
	unlocked bool
	dek      []byte // data encryption key (v2)；v1 解锁后也会填入派生密钥
	kek      []byte // 仅在内存中短暂保留（passwd 用）；一般可为空
	params   crypto.Params
	salt     []byte
	format   int // 磁盘信封版本
	data     plainData
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// DefaultPath 返回默认保险箱路径 ~/.password-safe/vault.dat
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".password-safe", "vault.dat"), nil
}

// New 创建 Vault 句柄（尚未解锁）。
func New(path string) *Vault {
	return &Vault{path: path}
}

func (v *Vault) Path() string { return v.path }

func (v *Vault) IsUnlocked() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.unlocked
}

// FormatVersion 返回磁盘信封版本（未读文件时为 0）。
func (v *Vault) FormatVersion() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.format != 0 {
		return v.format
	}
	env, err := v.readEnvelopeUnlocked()
	if err != nil {
		return 0
	}
	return env.Version
}

// Exists 检查保险箱文件是否存在。
func (v *Vault) Exists() bool {
	_, err := os.Stat(v.path)
	return err == nil
}

// Init 用主密码创建空保险箱（v2 KEK/DEK）。
func (v *Vault) Init(masterPassword []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if _, err := os.Stat(v.path); err == nil {
		return ErrAlreadyInit
	}

	dir := filepath.Dir(v.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	salt, err := crypto.GenerateSalt()
	if err != nil {
		return err
	}
	params := crypto.DefaultParams()
	kek := crypto.DeriveKey(masterPassword, salt, params)
	dek, err := crypto.GenerateKey()
	if err != nil {
		crypto.Zero(kek)
		return err
	}
	wrapped, err := crypto.WrapKey(kek, dek)
	crypto.Zero(kek)
	if err != nil {
		crypto.Zero(dek)
		return err
	}

	data := plainData{Version: DataVersion, Entries: []Entry{}}
	if err := v.writeEncryptedV2(dek, salt, params, wrapped, data); err != nil {
		crypto.Zero(dek)
		return err
	}

	v.dek = dek
	v.salt = salt
	v.params = params
	v.format = FormatVersion
	v.data = data
	v.unlocked = true
	lockout.Clear(v.path)
	return nil
}

// Unlock 用主密码解锁（支持 v1 与 v2；v1 解锁后仍可读写，建议 migrate）。
func (v *Vault) Unlock(masterPassword []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if err := lockout.Check(v.path); err != nil {
		return err
	}

	env, err := v.readEnvelopeUnlocked()
	if err != nil {
		return err
	}
	salt, err := crypto.DecodeB64(env.SaltB64)
	if err != nil {
		return fmt.Errorf("无效 salt: %w", err)
	}
	params := crypto.Params{Time: env.Time, Memory: env.Memory, Threads: env.Threads}
	kek := crypto.DeriveKey(masterPassword, salt, params)

	raw, err := crypto.DecodeB64(env.DataB64)
	if err != nil {
		crypto.Zero(kek)
		return fmt.Errorf("无效密文: %w", err)
	}

	var dek []byte
	ver := env.Version
	if ver == 0 {
		ver = 1 // 旧文件可能缺 version
	}

	if ver >= 2 {
		if env.WrappedDEKB64 == "" {
			crypto.Zero(kek)
			_ = lockout.RecordFailure(v.path, lockout.DefaultMaxAttempts)
			return crypto.ErrDecryptFailed
		}
		wrapped, err := crypto.DecodeB64(env.WrappedDEKB64)
		if err != nil {
			crypto.Zero(kek)
			return fmt.Errorf("无效 wrapped_dek: %w", err)
		}
		dek, err = crypto.UnwrapKey(kek, wrapped)
		crypto.Zero(kek)
		if err != nil {
			_ = lockout.RecordFailure(v.path, lockout.DefaultMaxAttempts)
			return err
		}
	} else {
		// v1: 派生密钥直接解密 data
		dek = kek
	}

	plain, err := crypto.Decrypt(dek, raw)
	if err != nil {
		crypto.Zero(dek)
		_ = lockout.RecordFailure(v.path, lockout.DefaultMaxAttempts)
		return err
	}
	var data plainData
	if err := json.Unmarshal(plain, &data); err != nil {
		crypto.Zero(dek)
		crypto.Zero(plain)
		_ = lockout.RecordFailure(v.path, lockout.DefaultMaxAttempts)
		return crypto.ErrDecryptFailed
	}
	crypto.Zero(plain)

	if v.dek != nil {
		crypto.Zero(v.dek)
	}
	v.dek = dek
	v.salt = salt
	v.params = params
	v.format = ver
	v.data = data
	v.unlocked = true
	lockout.Clear(v.path)
	return nil
}

// Lock 锁定并清零密钥。
func (v *Vault) Lock() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dek != nil {
		crypto.Zero(v.dek)
		v.dek = nil
	}
	if v.kek != nil {
		crypto.Zero(v.kek)
		v.kek = nil
	}
	v.data = plainData{}
	v.unlocked = false
}

// MigrateToV2 将已解锁的 v1 保险箱升级为 v2（KEK/DEK）。需要再次提供主密码以包装 DEK。
func (v *Vault) MigrateToV2(masterPassword []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return ErrLocked
	}
	if v.format >= FormatVersion {
		return fmt.Errorf("已是 v%d 格式，无需迁移", v.format)
	}

	salt, err := crypto.GenerateSalt()
	if err != nil {
		return err
	}
	params := crypto.DefaultParams()
	kek := crypto.DeriveKey(masterPassword, salt, params)
	dek, err := crypto.GenerateKey()
	if err != nil {
		crypto.Zero(kek)
		return err
	}
	wrapped, err := crypto.WrapKey(kek, dek)
	crypto.Zero(kek)
	if err != nil {
		crypto.Zero(dek)
		return err
	}

	// 用新 DEK 重加密全部条目明文
	data := v.data
	if data.Version < DataVersion {
		data.Version = DataVersion
	}
	if err := v.writeEncryptedV2(dek, salt, params, wrapped, data); err != nil {
		crypto.Zero(dek)
		return err
	}
	crypto.Zero(v.dek)
	v.dek = dek
	v.salt = salt
	v.params = params
	v.format = FormatVersion
	v.data = data
	return nil
}

// ChangeMasterPassword / Passwd：仅重新包装 DEK，不重加密条目（需 v2）。
func (v *Vault) ChangeMasterPassword(newPassword []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return ErrLocked
	}
	if v.format < 2 {
		return fmt.Errorf("请先 migrate 到 v2 后再更换主密码（passwd 仅重包装 DEK）")
	}
	salt, err := crypto.GenerateSalt()
	if err != nil {
		return err
	}
	params := crypto.DefaultParams()
	kek := crypto.DeriveKey(newPassword, salt, params)
	wrapped, err := crypto.WrapKey(kek, v.dek)
	crypto.Zero(kek)
	if err != nil {
		return err
	}
	if err := v.writeEncryptedV2(v.dek, salt, params, wrapped, v.data); err != nil {
		return err
	}
	v.salt = salt
	v.params = params
	return nil
}

// Verify 校验当前解锁状态下可解密且 GCM 标签完整；并重新从磁盘读一次验证。
func (v *Vault) Verify(masterPassword []byte) error {
	// 先确保能解锁
	tmp := New(v.path)
	if err := tmp.Unlock(masterPassword); err != nil {
		return err
	}
	defer tmp.Lock()

	tmp.mu.RLock()
	n := len(tmp.data.Entries)
	ver := tmp.format
	tmp.mu.RUnlock()

	// 再读信封并单独验证 data 解密
	env, err := tmp.readEnvelopeUnlocked()
	if err != nil {
		return err
	}
	raw, err := crypto.DecodeB64(env.DataB64)
	if err != nil {
		return err
	}
	plain, err := crypto.Decrypt(tmp.dek, raw)
	if err != nil {
		return err
	}
	crypto.Zero(plain)
	_ = n
	_ = ver
	return nil
}

// Add 添加条目。
func (v *Vault) Add(name, username, password, url, notes string, tags []string, totpSecret string) (*Entry, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return nil, ErrLocked
	}
	if name == "" {
		return nil, ErrInvalidName
	}
	now := time.Now().UTC()
	e := Entry{
		ID:         newID(),
		Name:       name,
		Username:   username,
		Password:   password,
		URL:        url,
		Notes:      notes,
		Tags:       tags,
		TOTPSecret: totpSecret,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	v.data.Entries = append(v.data.Entries, e)
	if err := v.persist(); err != nil {
		v.data.Entries = v.data.Entries[:len(v.data.Entries)-1]
		return nil, err
	}
	return &e, nil
}

// List 返回元数据列表。
func (v *Vault) List() ([]EntryMeta, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if !v.unlocked {
		return nil, ErrLocked
	}
	out := make([]EntryMeta, 0, len(v.data.Entries))
	for _, e := range v.data.Entries {
		out = append(out, toMeta(e))
	}
	return out, nil
}

// Search 按 name/username/url/tags 子串过滤（不区分大小写）。
func (v *Vault) Search(query string) ([]EntryMeta, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if !v.unlocked {
		return nil, ErrLocked
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]EntryMeta, 0)
	for _, e := range v.data.Entries {
		if q == "" || matchEntry(e, q) {
			out = append(out, toMeta(e))
		}
	}
	return out, nil
}

func matchEntry(e Entry, q string) bool {
	if strings.Contains(strings.ToLower(e.Name), q) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Username), q) {
		return true
	}
	if strings.Contains(strings.ToLower(e.URL), q) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Notes), q) {
		return true
	}
	for _, t := range e.Tags {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	return false
}

func toMeta(e Entry) EntryMeta {
	return EntryMeta{
		ID: e.ID, Name: e.Name, Username: e.Username,
		URL: e.URL, Tags: e.Tags, HasTOTP: e.TOTPSecret != "",
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

// TreeLines 按层级名称（work/github）生成树形文本行。
func (v *Vault) TreeLines() ([]string, error) {
	list, err := v.List()
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	type node struct {
		name     string
		children map[string]*node
		entries  []EntryMeta
	}
	root := &node{children: map[string]*node{}}
	for _, m := range list {
		parts := strings.Split(m.Name, "/")
		cur := root
		for i, p := range parts {
			if p == "" {
				continue
			}
			if i == len(parts)-1 {
				cur.entries = append(cur.entries, m)
				continue
			}
			if cur.children[p] == nil {
				cur.children[p] = &node{name: p, children: map[string]*node{}}
			}
			cur = cur.children[p]
		}
	}
	var lines []string
	var walk func(n *node, prefix string, isLast bool)
	walk = func(n *node, prefix string, isLast bool) {
		keys := make([]string, 0, len(n.children))
		for k := range n.children {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			last := i == len(keys)-1 && len(n.entries) == 0
			branch := "├── "
			nextPref := prefix + "│   "
			if last {
				branch = "└── "
				nextPref = prefix + "    "
			}
			lines = append(lines, prefix+branch+k+"/")
			walk(n.children[k], nextPref, last)
		}
		for i, e := range n.entries {
			last := i == len(n.entries)-1
			branch := "├── "
			if last {
				branch = "└── "
			}
			leaf := e.Name
			if idx := strings.LastIndex(e.Name, "/"); idx >= 0 {
				leaf = e.Name[idx+1:]
			}
			extra := ""
			if e.Username != "" {
				extra = "  (" + e.Username + ")"
			}
			lines = append(lines, prefix+branch+leaf+extra)
		}
	}
	// 顶层：直接列出 root 的 children 与 entries
	keys := make([]string, 0, len(root.children))
	for k := range root.children {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		last := i == len(keys)-1 && len(root.entries) == 0
		branch := "├── "
		nextPref := "│   "
		if last {
			branch = "└── "
			nextPref = "    "
		}
		lines = append(lines, branch+k+"/")
		walk(root.children[k], nextPref, last)
	}
	for i, e := range root.entries {
		last := i == len(root.entries)-1
		branch := "├── "
		if last {
			branch = "└── "
		}
		extra := ""
		if e.Username != "" {
			extra = "  (" + e.Username + ")"
		}
		lines = append(lines, branch+e.Name+extra)
	}
	if len(lines) == 0 {
		lines = []string{"(空)"}
	}
	return lines, nil
}

// Get 按 ID 或名称获取完整条目（名称取第一条匹配）。
func (v *Vault) Get(idOrName string) (*Entry, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if !v.unlocked {
		return nil, ErrLocked
	}
	for i := range v.data.Entries {
		e := v.data.Entries[i]
		if e.ID == idOrName || e.Name == idOrName {
			cp := e
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

// UpdateOpts 更新选项。
type UpdateOpts struct {
	Name       *string
	Username   *string
	Password   *string
	URL        *string
	Notes      *string
	Tags       *[]string
	TOTPSecret *string
}

func (v *Vault) Update(idOrName string, opts UpdateOpts) (*Entry, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return nil, ErrLocked
	}
	idx := -1
	for i, e := range v.data.Entries {
		if e.ID == idOrName || e.Name == idOrName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, ErrNotFound
	}
	e := &v.data.Entries[idx]
	if opts.Name != nil {
		if *opts.Name == "" {
			return nil, ErrInvalidName
		}
		e.Name = *opts.Name
	}
	if opts.Username != nil {
		e.Username = *opts.Username
	}
	if opts.Password != nil {
		e.Password = *opts.Password
	}
	if opts.URL != nil {
		e.URL = *opts.URL
	}
	if opts.Notes != nil {
		e.Notes = *opts.Notes
	}
	if opts.Tags != nil {
		e.Tags = *opts.Tags
	}
	if opts.TOTPSecret != nil {
		e.TOTPSecret = *opts.TOTPSecret
	}
	e.UpdatedAt = time.Now().UTC()
	if err := v.persist(); err != nil {
		return nil, err
	}
	cp := *e
	return &cp, nil
}

// Delete 删除条目。
func (v *Vault) Delete(idOrName string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return ErrLocked
	}
	idx := -1
	for i, e := range v.data.Entries {
		if e.ID == idOrName || e.Name == idOrName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	removed := v.data.Entries[idx]
	v.data.Entries = append(v.data.Entries[:idx], v.data.Entries[idx+1:]...)
	if err := v.persist(); err != nil {
		v.data.Entries = append(v.data.Entries[:idx], append([]Entry{removed}, v.data.Entries[idx:]...)...)
		return err
	}
	return nil
}

// ExportEncrypted 导出当前保险箱文件的原始加密内容。
func (v *Vault) ExportEncrypted(destPath string) error {
	v.mu.RLock()
	defer v.mu.RUnlock()
	raw, err := os.ReadFile(v.path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotInitialized
		}
		return err
	}
	return os.WriteFile(destPath, raw, 0o600)
}

// ImportCSV 从 CSV 导入（列：name,username,password,url,notes,tags[,totp_secret]）。
// 首行若为表头则自动跳过。tags 用分号或逗号分隔。
func (v *Vault) ImportCSV(r io.Reader) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return 0, ErrLocked
	}
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil {
		return 0, fmt.Errorf("CSV 解析失败: %w", err)
	}
	if len(records) == 0 {
		return 0, nil
	}
	start := 0
	// 默认列顺序: name,username,password,url,notes,tags,totp_secret
	col := map[string]int{"name": 0, "username": 1, "password": 2, "url": 3, "notes": 4, "tags": 5, "totp": 6}
	if looksLikeHeader(records[0]) {
		start = 1
		col = map[string]int{}
		alias := map[string]string{
			"name": "name", "title": "name", "entry": "name",
			"username": "username", "login": "username", "user": "username", "login_username": "username",
			"password": "password", "login_password": "password",
			"url": "url", "login_uri": "url", "website": "url",
			"note": "notes", "notes": "notes", "extra": "notes",
			"tags": "tags", "folder": "tags", "grouping": "tags",
			"totp": "totp", "totp_secret": "totp", "login_totp": "totp", "otpauth": "totp",
		}
		for i, h := range records[0] {
			k := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))
			if f, ok := alias[k]; ok {
				if _, dup := col[f]; !dup {
					col[f] = i
				}
			}
		}
	}
	get := func(row []string, f string) string {
		i, ok := col[f]
		if !ok || i >= len(row) {
			return ""
		}
		return row[i]
	}
	added := 0
	for _, row := range records[start:] {
		if len(row) == 0 {
			continue
		}
		username, password, url := get(row, "username"), get(row, "password"), get(row, "url")
		notes, tagsStr, totp := get(row, "notes"), get(row, "tags"), get(row, "totp")
		name := strings.TrimSpace(get(row, "name"))
		if name == "" {
			name = hostOf(url)
		}
		if name == "" {
			continue
		}
		if username != "" && nameTaken(v.data.Entries, name) {
			name = name + "/" + username
		}
		var tags []string
		for _, t := range splitTags(tagsStr) {
			tags = append(tags, t)
		}
		now := time.Now().UTC()
		e := Entry{
			ID: newID(), Name: name, Username: username, Password: password,
			URL: url, Notes: notes, Tags: tags, TOTPSecret: totp,
			CreatedAt: now, UpdatedAt: now,
		}
		v.data.Entries = append(v.data.Entries, e)
		added++
	}
	if added == 0 {
		return 0, nil
	}
	if err := v.persist(); err != nil {
		// 回滚本次追加
		v.data.Entries = v.data.Entries[:len(v.data.Entries)-added]
		return 0, err
	}
	return added, nil
}

func looksLikeHeader(row []string) bool {
	for _, c := range row {
		h := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(c, "\ufeff")))
		if h == "name" || h == "title" || h == "password" || h == "url" || h == "username" {
			return true
		}
	}
	return false
}

func hostOf(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	return u
}

func nameTaken(es []Entry, name string) bool {
	for _, e := range es {
		if e.Name == name {
			return true
		}
	}
	return false
}

func splitTags(s string) []string {
	s = strings.ReplaceAll(s, ";", ",")
	var out []string
	for _, t := range strings.Split(s, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func (v *Vault) persist() error {
	if v.format >= 2 {
		// 需要重新包装：用现有 salt/params，但 wrapped_dek 已在文件中。
		// 为简单起见，每次 persist 时用当前 salt 重新读 wrapped 不方便；
		// 我们重新用当前 dek 写文件，但 wrapped_dek 必须保留。
		// 因此 persist 时从磁盘读出旧 wrapped_dek，或在内存中保留。
		return v.persistV2()
	}
	// v1 兼容写入
	return v.writeEncryptedV1(v.dek, v.salt, v.params, v.data)
}

// wrappedDEK 缓存在解锁后：从信封取出。

func (v *Vault) persistV2() error {
	env, err := v.readEnvelopeUnlocked()
	if err != nil {
		return err
	}
	wrapped, err := crypto.DecodeB64(env.WrappedDEKB64)
	if err != nil {
		return err
	}
	return v.writeEncryptedV2(v.dek, v.salt, v.params, wrapped, v.data)
}

func (v *Vault) writeEncryptedV2(dek, salt []byte, params crypto.Params, wrappedDEK []byte, data plainData) error {
	if data.Version == 0 {
		data.Version = DataVersion
	}
	plain, err := json.Marshal(data)
	if err != nil {
		return err
	}
	enc, err := crypto.Encrypt(dek, plain)
	crypto.Zero(plain)
	if err != nil {
		return err
	}
	env := fileEnvelope{
		Version:       FormatVersion,
		KDF:           "argon2id",
		Time:          params.Time,
		Memory:        params.Memory,
		Threads:       params.Threads,
		SaltB64:       crypto.EncodeB64(salt),
		WrappedDEKB64: crypto.EncodeB64(wrappedDEK),
		DataB64:       crypto.EncodeB64(enc),
	}
	return v.writeEnvelope(env)
}

func (v *Vault) writeEncryptedV1(key, salt []byte, params crypto.Params, data plainData) error {
	plain, err := json.Marshal(data)
	if err != nil {
		return err
	}
	enc, err := crypto.Encrypt(key, plain)
	crypto.Zero(plain)
	if err != nil {
		return err
	}
	env := fileEnvelope{
		Version: 1,
		KDF:     "argon2id",
		Time:    params.Time,
		Memory:  params.Memory,
		Threads: params.Threads,
		SaltB64: crypto.EncodeB64(salt),
		DataB64: crypto.EncodeB64(enc),
	}
	return v.writeEnvelope(env)
}

func (v *Vault) writeEnvelope(env fileEnvelope) error {
	out, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	tmp := v.path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, v.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chmod(v.path, 0o600)
	return nil
}

func (v *Vault) readEnvelope() (*fileEnvelope, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.readEnvelopeUnlocked()
}

func (v *Vault) readEnvelopeUnlocked() (*fileEnvelope, error) {
	raw, err := os.ReadFile(v.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotInitialized
		}
		return nil, err
	}
	var env fileEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("保险箱文件格式无效")
	}
	if env.KDF != "argon2id" {
		return nil, fmt.Errorf("不支持的 KDF: %s", env.KDF)
	}
	return &env, nil
}

// EntryCount 返回条目数（需已解锁）。
func (v *Vault) EntryCount() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.data.Entries)
}
