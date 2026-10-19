package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

type State string

const (
	StateDisconnected State = "disconnected"
	StateConnecting   State = "connecting"
	StateConnected    State = "connected"
)

// Status 是对外（托盘、原生消息）暴露的快照，不含任何秘密。
type Status struct {
	State       State     `json:"state"`
	Error       string    `json:"error,omitempty"`
	Server      string    `json:"server,omitempty"`
	ActiveHost  string    `json:"active_host,omitempty"`
	ViaFallback bool      `json:"via_fallback,omitempty"`
	LocalAddr   string    `json:"local_addr,omitempty"`
	APIOK       bool      `json:"api_ok"`
	Unlocked    bool      `json:"vault_unlocked"`
	Since       time.Time `json:"since"`
	NeedsPass   bool      `json:"needs_passphrase,omitempty"`
	Configured  bool      `json:"configured"`
}

// ErrNeedPassphrase：私钥已加密，需要在设置页输入口令（只保存在内存）。
var ErrNeedPassphrase = errors.New("私钥已加密，请在设置页输入口令 / key is encrypted, enter passphrase in setup page")

type Tunnel struct {
	Cfg        Config
	KnownHosts *KnownHosts
	Logger     *log.Logger
	OnChange   func(Status)
	// 测试可覆盖
	HealthInterval time.Duration
	KeepAlive      time.Duration
	Backoff        Backoff

	mu         sync.Mutex
	st         Status
	client     *ssh.Client
	passphrase []byte
	kick       chan struct{}
}

func NewTunnel(cfg Config, kh *KnownHosts, lg *log.Logger) *Tunnel {
	return &Tunnel{Cfg: cfg, KnownHosts: kh, Logger: lg, HealthInterval: 20 * time.Second, KeepAlive: 15 * time.Second,
		Backoff: Backoff{Min: time.Second, Max: time.Minute}, kick: make(chan struct{}, 1),
		st: Status{State: StateDisconnected, Since: time.Now()}}
}

func (t *Tunnel) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.st
	if s.ActiveHost != "" {
		s.Server = s.ActiveHost
	} else {
		s.Server = t.Cfg.Host
	}
	s.LocalAddr = net.JoinHostPort("127.0.0.1", strconv.Itoa(t.Cfg.LocalPort))
	s.Configured = t.Cfg.Validate() == nil
	return s
}

func (t *Tunnel) set(f func(*Status)) {
	t.mu.Lock()
	old := t.st
	f(&t.st)
	if t.st.State != old.State {
		t.st.Since = time.Now()
	}
	cb := t.OnChange
	t.mu.Unlock()
	if cb != nil {
		cb(t.Status())
	}
}

// SetPassphrase 设置加密私钥口令（仅内存）并立即重连。
func (t *Tunnel) SetPassphrase(p string) { t.mu.Lock(); t.passphrase = []byte(p); t.mu.Unlock(); t.Reconnect() }

// Reconnect 断开当前连接并立即重试（跳过退避）。
func (t *Tunnel) Reconnect() {
	t.mu.Lock()
	c := t.client
	t.mu.Unlock()
	if c != nil {
		c.Close()
	}
	select {
	case t.kick <- struct{}{}:
	default:
	}
}

func (t *Tunnel) wait(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-t.kick:
	case <-time.After(d):
	}
}

func (t *Tunnel) signer() (ssh.Signer, error) {
	b, err := os.ReadFile(t.Cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("读取私钥失败 / cannot read key: %w", err)
	}
	s, err := ssh.ParsePrivateKey(b)
	var pe *ssh.PassphraseMissingError
	if errors.As(err, &pe) {
		t.mu.Lock()
		p := t.passphrase
		t.mu.Unlock()
		if len(p) == 0 {
			return nil, ErrNeedPassphrase
		}
		s, err = ssh.ParsePrivateKeyWithPassphrase(b, p)
	}
	if err != nil {
		return nil, fmt.Errorf("私钥无法解析 / bad key: %w", err)
	}
	return s, nil
}

// dialEndpoint 描述一次 SSH 拨号目标。
type dialEndpoint struct {
	Host        string
	Port        int
	ViaFallback bool
	Label       string // "primary" | "fallback"
}

