package client

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// App 是托盘进程的核心：隧道 + 本地控制/网页服务（127.0.0.1 随机端口，带随机令牌）。
type App struct {
	Dir, DataDir, Exe, Version string
	ExtFS                      fs.FS
	Log                        *log.Logger

	mu       sync.Mutex
	cfg      Config
	tun      *Tunnel
	cancel   context.CancelFunc
	ctx      context.Context
	token    string
	port     int
	OnStatus func(Status)
}

func (a *App) ExtensionDir() string { return filepath.Join(a.DataDir, "extension") }
func (a *App) LogPath() string      { return filepath.Join(a.Dir, "client.log") }
func (a *App) Config() Config       { a.mu.Lock(); defer a.mu.Unlock(); return a.cfg }
func (a *App) Status() Status       { a.mu.Lock(); t := a.tun; a.mu.Unlock(); return t.Status() }
func (a *App) Reconnect()           { a.mu.Lock(); t := a.tun; a.mu.Unlock(); t.Reconnect() }

// Start 读取配置、启动隧道与本地服务。返回是否首次运行（无配置）。
func (a *App) Start(ctx context.Context) (firstRun bool, err error) {
	a.ctx = ctx
	cfg, exists, err := LoadConfig(a.Dir)
	if err != nil {
		a.Log.Printf("config: %v", err)
	}
	a.cfg = cfg
	a.token = RandomToken()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return false, err
	}
	a.port = ln.Addr().(*net.TCPAddr).Port
	if err := WriteControlInfo(a.Dir, ControlInfo{Port: a.port, Token: a.token, PID: os.Getpid()}); err != nil {
		return false, err
	}
	go http.Serve(ln, a.handler())
	a.startTunnel()
	return !exists, nil
}

func (a *App) startTunnel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel
	a.tun = NewTunnel(a.cfg, &KnownHosts{Path: filepath.Join(a.Dir, "known_hosts")}, a.Log)
	a.tun.OnChange = func(s Status) {
		if a.OnStatus != nil {
			a.OnStatus(s)
		}
	}
	go a.tun.Run(ctx)
}

func (a *App) UIURL(page string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/ui/%s/%s", a.port, a.token, page)
}

// EnsureExtension 解压/更新嵌入的扩展。
func (a *App) EnsureExtension() error {
	if a.ExtFS == nil {
		return nil
	}
	return ExtractExtension(a.ExtFS, a.ExtensionDir())
}

// Backup：服务端备份需要会话令牌，托盘不持有令牌，因此只给出提示。
func (a *App) BackupHint() string {
	s := a.Status()
	if s.State != StateConnected {
		return T("not_conn")
	}
	if !s.Unlocked {
		return T("backup_lock")
	}
	return T("backup_tip")
}

func (a *App) handler() http.Handler {
	mux := http.NewServeMux()
	ctl := func(f func() any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.Header.Get("X-PS-Token") != a.token {
				http.Error(w, "forbidden", 403)
				return
			}
			writeJ(w, map[string]any{"ok": true, "status": f()})
		}
	}
	mux.HandleFunc("/ctl/status", ctl(func() any { return a.Status() }))
	mux.HandleFunc("/ctl/reconnect", ctl(func() any { a.Reconnect(); return a.Status() }))
	mux.HandleFunc("/ctl/guide", ctl(func() any { a.EnsureExtension(); OpenURL(a.UIURL("guide")); return a.Status() }))
	mux.HandleFunc("/ctl/setup", ctl(func() any { OpenURL(a.UIURL("setup")); return a.Status() }))

	pre := "/ui/" + a.token + "/"
	ui := func(f http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; form-action 'self'")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			if r.Method == http.MethodPost && r.Header.Get("Origin") != "" && r.Header.Get("Origin") != fmt.Sprintf("http://127.0.0.1:%d", a.port) {
				http.Error(w, "bad origin", 403)
				return
			}
			f(w, r)
		}
	}
	mux.HandleFunc(pre+"setup", ui(a.pageSetup))
	mux.HandleFunc(pre+"guide", ui(a.pageGuide))
	mux.HandleFunc(pre+"status", ui(func(w http.ResponseWriter, r *http.Request) { writeJ(w, a.Status()) }))
	mux.HandleFunc(pre+"open", ui(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		var err error
		switch b := r.URL.Query().Get("b"); b {
		case "chrome", "edge":
			err = OpenExtensionsPage(b)
		case "folder":
			err = OpenPath(a.ExtensionDir())
		}
		if err != nil {
			writeJ(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJ(w, map[string]any{"ok": true})
	}))
	mux.HandleFunc(pre+"passphrase", ui(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		a.mu.Lock()
		t := a.tun
		a.mu.Unlock()
		t.SetPassphrase(r.FormValue("passphrase"))
		http.Redirect(w, r, pre+"setup?saved=1", http.StatusSeeOther)
	}))
	return mux
}

func writeJ(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func atoiDef(s string, d int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return d
}

func (a *App) pageSetup(w http.ResponseWriter, r *http.Request) {
	msg, errMsg := "", ""
	if r.Method == http.MethodPost {
		c := a.Config()
		c.Host = r.FormValue("host")
		c.Port = atoiDef(r.FormValue("port"), 22)
		c.User = r.FormValue("user")
		c.KeyPath = r.FormValue("key")
		c.LocalPort = atoiDef(r.FormValue("lport"), 8787)
		c.RemotePort = atoiDef(r.FormValue("rport"), 8787)
		if err := c.Validate(); err != nil {
			errMsg = err.Error()
		} else if !fileExists(c.KeyPath) {
			errMsg = "找不到私钥文件 / key file not found: " + c.KeyPath
		} else if err := SaveConfig(a.Dir, c); err != nil {
			errMsg = err.Error()
		} else {
			a.mu.Lock()
			a.cfg = c
			a.mu.Unlock()
			a.Log.Printf("config saved (host set, key path set)")
			a.startTunnel()
			msg = "已保存，正在连接… / Saved, connecting…"
		}
	} else if r.URL.Query().Get("saved") == "1" {
		msg = "已提交口令，正在重连 / passphrase set, reconnecting"
	}
	render(w, setupTpl, map[string]any{"C": a.Config(), "Msg": msg, "Err": errMsg, "S": a.Status(), "Pre": "/ui/" + a.token + "/", "Dir": a.Dir})
}

func (a *App) pageGuide(w http.ResponseWriter, r *http.Request) {
	render(w, guideTpl, map[string]any{"Path": a.ExtensionDir(), "ID": ExtensionID, "Pre": "/ui/" + a.token + "/", "Ver": a.Version})
}

func render(w http.ResponseWriter, t *template.Template, d any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, d); err != nil {
		log.Printf("template: %v", err)
	}
}
