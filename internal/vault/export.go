package vault

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tjut-lfl/password-safe/internal/crypto"
)

// 导出/导入格式
const (
	FormatEncrypted     = "encrypted"      // 保险箱文件原样副本（同一主密码）
	FormatJSONEncrypted = "json-encrypted" // 可移植加密 JSON 包
	FormatJSON          = "json"           // 明文 JSON
	FormatCSV           = "csv"            // 明文 CSV（Google/Chrome 兼容）
	FormatCSVFull       = "csv-full"       // 明文 CSV + tags,totp_secret
	FormatBitwarden     = "bitwarden"      // 明文 Bitwarden JSON
)

// BundleMagic / BundleSchemaVersion 可移植导出包标识与 schema 版本。
// 约定：后续版本必须能读取所有 schema_version <= 当前值的包。
const (
	BundleMagic         = "password-safe-export"
	PlainMagic          = "password-safe-plain"
	BundleSchemaVersion = 1
)

var ErrNeedPassphrase = errors.New("该文件已加密，需要口令")

// Bundle 可移植加密导出包（JSON）。
type Bundle struct {
	Format        string    `json:"format"`
	SchemaVersion int       `json:"schema_version"`
	AppVersion    string    `json:"app_version,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	KeySource     string    `json:"key_source"` // "passphrase" | "master"（仅提示）
	KDF           struct {
		Name    string `json:"name"`
		Time    uint32 `json:"time"`
		Memory  uint32 `json:"memory"`
		Threads uint8  `json:"threads"`
		Salt    string `json:"salt"`
	} `json:"kdf"`
	Cipher  string `json:"cipher"`
	Entries int    `json:"entries"`
	Data    string `json:"data"` // base64(nonce||AES-256-GCM(plainExport))
}

type plainExport struct {
	Format        string    `json:"format"`
	SchemaVersion int       `json:"schema_version"`
	AppVersion    string    `json:"app_version,omitempty"`
	ExportedAt    time.Time `json:"exported_at"`
	Entries       []Entry   `json:"entries"`
}

// Entries 返回全部条目副本（含密码，需已解锁）。
func (v *Vault) Entries() ([]Entry, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if !v.unlocked {
		return nil, ErrLocked
	}
	out := make([]Entry, len(v.data.Entries))
	copy(out, v.data.Entries)
	return out, nil
}

// ExportBundle 生成加密 JSON 包。keySource 仅作为提示写入（"passphrase" 或 "master"）。
func ExportBundle(entries []Entry, passphrase []byte, appVersion, keySource string) ([]byte, error) {
	if len(passphrase) < 8 {
		return nil, fmt.Errorf("导出口令至少 8 个字符")
	}
	pl, err := json.Marshal(plainExport{Format: PlainMagic, SchemaVersion: BundleSchemaVersion, AppVersion: appVersion, ExportedAt: time.Now().UTC(), Entries: nonNil(entries)})
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(pl)
	salt, err := crypto.GenerateSalt()
	if err != nil {
		return nil, err
	}
	p := crypto.DefaultParams()
	key := crypto.DeriveKey(passphrase, salt, p)
	defer crypto.Zero(key)
	enc, err := crypto.Encrypt(key, pl)
	if err != nil {
		return nil, err
	}
	b := Bundle{Format: BundleMagic, SchemaVersion: BundleSchemaVersion, AppVersion: appVersion, CreatedAt: time.Now().UTC(), KeySource: keySource, Cipher: "aes-256-gcm", Entries: len(entries), Data: crypto.EncodeB64(enc)}
	b.KDF.Name, b.KDF.Time, b.KDF.Memory, b.KDF.Threads, b.KDF.Salt = "argon2id", p.Time, p.Memory, p.Threads, crypto.EncodeB64(salt)
	return json.MarshalIndent(b, "", "  ")
}

// DecodeBundle 解密加密 JSON 包。
func DecodeBundle(raw, passphrase []byte) ([]Entry, error) {
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil || b.Format != BundleMagic {
		return nil, fmt.Errorf("不是 password-safe 加密导出包")
	}
	if b.SchemaVersion > BundleSchemaVersion {
		return nil, fmt.Errorf("导出包 schema_version=%d 高于本程序支持的 %d，请升级", b.SchemaVersion, BundleSchemaVersion)
	}
	if b.KDF.Name != "argon2id" || b.Cipher != "aes-256-gcm" {
		return nil, fmt.Errorf("不支持的 KDF/加密算法: %s/%s", b.KDF.Name, b.Cipher)
	}
	salt, err := crypto.DecodeB64(b.KDF.Salt)
	if err != nil {
		return nil, err
	}
	data, err := crypto.DecodeB64(b.Data)
	if err != nil {
		return nil, err
	}
	key := crypto.DeriveKey(passphrase, salt, crypto.Params{Time: b.KDF.Time, Memory: b.KDF.Memory, Threads: b.KDF.Threads})
	defer crypto.Zero(key)
	pl, err := crypto.Decrypt(key, data)
	if err != nil {
		return nil, fmt.Errorf("解密失败：口令错误或文件损坏")
	}
	defer crypto.Zero(pl)
	return decodePlainJSON(pl)
}

func decodePlainJSON(raw []byte) ([]Entry, error) {
	var p plainExport
	if err := json.Unmarshal(raw, &p); err != nil || p.Format != PlainMagic {
		return nil, fmt.Errorf("不是 password-safe 明文 JSON 导出")
	}
	if p.SchemaVersion > BundleSchemaVersion {
		return nil, fmt.Errorf("导出 schema_version=%d 高于本程序支持的 %d，请升级", p.SchemaVersion, BundleSchemaVersion)
	}
	return p.Entries, nil
}

func nonNil(e []Entry) []Entry {
	if e == nil {
		return []Entry{}
	}
	return e
}

// ExportPlainJSON 明文 JSON。
func ExportPlainJSON(entries []Entry, appVersion string) ([]byte, error) {
	return json.MarshalIndent(plainExport{Format: PlainMagic, SchemaVersion: BundleSchemaVersion, AppVersion: appVersion, ExportedAt: time.Now().UTC(), Entries: nonNil(entries)}, "", "  ")
}

// ExportCSV 明文 CSV：name,url,username,password,note[,tags,totp_secret]。
func ExportCSV(entries []Entry, full bool) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	h := []string{"name", "url", "username", "password", "note"}
	if full {
		h = append(h, "tags", "totp_secret")
	}
	_ = w.Write(h)
	for _, e := range entries {
		r := []string{e.Name, e.URL, e.Username, e.Password, e.Notes}
		if full {
			r = append(r, strings.Join(e.Tags, ";"), e.TOTPSecret)
		}
		_ = w.Write(r)
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

type bwURI struct {
	URI   string `json:"uri"`
	Match *int   `json:"match"`
}
type bwLogin struct {
	URIs     []bwURI `json:"uris,omitempty"`
	Username string  `json:"username"`
	Password string  `json:"password"`
	TOTP     string  `json:"totp,omitempty"`
}
type bwFolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type bwItem struct {
	ID       string   `json:"id,omitempty"`
	FolderID *string  `json:"folderId"`
	Type     int      `json:"type"`
	Name     string   `json:"name"`
	Notes    *string  `json:"notes"`
	Favorite bool     `json:"favorite"`
	Login    *bwLogin `json:"login,omitempty"`
}
type bwExport struct {
	Encrypted bool       `json:"encrypted"`
	Folders   []bwFolder `json:"folders"`
	Items     []bwItem   `json:"items"`
}

// ExportBitwarden 明文 Bitwarden JSON（首个 tag 作为 folder）。
func ExportBitwarden(entries []Entry) ([]byte, error) {
	out := bwExport{Folders: []bwFolder{}, Items: []bwItem{}}
	folders := map[string]string{}
	for _, e := range entries {
		it := bwItem{ID: e.ID, Type: 1, Name: e.Name, Login: &bwLogin{Username: e.Username, Password: e.Password, TOTP: e.TOTPSecret}}
		if e.Notes != "" {
			n := e.Notes
			it.Notes = &n
		}
		if e.URL != "" {
			it.Login.URIs = []bwURI{{URI: e.URL}}
		}
		if len(e.Tags) > 0 {
			id, ok := folders[e.Tags[0]]
			if !ok {
				id = newID()
				folders[e.Tags[0]] = id
				out.Folders = append(out.Folders, bwFolder{ID: id, Name: e.Tags[0]})
			}
			it.FolderID = &id
		}
		out.Items = append(out.Items, it)
	}
	return json.MarshalIndent(out, "", "  ")
}

func parseBitwarden(raw []byte) ([]Entry, error) {
	var b bwExport
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	if b.Encrypted {
		return nil, fmt.Errorf("不支持 Bitwarden 加密导出，请在 Bitwarden 中选择 .json（未加密）格式")
	}
	folders := map[string]string{}
	for _, f := range b.Folders {
		folders[f.ID] = f.Name
	}
	var out []Entry
	for _, it := range b.Items {
		if it.Type != 1 || it.Login == nil {
			continue
		}
		e := Entry{Name: it.Name, Username: it.Login.Username, Password: it.Login.Password, TOTPSecret: it.Login.TOTP}
		if it.Notes != nil {
			e.Notes = *it.Notes
		}
		if len(it.Login.URIs) > 0 {
			e.URL = it.Login.URIs[0].URI
		}
		if it.FolderID != nil && folders[*it.FolderID] != "" {
			e.Tags = []string{folders[*it.FolderID]}
		}
		out = append(out, e)
	}
	return out, nil
}

// DetectFormat 根据内容判断导入格式。
func DetectFormat(raw []byte) string {
	t := bytes.TrimSpace(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")))
	if len(t) > 0 && t[0] == '{' {
		var probe struct {
			Format     string          `json:"format"`
			KDF        json.RawMessage `json:"kdf"`
			WrappedDEK string          `json:"wrapped_dek"`
			Items      json.RawMessage `json:"items"`
			Data       string          `json:"data"`
		}
		if json.Unmarshal(t, &probe) == nil {
			switch {
			case probe.Format == BundleMagic:
				return FormatJSONEncrypted
			case probe.Format == PlainMagic:
				return FormatJSON
			case probe.Items != nil:
				return FormatBitwarden
			case probe.Data != "" && probe.KDF != nil:
				return FormatEncrypted
			}
		}
		return ""
	}
	return FormatCSV
}

// NeedsPassphrase 该格式导入时是否需要口令/主密码。
func NeedsPassphrase(format string) bool {
	return format == FormatJSONEncrypted || format == FormatEncrypted
}

// ParseImport 解析导入文件为条目列表。format 为空时自动识别。
// encrypted（保险箱文件副本）需要它自己的主密码；json-encrypted 需要导出口令。
func ParseImport(raw []byte, format string, passphrase []byte) ([]Entry, string, error) {
	if format == "" || format == "auto" {
		format = DetectFormat(raw)
		if format == "" {
			return nil, "", fmt.Errorf("无法识别的导入格式")
		}
	}
	switch format {
	case FormatJSONEncrypted:
		if len(passphrase) == 0 {
			return nil, format, ErrNeedPassphrase
		}
		e, err := DecodeBundle(raw, passphrase)
		return e, format, err
	case FormatJSON:
		e, err := decodePlainJSON(raw)
		return e, format, err
	case FormatBitwarden:
		e, err := parseBitwarden(raw)
		return e, format, err
	case FormatCSV, FormatCSVFull:
		e, err := ParseCSV(bytes.NewReader(raw))
		return e, format, err
	case FormatEncrypted:
		if len(passphrase) == 0 {
			return nil, format, ErrNeedPassphrase
		}
		f, err := os.CreateTemp("", "ps-import-*.dat")
		if err != nil {
			return nil, format, err
		}
		defer os.Remove(f.Name())
		_, _ = f.Write(raw)
		f.Close()
		t := New(f.Name())
		if err := t.Unlock(passphrase); err != nil {
			return nil, format, err
		}
		defer t.Lock()
		e, err := t.Entries()
		return e, format, err
	}
	return nil, format, fmt.Errorf("不支持的格式: %s", format)
}

// ParseCSV 解析 CSV（自动识别表头；无表头时列序 name,username,password,url,notes,tags,totp_secret）。
func ParseCSV(r io.Reader) ([]Entry, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV 解析失败: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	start := 0
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
	var out []Entry
	for _, row := range records[start:] {
		if len(row) == 0 || (len(row) == 1 && strings.TrimSpace(row[0]) == "") {
			continue
		}
		out = append(out, Entry{
			Name: strings.TrimSpace(get(row, "name")), Username: get(row, "username"), Password: get(row, "password"),
			URL: get(row, "url"), Notes: get(row, "notes"), Tags: splitTags(get(row, "tags")), TOTPSecret: get(row, "totp"),
		})
	}
	return out, nil
}

// ImportResult 导入统计。
type ImportResult struct {
	Added   int `json:"added"`
	Skipped int `json:"skipped"`
}

func dupKey(url, name, username string) string {
	h := strings.ToLower(strings.TrimPrefix(hostOf(url), "www."))
	if h == "" {
		h = strings.ToLower(name)
	}
	return h + "\x00" + strings.ToLower(username)
}

// ImportEntries 追加条目。skipDuplicates 时按 host(url)+username 去重（无 url 时用名称）。
// 保留原 ID（若不冲突）与时间戳，保证导出/导入往返一致。
func (v *Vault) ImportEntries(in []Entry, skipDuplicates bool) (ImportResult, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	var res ImportResult
	if !v.unlocked {
		return res, ErrLocked
	}
	seen := map[string]bool{}
	ids := map[string]bool{}
	for _, e := range v.data.Entries {
		seen[dupKey(e.URL, e.Name, e.Username)] = true
		ids[e.ID] = true
	}
	orig := len(v.data.Entries)
	for _, e := range in {
		if e.Name == "" {
			e.Name = hostOf(e.URL)
		}
		if e.Name == "" {
			res.Skipped++
			continue
		}
		k := dupKey(e.URL, e.Name, e.Username)
		if skipDuplicates && seen[k] {
			res.Skipped++
			continue
		}
		seen[k] = true
		if nameTaken(v.data.Entries, e.Name) && e.Username != "" && !nameTaken(v.data.Entries, e.Name+"/"+e.Username) {
			e.Name = e.Name + "/" + e.Username
		}
		if e.ID == "" || ids[e.ID] {
			e.ID = newID()
		}
		ids[e.ID] = true
		now := time.Now().UTC()
		if e.CreatedAt.IsZero() {
			e.CreatedAt = now
		}
		if e.UpdatedAt.IsZero() {
			e.UpdatedAt = now
		}
		v.data.Entries = append(v.data.Entries, e)
		res.Added++
	}
	if res.Added == 0 {
		return res, nil
	}
	if err := v.persist(); err != nil {
		v.data.Entries = v.data.Entries[:orig]
		return ImportResult{}, err
	}
	return res, nil
}
