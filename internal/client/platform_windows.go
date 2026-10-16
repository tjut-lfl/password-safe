//go:build windows

package client

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const runName = "PasswordSafeClient"

var nmKeys = []string{
	`Software\Google\Chrome\NativeMessagingHosts\` + NativeHostName,
	`Software\Microsoft\Edge\NativeMessagingHosts\` + NativeHostName,
}

func platformLang() string {
	l, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err == nil && len(l) > 0 {
		return l[0]
	}
	return ""
}

func hidden(c *exec.Cmd) *exec.Cmd {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return c
}

// OpenURL / OpenPath 用默认程序打开。
func OpenURL(u string) error  { return hidden(exec.Command("rundll32", "url.dll,FileProtocolHandler", u)).Start() }
func OpenPath(p string) error { return hidden(exec.Command("rundll32", "url.dll,FileProtocolHandler", p)).Start() }
func OpenText(p string) error { return exec.Command("notepad.exe", p).Start() }

// FindBrowser 通过 App Paths 注册表与常见路径查找 chrome.exe / msedge.exe。
func FindBrowser(name string) string {
	exe := map[string]string{"chrome": "chrome.exe", "edge": "msedge.exe"}[name]
	if exe == "" {
		return ""
	}
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		k, err := registry.OpenKey(root, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\`+exe, registry.QUERY_VALUE)
		if err == nil {
			v, _, err := k.GetStringValue("")
			k.Close()
			if err == nil && fileExists(v) {
				return v
			}
		}
	}
	var rel string
	if name == "chrome" {
		rel = `Google\Chrome\Application\chrome.exe`
	} else {
		rel = `Microsoft\Edge\Application\msedge.exe`
	}
	for _, env := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)", "LOCALAPPDATA"} {
		if b := os.Getenv(env); b != "" {
			if p := filepath.Join(b, rel); fileExists(p) {
				return p
			}
		}
	}
	return ""
}

// OpenExtensionsPage 用浏览器可执行文件打开 chrome://extensions（URL 协议无法直接打开）。
func OpenExtensionsPage(name string) error {
	p := FindBrowser(name)
	if p == "" {
		return fmt.Errorf("未找到 %s / browser not found", name)
	}
	u := "chrome://extensions/"
	if name == "edge" {
		u = "edge://extensions/"
	}
	return exec.Command(p, u).Start()
}

func SetAutostart(on bool, exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if on {
		return k.SetStringValue(runName, `"`+exe+`"`)
	}
	if err := k.DeleteValue(runName); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

func AutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runName)
	return err == nil
}

// RegisterNativeHost 写清单并在 HKCU 下为 Chrome 与 Edge 注册。
func RegisterNativeHost(cfgDir, exe string) error {
	mp, err := WriteNativeManifest(cfgDir, exe)
	if err != nil {
		return err
	}
	for _, kp := range nmKeys {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, kp, registry.SET_VALUE)
		if err != nil {
			return err
		}
		err = k.SetStringValue("", mp)
		k.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func UnregisterNativeHost(cfgDir string) error {
	for _, kp := range nmKeys {
		if err := registry.DeleteKey(registry.CURRENT_USER, kp); err != nil && err != registry.ErrNotExist {
			return err
		}
	}
	os.Remove(filepath.Join(cfgDir, NativeHostName+".json"))
	return nil
}
