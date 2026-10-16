// password-safe-client：桌面托盘客户端，维持 SSH 隧道、注册原生消息、引导安装浏览器插件。
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/tjut-lfl/password-safe/extension"
	"github.com/tjut-lfl/password-safe/internal/client"
)

var Version = "dev"

func main() {
	dir := client.ConfigDir()
	os.MkdirAll(dir, 0o700)
	// 原生消息模式：浏览器启动本程序，参数含 chrome-extension://<id>/
	if client.IsNativeHostInvocation(os.Args[1:]) {
		if err := client.RunNativeHost(dir, os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		return
	}
	install := flag.Bool("install", false, "注册原生消息主机并开启开机自启 / register native host + autostart")
	uninstall := flag.Bool("uninstall", false, "移除注册表项与开机自启 / remove registration and autostart")
	headless := flag.Bool("headless", false, "不显示托盘，仅运行隧道（调试/服务器）/ no tray")
	ver := flag.Bool("version", false, "print version")
	flag.Parse()
	exe, _ := os.Executable()
	exe, _ = filepath.Abs(exe)
	switch {
	case *ver:
		fmt.Println(Version)
		return
	case *install:
		must(client.RegisterNativeHost(dir, exe))
		must(client.SetAutostart(true, exe))
		fmt.Println("installed: native messaging host " + client.NativeHostName + " + autostart")
		return
	case *uninstall:
		must(client.UnregisterNativeHost(dir))
		must(client.SetAutostart(false, exe))
		fmt.Println("uninstalled")
		return
	}

	lw := &client.RotatingFile{Path: filepath.Join(dir, "client.log"), MaxBytes: 1 << 20, Keep: 3}
	lg := log.New(lw, "", log.LstdFlags)
	lg.Printf("password-safe-client %s starting", Version)

	// 单实例：已有实例在运行时，让它打开设置/引导页后退出
	if _, err := client.CallControl(dir, "status"); err == nil {
		client.CallControl(dir, "guide")
		return
	}
	ext, _ := fs.Sub(extension.FS, ".")
	app := &client.App{Dir: dir, DataDir: client.DataDir(), Exe: exe, Version: Version, ExtFS: ext, Log: lg}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, err := app.Start(ctx)
	if err != nil {
		lg.Printf("start: %v", err)
		os.Exit(1)
	}
	// 原生消息注册放在每次启动（exe 路径可能变化）；失败只记日志
	if err := client.RegisterNativeHost(dir, exe); err != nil {
		lg.Printf("register native host: %v", err)
	}
	if err := app.EnsureExtension(); err != nil {
		lg.Printf("extract extension: %v", err)
	}
	if first {
		client.OpenURL(app.UIURL("setup"))
		client.OpenURL(app.UIURL("guide"))
	}
	if *headless {
		app.OnStatus = func(s client.Status) { lg.Printf("status: %s %s", s.State, s.Error) }
		fmt.Println("headless; setup:", app.UIURL("setup"))
		select {}
	}
	runTray(app, cancel)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
