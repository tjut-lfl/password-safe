// Package server 提供仅绑定 localhost 的 HTTP API。
package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tjut-lfl/password-safe/internal/crypto"
	"github.com/tjut-lfl/password-safe/internal/vault"

	"github.com/pquerna/otp/totp"
)

// Server HTTP 保险箱服务。
type Server struct {
	vault   *vault.Vault
	addr    string
	mu      sync.RWMutex
	token   string
	tokenAt time.Time
	ttl     time.Duration
}

// DefaultSessionTTL 默认会话有效期（6 小时），可用环境变量 PASSWORD_SAFE_SESSION_TTL
// （Go duration，如 6h、30m）覆盖。无效值回退到默认。
func DefaultSessionTTL() time.Duration {
	const def = 6 * time.Hour
	if env := strings.TrimSpace(os.Getenv("PASSWORD_SAFE_SESSION_TTL")); env != "" {
		if d, err := time.ParseDuration(env); err == nil && d > 0 {
			return d
		}
	}
	return def
}

// FormatTTL 把 duration 格式化为简短 Go duration 字符串（如 6h、30m）。
func FormatTTL(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	if d%time.Second == 0 {
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
	return d.String()
}

func New(v *vault.Vault, addr string) *Server {
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	return &Server{vault: v, addr: addr, ttl: DefaultSessionTTL()}
}

// SetTTL 设置会话有效期；非正数时忽略。
func (s *Server) SetTTL(d time.Duration) {
	if d > 0 {
		s.ttl = d
	}
}

// TTL 返回当前会话有效期。
func (s *Server) TTL() time.Duration { return s.ttl }

func (s *Server) Addr() string { return s.addr }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/unlock", s.handleUnlock)
	mux.HandleFunc("/api/v1/lock", s.auth(s.handleLock))
	mux.HandleFunc("/api/v1/entries", s.auth(s.handleEntries))
	mux.HandleFunc("/api/v1/entries/", s.auth(s.handleEntryByID))
	mux.HandleFunc("/api/v1/search", s.auth(s.handleSearch))
	mux.HandleFunc("/api/v1/totp/", s.auth(s.handleTOTP))
	mux.HandleFunc("/api/v1/generate", s.auth(s.handleGenerate))
	mux.HandleFunc("/api/v1/export", s.auth(s.handleExport))
	mux.HandleFunc("/api/v1/backup", s.auth(s.handleBackup))
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/verify", s.auth(s.handleVerify))
	return mux
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	host, _, _ := net.SplitHostPort(ln.Addr().String())
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		_ = ln.Close()
		return errors.New("安全限制：仅允许绑定到 localhost (127.0.0.1 / ::1)")
	}
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	return srv.Serve(ln)
}

func (s *Server) issueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	s.token = tok
	s.tokenAt = time.Now()
	s.mu.Unlock()
	return tok, nil
}

func (s *Server) clearToken() {
	s.mu.Lock()
	s.token = ""
	s.tokenAt = time.Time{}
	s.mu.Unlock()
}

func (s *Server) validToken(t string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.token == "" || t == "" {
		return false
	}
	if time.Since(s.tokenAt) > s.ttl {
		return false
	}
	return crypto.Equal([]byte(s.token), []byte(t))
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		tok := strings.TrimPrefix(h, "Bearer ")
		if tok == h {
			tok = r.Header.Get("X-Session-Token")
		}
		if !s.validToken(tok) {
			writeErr(w, http.StatusUnauthorized, "未授权或会话已过期，请先 unlock")
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"status":    "ok",
		"service":   "password-safe",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"initialized":    s.vault.Exists(),
		"unlocked":       s.vault.IsUnlocked(),
		"format_version": s.vault.FormatVersion(),
	})
}

