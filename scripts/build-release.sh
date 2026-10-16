#!/usr/bin/env bash
# 交叉编译服务端/CLI 并打包，生成 SHA256SUMS
set -euo pipefail
cd "$(dirname "$0")/.."
VER="${VERSION:-v0.1.0-beta}"; OUT=dist; mkdir -p "$OUT"
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  os=${t%/*}; arch=${t#*/}; name="password-safe-$VER-$os-$arch"; d=$(mktemp -d)/$name; mkdir -p "$d"
  ext=""; [ "$os" = windows ] && ext=".exe"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X github.com/tjut-lfl/password-safe/internal/cli.Version=$VER" -o "$d/password-safe$ext" ./cmd/password-safe
  cp README.md LICENSE CHANGELOG.md "$d/"; [ "$os" = linux ] && cp deploy/password-safe.service "$d/"
  if [ "$os" = windows ]; then (cd "$(dirname "$d")" && zip -qr "$OLDPWD/$OUT/$name.zip" "$name"); else tar -C "$(dirname "$d")" -czf "$OUT/$name.tar.gz" "$name"; fi
done
bash scripts/build-extensions.sh
(cd "$OUT" && rm -f SHA256SUMS && sha256sum password-safe-* > SHA256SUMS && cat SHA256SUMS)
