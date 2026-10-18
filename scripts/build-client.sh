#!/usr/bin/env bash
# 交叉编译桌面客户端：Windows（托盘，GUI 子系统）、Linux（托盘）、macOS（无托盘模式）
set -euo pipefail
cd "$(dirname "$0")/.."
VER="${VERSION:-v0.3.2-beta}"; OUT=dist; mkdir -p "$OUT"
LD="-s -w -X main.Version=$VER"
for t in windows/amd64 windows/arm64 linux/amd64 darwin/amd64 darwin/arm64; do
  os=${t%/*}; arch=${t#*/}; name="password-safe-client-$VER-$os-$arch"; d=$(mktemp -d)/$name; mkdir -p "$d"
  ext=""; ld="$LD"; [ "$os" = windows ] && { ext=".exe"; ld="$LD -H windowsgui"; }
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$ld" -o "$d/password-safe-client$ext" ./cmd/password-safe-client
  cp cmd/password-safe-client/README.md "$d/README.md"; cp cmd/password-safe-client/README.zh-CN.md "$d/README.zh-CN.md"; cp LICENSE "$d/"
  if [ "$os" = windows ]; then (cd "$(dirname "$d")" && zip -qr "$OLDPWD/$OUT/$name.zip" "$name"); else tar -C "$(dirname "$d")" -czf "$OUT/$name.tar.gz" "$name"; fi
done
ls -l "$OUT"/password-safe-client-*
