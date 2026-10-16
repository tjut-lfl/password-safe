//go:build windows || linux

package main

import (
	"context"

	"fyne.io/systray"
	"github.com/tjut-lfl/password-safe/internal/client"
)

func runTray(app *client.App, cancel context.CancelFunc) {
	systray.Run(func() { onReady(app) }, func() { cancel() })
}

func onReady(app *client.App) {
	T := client.T
	systray.SetIcon(iconGray)
	systray.SetTitle("")
	systray.SetTooltip("Password Safe")
	mStatus := systray.AddMenuItem(T("status")+": "+T("disconnected"), "")
	mStatus.Disable()
	mInfo := systray.AddMenuItem("", "")
	mInfo.Disable()
	mInfo.Hide()
	systray.AddSeparator()
	mRe := systray.AddMenuItem(T("reconnect"), "")
	mGuide := systray.AddMenuItem(T("guide"), "")
	mSetup := systray.AddMenuItem(T("setup"), "")
	mBackup := systray.AddMenuItem(T("backup"), "")
	systray.AddSeparator()
	mCfg := systray.AddMenuItem(T("config"), "")
	mLog := systray.AddMenuItem(T("logs"), "")
	mAuto := systray.AddMenuItemCheckbox(T("autostart"), "", client.AutostartEnabled())
	systray.AddSeparator()
	mQuit := systray.AddMenuItem(T("quit"), "")

	upd := func(s client.Status) {
		label, icon := T("disconnected"), iconGray
		switch {
		case s.State == client.StateConnected && s.APIOK:
			label, icon = T("connected"), iconGreen
			if s.Unlocked {
				label += " · " + T("unlocked")
			} else {
				label += " · " + T("locked")
			}
		case s.State == client.StateConnected:
			label, icon = T("api_down"), iconYellow
		case s.State == client.StateConnecting:
			label, icon = T("connecting"), iconYellow
		case s.Error != "":
			icon = iconRed
		}
		systray.SetIcon(icon)
		mStatus.SetTitle(T("status") + ": " + label)
		tip := "Password Safe — " + label
		if s.Error != "" {
			tip += "\n" + s.Error
		}
		if len(tip) > 120 {
			tip = tip[:120]
		}
		systray.SetTooltip(tip)
	}
	app.OnStatus = upd
	upd(app.Status())
	tip := func(msg string) { mInfo.SetTitle("ⓘ " + msg); mInfo.Show(); systray.SetTooltip(msg) }

	go func() {
		for {
			select {
			case <-mRe.ClickedCh:
				app.Reconnect()
			case <-mGuide.ClickedCh:
				app.EnsureExtension()
				client.OpenURL(app.UIURL("guide"))
			case <-mSetup.ClickedCh:
				client.OpenURL(app.UIURL("setup"))
			case <-mBackup.ClickedCh:
				tip(app.BackupHint())
			case <-mCfg.ClickedCh:
				if app.Config().Validate() != nil {
					client.OpenURL(app.UIURL("setup"))
				} else {
					client.OpenText(client.ConfigPath(app.Dir))
				}
			case <-mLog.ClickedCh:
				client.OpenText(app.LogPath())
			case <-mAuto.ClickedCh:
				on := !mAuto.Checked()
				if err := client.SetAutostart(on, app.Exe); err != nil {
					tip(err.Error())
				} else if on {
					mAuto.Check()
				} else {
					mAuto.Uncheck()
				}
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}