func (s *Server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Password == "" {
		writeErr(w, 400, "需要 password 字段")
		return
	}
	pw := []byte(body.Password)
	err := s.vault.Unlock(pw)
	crypto.Zero(pw)
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	tok, err := s.issueToken()
	if err != nil {
		writeErr(w, 500, "生成会话失败")
		return
	}
	writeJSON(w, 200, map[string]any{
		"token":          tok,
		"expires_in":     FormatTTL(s.ttl),
		"expires_in_sec": int(s.ttl.Seconds()),
		"format_version": s.vault.FormatVersion(),
	})
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	s.vault.Lock()
	s.clearToken()
	writeJSON(w, 200, map[string]string{"status": "locked"})
}

func (s *Server) handleEntries(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query().Get("q")
		if q == "" {
			q = r.URL.Query().Get("query")
		}
		var list []vault.EntryMeta
		var err error
		if q != "" {
			list, err = s.vault.Search(q)
		} else {
			list, err = s.vault.List()
		}
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"entries": list})
	case http.MethodPost:
		var body struct {
			Name       string   `json:"name"`
			Username   string   `json:"username"`
			Password   string   `json:"password"`
			URL        string   `json:"url"`
			Notes      string   `json:"notes"`
			Tags       []string `json:"tags"`
			TOTPSecret string   `json:"totp_secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, "无效 JSON")
			return
		}
		e, err := s.vault.Add(body.Name, body.Username, body.Password, body.URL, body.Notes, body.Tags, body.TOTPSecret)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 201, e)
	default:
		writeErr(w, 405, "方法不允许")
	}
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, "方法不允许")
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		q = r.URL.Query().Get("query")
	}
	list, err := s.vault.Search(q)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"entries": list, "query": q})
}

func (s *Server) handleTOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, "方法不允许")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/totp/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeErr(w, 400, "缺少条目 ID/名称")
		return
	}
	e, err := s.vault.Get(id)
	if err != nil {
		code := 404
		if errors.Is(err, vault.ErrLocked) {
			code = 401
		}
		writeErr(w, code, err.Error())
		return
	}
	if e.TOTPSecret == "" {
		writeErr(w, 400, "该条目未配置 TOTP")
		return
	}
	code, err := totp.GenerateCode(e.TOTPSecret, time.Now())
	if err != nil {
		writeErr(w, 400, "TOTP 生成失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"name": e.Name,
		"code": code,
	})
}

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	// 已通过 auth，保险箱已解锁；重新从磁盘验证 GCM
	if !s.vault.IsUnlocked() {
		writeErr(w, 401, vault.ErrLocked.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok":             true,
		"entries":        s.vault.EntryCount(),
		"format_version": s.vault.FormatVersion(),
	})
}

func (s *Server) handleEntryByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/entries/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeErr(w, 400, "缺少条目 ID")
		return
	}
	// 子路径 password：仅返回密码（会话已解锁时）
	if strings.HasSuffix(id, "/password") {
		id = strings.TrimSuffix(id, "/password")
		id = strings.Trim(id, "/")
		if r.Method != http.MethodGet {
			writeErr(w, 405, "方法不允许")
			return
		}
		e, err := s.vault.Get(id)
		if err != nil {
			code := 404
			if errors.Is(err, vault.ErrLocked) {
				code = 401
			}
			writeErr(w, code, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"name": e.Name, "password": e.Password})
		return
	}
	switch r.Method {
	case http.MethodGet:
		e, err := s.vault.Get(id)
		if err != nil {
			code := 404
			if errors.Is(err, vault.ErrLocked) {
				code = 401
			}
			writeErr(w, code, err.Error())
			return
		}
		writeJSON(w, 200, e)
	case http.MethodPut, http.MethodPatch:
		var body struct {
			Name       *string   `json:"name"`
			Username   *string   `json:"username"`
			Password   *string   `json:"password"`
			URL        *string   `json:"url"`
			Notes      *string   `json:"notes"`
			Tags       *[]string `json:"tags"`
			TOTPSecret *string   `json:"totp_secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, "无效 JSON")
			return
		}
		e, err := s.vault.Update(id, vault.UpdateOpts{
			Name: body.Name, Username: body.Username, Password: body.Password,
			URL: body.URL, Notes: body.Notes, Tags: body.Tags, TOTPSecret: body.TOTPSecret,
		})
		if err != nil {
			code := 400
			if errors.Is(err, vault.ErrNotFound) {
				code = 404
			}
			writeErr(w, code, err.Error())
			return
		}
		writeJSON(w, 200, e)
	case http.MethodDelete:
		if err := s.vault.Delete(id); err != nil {
			code := 400
			if errors.Is(err, vault.ErrNotFound) {
				code = 404
			}
			writeErr(w, code, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"status": "deleted"})
	default:
		writeErr(w, 405, "方法不允许")
	}
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		writeErr(w, 405, "方法不允许")
		return
	}
	length := 20
	symbols := true
	memorable := false
	words := 5
	if r.Method == http.MethodPost {
		var body struct {
			Length    int  `json:"length"`
			Symbols   bool `json:"symbols"`
			Memorable bool `json:"memorable"`
			Words     int  `json:"words"`
		}
		body.Symbols = true
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Length > 0 {
			length = body.Length
		}
		symbols = body.Symbols
		memorable = body.Memorable
		if body.Words > 0 {
			words = body.Words
		}
	}
	if memorable {
		pp, err := crypto.GeneratePassphrase(words, "-")
		if err != nil {
			writeErr(w, 500, "生成失败")
			return
		}
		writeJSON(w, 200, map[string]string{"password": pp, "type": "passphrase"})
		return
	}
	pw, err := crypto.GeneratePassword(length, symbols)
	if err != nil {
		writeErr(w, 500, "生成失败")
		return
	}
	writeJSON(w, 200, map[string]string{"password": pw, "type": "random"})
}