// endpoints 返回拨号顺序：先主主机，再备用（若已配置且与主不同）。
func (c Config) endpoints() []dialEndpoint {
	out := []dialEndpoint{{Host: strings.TrimSpace(c.Host), Port: c.Port, ViaFallback: false, Label: "primary"}}
	fh := strings.TrimSpace(c.FallbackHost)
	if fh == "" {
		return out
	}
	fp := c.FallbackPortOrDefault()
	if fh == out[0].Host && fp == out[0].Port {
		return out
	}
	return append(out, dialEndpoint{Host: fh, Port: fp, ViaFallback: true, Label: "fallback"})
}

func (t *Tunnel) dialOne(ep dialEndpoint, s ssh.Signer) (*ssh.Client, error) {
	cc := &ssh.ClientConfig{User: t.Cfg.User, Auth: []ssh.AuthMethod{ssh.PublicKeys(s)},
		HostKeyCallback: t.KnownHosts.Callback(), Timeout: 15 * time.Second}
	addr := net.JoinHostPort(ep.Host, strconv.Itoa(ep.Port))
	t.Logger.Printf("ssh dialing %s (%s)", addr, ep.Label)
	return ssh.Dial("tcp", addr, cc)
}

// dial 先试主主机，失败且可重试时再试备用主机。返回 client、实际主机、是否经备用。
func (t *Tunnel) dial() (*ssh.Client, string, bool, error) {
	s, err := t.signer()
	if err != nil {
		return nil, "", false, err
	}
	eps := t.Cfg.endpoints()
	var last error
	for i, ep := range eps {
		c, err := t.dialOne(ep, s)
		if err == nil {
			return c, ep.Host, ep.ViaFallback, nil
		}
		last = err
		t.Logger.Printf("ssh dial %s (%s) failed: %v", net.JoinHostPort(ep.Host, strconv.Itoa(ep.Port)), ep.Label, err)
		if errors.Is(err, ErrNeedPassphrase) {
			return nil, "", false, err
		}
		var mm *HostKeyMismatchError
		if errors.As(err, &mm) {
			// 主机密钥不匹配需要用户处理；若还有备用主机可继续试另一端点
			if i+1 < len(eps) {
				continue
			}
			return nil, "", false, err
		}
	}
	if last == nil {
		last = errors.New("no endpoints")
	}
	return nil, "", false, last
}

func (t *Tunnel) remoteAddr() string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(t.Cfg.RemotePort)) }

// IsAddrInUse 判断监听失败是否因端口占用。
func IsAddrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE) || (err != nil && containsAny(err.Error(), "address already in use", "Only one usage of each socket address"))
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if len(s) >= len(x) {
			for i := 0; i+len(x) <= len(s); i++ {
				if s[i:i+len(x)] == x {
					return true
				}
			}
		}
	}
	return false
}

// Run 阻塞运行直到 ctx 结束：保持本地监听，断线指数退避重连。
func (t *Tunnel) Run(ctx context.Context) {
	var ln net.Listener
	for ctx.Err() == nil {
		if err := t.Cfg.Validate(); err != nil {
			t.set(func(s *Status) { s.State, s.Error, s.ActiveHost, s.ViaFallback = StateDisconnected, "未配置 / not configured: "+err.Error(), "", false })
			t.wait(ctx, 30*time.Second)
			continue
		}
		var err error
		ln, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(t.Cfg.LocalPort)))
		if err != nil {
			msg := err.Error()
			if IsAddrInUse(err) {
				msg = fmt.Sprintf("本地端口 %d 已被占用（是否还开着手动的 ssh -L 隧道？）/ local port %d is in use", t.Cfg.LocalPort, t.Cfg.LocalPort)
			}
			t.Logger.Printf("listen: %s", msg)
			t.set(func(s *Status) { s.State, s.Error, s.ActiveHost, s.ViaFallback = StateDisconnected, msg, "", false })
			t.wait(ctx, t.Backoff.Next())
			continue
		}
		break
	}
	if ln == nil {
		return
	}
	go func() { <-ctx.Done(); ln.Close() }()
	go t.acceptLoop(ln)
	t.Backoff.Reset()
	for ctx.Err() == nil {
		t.set(func(s *Status) { s.State, s.APIOK, s.ActiveHost, s.ViaFallback = StateConnecting, false, "", false })
		c, host, viaFB, err := t.dial()
		if err != nil {
			t.Logger.Printf("ssh connect: %v", err)
			needs := errors.Is(err, ErrNeedPassphrase)
			t.set(func(s *Status) { s.State, s.Error, s.NeedsPass, s.ActiveHost, s.ViaFallback = StateDisconnected, err.Error(), needs, "", false })
			d := t.Backoff.Next()
			var mm *HostKeyMismatchError
			if errors.As(err, &mm) || needs {
				d = 10 * time.Minute // 等用户处理，或手动重连
			}
			t.wait(ctx, d)
			continue
		}
		t.mu.Lock()
		t.client = c
		t.mu.Unlock()
		via := "primary"
		if viaFB {
			via = "fallback"
		}
		t.Logger.Printf("ssh connected to %s (%s)", host, via)
		t.set(func(s *Status) {
			s.State, s.Error, s.NeedsPass = StateConnected, "", false
			s.ActiveHost, s.ViaFallback = host, viaFB
		})
		t.Backoff.Reset()
		t.supervise(ctx, c)
		c.Close()
		t.mu.Lock()
		t.client = nil
		t.mu.Unlock()
		if ctx.Err() == nil {
			t.Logger.Printf("ssh disconnected; reconnecting")
			t.set(func(s *Status) { s.State, s.APIOK, s.ActiveHost, s.ViaFallback = StateDisconnected, false, "", false })
			t.wait(ctx, t.Backoff.Next())
		}
	}
	t.set(func(s *Status) { s.State, s.ActiveHost, s.ViaFallback = StateDisconnected, "", false })
}

