package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// 进程内 SSH 服务器（支持 direct-tcpip 与 keepalive），用于验证转发与断线重连。
type testSSHD struct {
	ln    net.Listener
	conns atomic.Int32
	live  chan ssh.Conn
}

func startSSHD(t *testing.T, hostKey ssh.Signer, clientPub ssh.PublicKey, addr string) *testSSHD {
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if string(k.Marshal()) == string(clientPub.Marshal()) {
			return nil, nil
		}
		return nil, fmt.Errorf("denied")
	}}
	cfg.AddHostKey(hostKey)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	s := &testSSHD{ln: ln, live: make(chan ssh.Conn, 10)}
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				s.conns.Add(1)
				s.live <- sc
				go func() {
					for r := range reqs {
						r.Reply(r.Type == "keepalive@openssh.com", nil)
					}
				}()
				for nch := range chans {
					if nch.ChannelType() != "direct-tcpip" {
						nch.Reject(ssh.UnknownChannelType, "no")
						continue
					}
					var p struct {
						Host string
						Port uint32
						OH   string
						OP   uint32
					}
					ssh.Unmarshal(nch.ExtraData(), &p)
					tc, err := net.Dial("tcp", net.JoinHostPort(p.Host, strconv.Itoa(int(p.Port))))
					if err != nil {
						nch.Reject(ssh.ConnectionFailed, err.Error())
						continue
					}
					ch, rq, _ := nch.Accept()
					go ssh.DiscardRequests(rq)
					go func() { io.Copy(ch, tc); ch.CloseWrite() }()
					go func() { io.Copy(tc, ch); tc.Close() }()
				}
			}()
		}
	}()
	return s
}

func freePort(t *testing.T) int {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestTunnelForwardAndReconnect(t *testing.T) {
	// 假的服务端 API
	api := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"initialized":true,"unlocked":false}`))
	})}
	al, _ := net.Listen("tcp", "127.0.0.1:0")
	go api.Serve(al)
	defer api.Close()
	remotePort := al.Addr().(*net.TCPAddr).Port

	_, hpriv, _ := ed25519.GenerateKey(rand.Reader)
	hs, _ := ssh.NewSignerFromKey(hpriv)
	cpub, cpriv, _ := ed25519.GenerateKey(rand.Reader)
	cp, _ := ssh.NewPublicKey(cpub)
	dir := t.TempDir()
	blk, _ := ssh.MarshalPrivateKey(cpriv, "")
	keyPath := filepath.Join(dir, "id.pem")
	os.WriteFile(keyPath, pem.EncodeToMemory(blk), 0o600)

	sshPort := freePort(t)
	addr := "127.0.0.1:" + strconv.Itoa(sshPort)
	srv := startSSHD(t, hs, cp, addr)

	cfg := Config{Host: "127.0.0.1", Port: sshPort, User: "u", KeyPath: keyPath, LocalPort: freePort(t), RemotePort: remotePort}
	tun := NewTunnel(cfg, &KnownHosts{Path: filepath.Join(dir, "kh")}, log.New(io.Discard, "", 0))
	tun.Backoff = Backoff{Min: 50 * time.Millisecond, Max: 200 * time.Millisecond}
	tun.HealthInterval, tun.KeepAlive = 200*time.Millisecond, 200*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tun.Run(ctx)

	waitFor(t, func() bool { s := tun.Status(); return s.State == StateConnected && s.APIOK })
	get := func() error {
		r, err := (&http.Client{Timeout: 3 * time.Second}).Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/status", cfg.LocalPort))
		if err != nil {
			return err
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		if len(b) == 0 {
			return fmt.Errorf("empty")
		}
		return nil
	}
	if err := get(); err != nil {
		t.Fatal("forward:", err)
	}
	// 服务器断开连接 → 应自动重连
	sc := <-srv.live
	sc.Close()
	waitFor(t, func() bool { return srv.conns.Load() >= 2 && tun.Status().State == StateConnected })
	waitFor(t, func() bool { return get() == nil })

	// 端口占用检测
	t2 := NewTunnel(cfg, &KnownHosts{Path: filepath.Join(dir, "kh")}, log.New(io.Discard, "", 0))
	t2.Backoff = Backoff{Min: time.Hour, Max: time.Hour}
	c2, cancel2 := context.WithCancel(context.Background())
	go t2.Run(c2)
	waitFor(t, func() bool { e := t2.Status().Error; return e != "" && containsAny(e, "已被占用") })
	cancel2()

	// 主机密钥变化 → 拒绝
	srv.ln.Close()
	_, hpriv2, _ := ed25519.GenerateKey(rand.Reader)
	hs2, _ := ssh.NewSignerFromKey(hpriv2)
	time.Sleep(100 * time.Millisecond)
	for _, c := range []chan ssh.Conn{srv.live} {
		select {
		case x := <-c:
			x.Close()
		default:
		}
	}
	startSSHD(t, hs2, cp, addr)
	tun.Reconnect()
	waitFor(t, func() bool { return containsAny(tun.Status().Error, "主机密钥已变化") })
}

func waitFor(t *testing.T, f func() bool) {
	t.Helper()
	dl := time.Now().Add(10 * time.Second)
	for time.Now().Before(dl) {
		if f() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timeout waiting for condition")
}

func TestTunnelFallbackHost(t *testing.T) {
	api := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"initialized":true,"unlocked":false}`))
	})}
	al, _ := net.Listen("tcp", "127.0.0.1:0")
	go api.Serve(al)
	defer api.Close()
	remotePort := al.Addr().(*net.TCPAddr).Port

	_, hpriv, _ := ed25519.GenerateKey(rand.Reader)
	hs, _ := ssh.NewSignerFromKey(hpriv)
	cpub, cpriv, _ := ed25519.GenerateKey(rand.Reader)
	cp, _ := ssh.NewPublicKey(cpub)
	dir := t.TempDir()
	blk, _ := ssh.MarshalPrivateKey(cpriv, "")
	keyPath := filepath.Join(dir, "id.pem")
	os.WriteFile(keyPath, pem.EncodeToMemory(blk), 0o600)

	goodPort := freePort(t)
	startSSHD(t, hs, cp, "127.0.0.1:"+strconv.Itoa(goodPort))
	badPort := freePort(t) // nothing listening

	cfg := Config{
		Host: "127.0.0.1", Port: badPort,
		FallbackHost: "127.0.0.1", FallbackPort: goodPort,
		User: "u", KeyPath: keyPath, LocalPort: freePort(t), RemotePort: remotePort,
	}
	tun := NewTunnel(cfg, &KnownHosts{Path: filepath.Join(dir, "kh")}, log.New(io.Discard, "", 0))
	tun.Backoff = Backoff{Min: 50 * time.Millisecond, Max: 200 * time.Millisecond}
	tun.HealthInterval, tun.KeepAlive = time.Hour, time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tun.Run(ctx)

	waitFor(t, func() bool {
		s := tun.Status()
		return s.State == StateConnected && s.APIOK && s.ViaFallback && s.ActiveHost == "127.0.0.1"
	})
}