// handleExport 导出条目。默认返回加密 JSON 包（json-encrypted）。
// 请求体示例：
//
//	{"passphrase":"..."}                        → 加密 JSON（默认）
//	{"format":"json-encrypted","passphrase":"..."}
//	{"format":"csv","confirm":"EXPORT"}         → 明文 CSV（需显式确认，勿在日志中打印）
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	var body struct {
		Passphrase string `json:"passphrase"`
		Format     string `json:"format"`
		Confirm    string `json:"confirm"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, 400, "无效 JSON")
		return
	}
	format := strings.ToLower(strings.TrimSpace(body.Format))
	if format == "" {
		format = vault.FormatJSONEncrypted
	}
	entries, err := s.vault.Entries()
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	switch format {
	case vault.FormatJSONEncrypted, "encrypted-json":
		if len(body.Passphrase) < 8 {
			writeErr(w, 400, "需要 passphrase 字段（至少 8 个字符）")
			return
		}
		pass := []byte(body.Passphrase)
		data, err := vault.ExportBundle(entries, pass, s.vault.AppVersion, "passphrase")
		crypto.Zero(pass)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		name := "password-safe-export-" + time.Now().Format("20060102-150405") + ".json"
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename="+name)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(200)
		_, _ = w.Write(data)
	case vault.FormatCSV:
		if body.Confirm != "EXPORT" {
			writeErr(w, 400, `明文 CSV 导出需要 confirm 字段为 "EXPORT"`)
			return
		}
		data, err := vault.ExportCSV(entries, false)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		name := "password-safe-export-" + time.Now().Format("20060102") + ".csv"
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename="+name)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(200)
		_, _ = w.Write(data)
	default:
		writeErr(w, 400, "不支持的 format（支持 json-encrypted 或 csv）")
	}
}

// handleBackup 在服务端备份目录立即创建一份加密备份。
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	p, err := s.vault.CreateBackup(vault.BackupManual)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "file": filepath.Base(p), "dir": s.vault.BackupDir()})
}
