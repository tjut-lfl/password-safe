// Package clipboard 通过系统剪贴板工具复制文本，并在一段时间后自动清空。
package clipboard

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DefaultClearAfter 默认自动清空时间。
const DefaultClearAfter = 30 * time.Second

// Copy 将 text 写入剪贴板。优先 wl-copy（Wayland），其次 xclip / xsel。
func Copy(text string) error {
	type try struct {
		name string
		args []string
	}
	candidates := []try{
		{"wl-copy", nil},
		{"xclip", []string{"-selection", "clipboard"}},
		{"xsel", []string{"--clipboard", "--input"}},
	}
	var tried []string
	for _, c := range candidates {
		path, err := exec.LookPath(c.name)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, c.args...)
		cmd.Stdin = strings.NewReader(text)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			tried = append(tried, fmt.Sprintf("%s(%v)", c.name, err))
			continue
		}
		return nil
	}
	if len(tried) == 0 {
		return fmt.Errorf("未找到剪贴板工具，请安装 wl-clipboard、xclip 或 xsel")
	}
	return fmt.Errorf("剪贴板写入失败: %s", strings.Join(tried, "; "))
}

// Clear 清空剪贴板。
func Clear() error {
	return Copy("")
}

// CopyThenClear 复制后在后台等待 d 再清空。
func CopyThenClear(text string, d time.Duration) error {
	if d <= 0 {
		d = DefaultClearAfter
	}
	if err := Copy(text); err != nil {
		return err
	}
	go func() {
		time.Sleep(d)
		_ = Clear()
	}()
	return nil
}

// Available 返回是否检测到可用剪贴板工具。
func Available() bool {
	for _, name := range []string{"wl-copy", "xclip", "xsel"} {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}

// Hint 安装提示。
func Hint() string {
	return "Ubuntu: sudo apt install wl-clipboard  或  sudo apt install xclip"
}
