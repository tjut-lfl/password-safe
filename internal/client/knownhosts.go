package client

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

// HostKeyMismatchError：服务器指纹与首次连接记录不一致。
type HostKeyMismatchError struct {
	Host, Want, Got string
}

func (e *HostKeyMismatchError) Error() string {
	return fmt.Sprintf("服务器 %s 的主机密钥已变化（记录 %s，实际 %s），已拒绝连接。若确认服务器重装过，请删除 known_hosts 中对应行。/ host key changed, refusing to connect", e.Host, e.Want, e.Got)
}

// TOFU：首次连接记录指纹，之后不一致即拒绝。文件格式：<host:port> <SHA256:...>
type KnownHosts struct {
	Path string
	mu   sync.Mutex
}

func (k *KnownHosts) load() (map[string]string, error) {
	m := map[string]string{}
	f, err := os.Open(k.Path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := strings.Fields(line)
		if len(p) >= 2 {
			m[p[0]] = p[1]
		}
	}
	return m, sc.Err()
}

func (k *KnownHosts) Callback() ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		return k.Check(hostname, key)
	}
}

func (k *KnownHosts) Check(hostport string, key ssh.PublicKey) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	fp := ssh.FingerprintSHA256(key)
	m, err := k.load()
	if err != nil {
		return err
	}
	if want, ok := m[hostport]; ok {
		if want != fp {
			return &HostKeyMismatchError{Host: hostport, Want: want, Got: fp}
		}
		return nil
	}
	f, err := os.OpenFile(k.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s %s %s\n", hostport, fp, key.Type())
	return err
}
