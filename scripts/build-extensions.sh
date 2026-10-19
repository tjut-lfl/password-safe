#!/usr/bin/env bash
# 打包浏览器扩展：chromium（Chrome/Edge/Brave/Opera/Vivaldi）与 firefox
set -euo pipefail
cd "$(dirname "$0")/.."
VER="${VERSION:-v0.3.4-beta}"
OUT=dist; mkdir -p "$OUT"; tmp=$(mktemp -d)
cp -r extension "$tmp/chromium"; rm -rf "$tmp/chromium/README.md" "$tmp/chromium/README.zh-CN.md" "$tmp/chromium/test" "$tmp/chromium/embed.go"
(cd "$tmp/chromium" && zip -qr "$OLDPWD/$OUT/password-safe-extension-chromium-$VER.zip" .)
cp -r extension "$tmp/firefox"; rm -rf "$tmp/firefox/README.md" "$tmp/firefox/README.zh-CN.md" "$tmp/firefox/test" "$tmp/firefox/embed.go"
python3 - "$tmp/firefox/manifest.json" <<'PY'
import json,sys
p=sys.argv[1];m=json.load(open(p))
m.pop("options_page",None); m["options_ui"]={"page":"options.html","open_in_tab":True}
m["background"]={"scripts":["background.js"]}
m.pop("version_name",None); m.pop("key",None)
m["permissions"]=[x for x in m["permissions"] if x!="nativeMessaging"]
m["browser_specific_settings"]={"gecko":{"id":"password-safe@tjut-lfl","strict_min_version":"115.0","data_collection_permissions":{"required":["none"]}}}
json.dump(m,open(p,"w"),ensure_ascii=False,indent=2)
PY
(cd "$tmp/firefox" && zip -qr "$OLDPWD/$OUT/password-safe-extension-firefox-$VER.zip" .)
rm -rf "$tmp"; ls -l "$OUT"/password-safe-extension-*