// supervise：保活 + 健康检查，连接失效时返回。
func (t *Tunnel) supervise(ctx context.Context, c *ssh.Client) {
	done := make(chan struct{})
	go func() { c.Wait(); close(done) }()
	t.health(c)
	ka := time.NewTicker(t.KeepAlive)
	hc := time.NewTicker(t.HealthInterval)
	defer ka.Stop()
	defer hc.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ka.C:
			res := make(chan error, 1)
			go func() { _, _, err := c.SendRequest("keepalive@openssh.com", true, nil); res <- err }()
			select {
			case err := <-res:
				if err != nil {
					t.Logger.Printf("keepalive failed: %v", err)
					return
				}
			case <-time.After(10 * time.Second):
				t.Logger.Printf("keepalive timeout")
				return
			}
		case <-hc.C:
			t.health(c)
		}
	}
}

func (t *Tunnel) health(c *ssh.Client) {
	hc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return c.Dial("tcp", t.remoteAddr()) }}}
	ok, unlocked, errMsg := false, false, ""
	resp, err := hc.Get("http://" + t.remoteAddr() + "/api/v1/status")
	if err != nil {
		errMsg = "隧道已连上，但服务端 password-safe 未响应 / tunnel up but server API unreachable"
	} else {
		var v struct {
			Unlocked bool `json:"unlocked"`
		}
		ok = resp.StatusCode == 200
		_ = decodeJSON(resp.Body, &v)
		resp.Body.Close()
		unlocked = v.Unlocked
	}
	t.set(func(s *Status) { s.APIOK, s.Unlocked, s.Error = ok, unlocked, errMsg })
}

// APIRequest 通过隧道调用服务端（托盘“立即备份”等）。
func (t *Tunnel) APIRequest(method, path string, body io.Reader, hdr map[string]string) (*http.Response, error) {
	t.mu.Lock()
	c := t.client
	t.mu.Unlock()
	if c == nil {
		return nil, errors.New("隧道未连接 / tunnel not connected")
	}
	hc := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return c.Dial("tcp", t.remoteAddr()) }}}
	req, err := http.NewRequest(method, "http://"+t.remoteAddr()+path, body)
	if err != nil {
		return nil, err
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return hc.Do(req)
}

func (t *Tunnel) acceptLoop(ln net.Listener) {
	for {
		lc, err := ln.Accept()
		if err != nil {
			return
		}
		go t.forward(lc)
	}
}

func (t *Tunnel) forward(lc net.Conn) {
	defer lc.Close()
	t.mu.Lock()
	c := t.client
	t.mu.Unlock()
	if c == nil {
		return
	}
	rc, err := c.Dial("tcp", t.remoteAddr())
	if err != nil {
		t.Logger.Printf("forward dial: %v", err)
		return
	}
	defer rc.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(rc, lc); done <- struct{}{} }()
	go func() { io.Copy(lc, rc); done <- struct{}{} }()
	<-done
}
