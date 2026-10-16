// Package extension 把 Chromium 扩展嵌入桌面客户端（打包扩展时会排除本文件）。
package extension

import "embed"

//go:embed manifest.json *.js *.css *.html _locales icons
var FS embed.FS
