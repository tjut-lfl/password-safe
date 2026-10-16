#!/usr/bin/env bash
# 打包浏览器扩展：chromium（Chrome/Edge/Brave/Opera/Vivaldi）与 firefox
set -euo pipefail
cd "$(dirname "$0")/.."
VER="${VERSION:-v0.2.0-beta}"
OUT=dist; mkdir -p "$OUT"; tmp=$(mktemp -d)
cp -r extension "$tmp/chromium"; rm -f "$tmp/chromium/README.md"
(cd "$tmp/chromium" && zip -qr "$OLDPWD/$OUT/password-safe-extension-chromium-$VER.zip" .)
cp -r extension "$tmp/firefox"; rm -f "$tmp/firefox/README.md"
python3 - "$tmp/firefox/manifest.json" <<'PY'
import json,sys
p=sys.argv[1];m=json.load(open(p))
m.pop("options_page",None); m["options_ui"]={"page":"options.html","open_in_tab":True}
m["background"]={"scripts":["background.js"]}
m.pop("version_name",None)
m["browser_specific_settings"]={"gecko":{"id":"password-safe@tjut-lfl","strict_min_version":"115.0","data_collection_permissions":{"required":["none"]}}}
json.dump(m,open(p,"w"),ensure_ascii=False,indent=2)
PY
(cd "$tmp/firefox" && zip -qr "$OLDPWD/$OUT/password-safe-extension-firefox-$VER.zip" .)
rm -rf "$tmp"; ls -l "$OUT"/password-safe-extension-*
