//go:build !windows

package client

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func platformLang() string { return "" }

func opener() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	return "xdg-open"
}
func OpenURL(u string) error  { return exec.Command(opener(), u).Start() }
func OpenPath(p string) error { return exec.Command(opener(), p).Start() }
func OpenText(p string) error { return OpenPath(p) }

func FindBrowser(name string) string {
	cands := map[string][]string{
		"chrome": {"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"},
		"edge":   {"microsoft-edge", "microsoft-edge-stable", "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"},
	}[name]
	for _, c := range cands {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

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

func autostartFile() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "autostart", "password-safe-client.desktop")
}

func SetAutostart(on bool, exe string) error {
	if runtime.GOOS != "linux" {
		return errors.New("autostart not supported on this OS")
	}
	if !on {
		err := os.Remove(autostartFile())
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	os.MkdirAll(filepath.Dir(autostartFile()), 0o755)
	return os.WriteFile(autostartFile(), []byte("[Desktop Entry]\nType=Application\nName=Password Safe Client\nExec=\""+exe+"\"\nX-GNOME-Autostart-enabled=true\n"), 0o644)
}

func AutostartEnabled() bool { return fileExists(autostartFile()) }

func nmDirs() []string {
	h, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		b := filepath.Join(h, "Library", "Application Support")
		return []string{filepath.Join(b, "Google", "Chrome", "NativeMessagingHosts"), filepath.Join(b, "Microsoft Edge", "NativeMessagingHosts"), filepath.Join(b, "Chromium", "NativeMessagingHosts")}
	}
	b := filepath.Join(h, ".config")
	return []string{filepath.Join(b, "google-chrome", "NativeMessagingHosts"), filepath.Join(b, "microsoft-edge", "NativeMessagingHosts"), filepath.Join(b, "chromium", "NativeMessagingHosts")}
}

func RegisterNativeHost(cfgDir, exe string) error {
	for _, d := range nmDirs() {
		if _, err := WriteNativeManifest(d, exe); err != nil {
			return err
		}
	}
	return nil
}

func UnregisterNativeHost(cfgDir string) error {
	for _, d := range nmDirs() {
		os.Remove(filepath.Join(d, NativeHostName+".json"))
	}
	return nil
}
