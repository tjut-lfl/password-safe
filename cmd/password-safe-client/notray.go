//go:build !windows && !linux

package main

import (
	"context"
	"fmt"

	"github.com/tjut-lfl/password-safe/internal/client"
)

// macOS 托盘需要 cgo；交叉编译版本以无托盘模式运行，用设置页管理。
func runTray(app *client.App, cancel context.CancelFunc) {
	app.OnStatus = func(s client.Status) { app.Log.Printf("status: %s %s", s.State, s.Error) }
	fmt.Println("tray not available in this build; setup page:", app.UIURL("setup"))
	select {}
}
