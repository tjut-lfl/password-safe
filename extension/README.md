# Password Safe browser extension (Chrome / Edge / Firefox)

**English** | [简体中文](README.zh-CN.md)

This extension is the browser client of the `password-safe` service:
- Autofill on login pages (🔒 icon on the right of the field, or click an entry in the toolbar popup)
- One-click strong password on sign-up / change-password pages, filling both "password" and "confirm password"
- **Multi-step login** (username page first, password page next — Google, Microsoft, AWS, Apple…): pick an entry on the username page to fill only the username; the extension remembers the choice for that tab (memory only, expires after 5 minutes, no password stored). When the password field appears (same page, or after navigating to another subdomain of the same site) the entry's password is filled automatically, or pick it from 🔒
- After a form is submitted it asks "Save to Password Safe?" and only creates/updates the entry **after you confirm**
- The master password is only used to unlock; the session token lives in browser memory (`chrome.storage.session`), is gone when the browser closes, and auto-locks after 6 hours

```
Extension ──http──▶ 127.0.0.1:8787 (local) ══SSH tunnel══▶ 127.0.0.1:8787 on the server (password-safe serve)
```
The server always listens on `127.0.0.1` only. **Do not** open port 8787 in your cloud firewall/security group; only 22.

---

## 1. Server: run `serve` with systemd

`serve` starts **without** the master password, in the locked state; the extension (or curl) unlocks it via `/api/v1/unlock`, so it is safe to run under systemd.

1. SSH to the server and initialize the vault (once):
   ```bash
   password-safe init
   ```
2. Create a user service:
   ```bash
   mkdir -p ~/.config/systemd/user
   cat > ~/.config/systemd/user/password-safe.service <<'UNIT'
   [Unit]
   Description=Password Safe API (localhost only)

   [Service]
   ExecStart=/usr/local/bin/password-safe serve --vault %h/.password-safe/vault.dat --addr 127.0.0.1:8787
   Restart=on-failure
   RestartSec=3
   NoNewPrivileges=true
   PrivateTmp=true

   [Install]
   WantedBy=default.target
   UNIT
   systemctl --user daemon-reload
   systemctl --user enable --now password-safe
   sudo loginctl enable-linger ubuntu      # keep running after you log out and after reboot
   systemctl --user status password-safe
   curl -s http://127.0.0.1:8787/health     # should return {"status":"ok",...}
   ```
   Logs: `journalctl --user -u password-safe -f`

> Note: the server has a single session token at a time. Unlocking with curl or from another computer invalidates the current extension session; just unlock again. "Lock" locks the whole vault on the server.

---

## 2. Your computer: open the SSH tunnel

Replace `key.pem` with your key path. Once the tunnel is up, local `http://127.0.0.1:8787` is the server's service.

### Windows (PowerShell, built-in OpenSSH)
```powershell
ssh -i C:\Users\you\key.pem -N -L 8787:127.0.0.1:8787 -o ServerAliveInterval=30 -o ExitOnForwardFailure=yes ubuntu@<your-server-ip>
```
If the key permission check fails: `icacls key.pem /inheritance:r /grant:r "$($env:USERNAME):R"`

Keep it running (auto-reconnect): create `tunnel.ps1`
```powershell
while ($true) {
  ssh -i C:\Users\you\key.pem -N -L 8787:127.0.0.1:8787 -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -o ExitOnForwardFailure=yes ubuntu@<your-server-ip>
  Start-Sleep 5
}
```
Then Task Scheduler → Create Task → trigger "At log on" → action `powershell -WindowStyle Hidden -File C:\path\tunnel.ps1`.

### macOS / Linux
```bash
chmod 600 ~/key.pem
ssh -i ~/key.pem -N -L 8787:127.0.0.1:8787 -o ServerAliveInterval=30 -o ExitOnForwardFailure=yes ubuntu@<your-server-ip>
```
Keep it running with autossh (`brew install autossh` / `sudo apt install autossh`):
```bash
autossh -M 0 -f -N -i ~/key.pem -L 8787:127.0.0.1:8787 \
  -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -o ExitOnForwardFailure=yes ubuntu@<your-server-ip>
```
On Linux this can be a systemd user service (`ExecStart=/usr/bin/autossh -M 0 -N ...`, without `-f`); on macOS use launchd or a login item.

Simplify with `~/.ssh/config`:
```
Host vault
  HostName <your-server-ip>
  User ubuntu
  IdentityFile ~/key.pem
  LocalForward 8787 127.0.0.1:8787
  ServerAliveInterval 30
  ExitOnForwardFailure yes
```
Then just `ssh -N vault`.

If local port 8787 is taken, use `-L 18787:127.0.0.1:8787` and set `http://127.0.0.1:18787` in the extension settings.

---

## 3. Install the extension (load unpacked)

1. Unzip the extension zip into a permanent folder (don't delete it later)
2. **Chrome**: open `chrome://extensions` → enable "Developer mode" (top right) → "Load unpacked" → choose the folder
3. **Edge**: open `edge://extensions` → enable "Developer mode" (left) → "Load unpacked" → choose the folder
4. Pin the icon to the toolbar; right-click → "Options" → "Test connection" should say success

---

## 4. Usage

- **Unlock**: click the toolbar icon, enter the master password → "Unlock"
- **Log in**: click 🔒 on the right of the password field → choose an account; or click an entry under "Current site" in the popup
- **Multi-step login**: on the username page, click 🔒 next to the username/email field (or the entry in the popup) → only the username is filled; press "Next" and the password is filled on the next step. In the popup, the remembered entry is listed first
- **Search**: type in the popup search box; click a result to fill the current page
- **Sign up**: on a sign-up page click 🔒 → "Generate strong password" (server-generated, or local secure random if the server is unreachable); both password fields are filled
- **Save**: after submitting a login/sign-up form, "Save to Password Safe?" appears at the top right; click "Save" to write it. The entry is named after the domain (`domain/username` when a site has several accounts); an existing same-site same-username entry is "Updated". For multi-step logins the username typed on step 1 is combined with the step-2 password
- **Lock**: click "Lock" in the popup; auto-locks after 6 hours

## 5. Security notes & limits
- No remote code, passwords are never logged; only the background script talks to the server, pages never see the token
- Plain HTTP inside the tunnel, but only over loopback and the encrypted SSH channel
- The extension has host permission for `127.0.0.1`, so the server needs **no** CORS
- Matching is domain-based (entry URL or name equals the site domain, a subdomain, or the same registrable domain)
- Limited support for login forms inside iframes and Shadow DOM; multi-step cross-domain matching uses registrable-domain heuristics plus a few built-in aliases (microsoft/google/apple/amazon) — for other flows spanning different domains pick the entry in the popup. No TOTP autofill (use `password-safe totp name`)
- If you generate a password but never submit the form, it is not in the vault — always click "Save" on the prompt

## Per-browser install (v0.2.1-beta)

Download the zip from the Release and unzip it to a permanent folder:

| Browser | Package | How |
|---|---|---|
| Chrome | chromium | `chrome://extensions` → Developer mode → Load unpacked |
| Edge | chromium | `edge://extensions` → Developer mode → Load unpacked |
| Brave | chromium | `brave://extensions` → Developer mode → Load unpacked |
| Opera | chromium | `opera://extensions` → Developer mode → Load unpacked |
| Vivaldi | chromium | `vivaldi://extensions` → Developer mode → Load unpacked |
| Firefox 115+ | firefox | `about:debugging#/runtime/this-firefox` → Load Temporary Add-on → choose the zip or `manifest.json` (removed on restart); for permanent use set `xpinstall.signatures.required` to false in Developer Edition/Nightly, or get it signed on AMO |

In Firefox, allow access to `127.0.0.1` under Add-ons → Permissions on first use.

**Safari**: not built. Convert on macOS with `xcrun safari-web-extension-converter extension/` and sign it.

Build yourself: `bash scripts/build-extensions.sh` (output in `dist/`).

Manual multi-step login test: see [test/README.md](test/README.md).

## Desktop client (v0.3.0-beta)
Instead of a manual SSH tunnel you can run the Windows tray client (`password-safe-client.exe`), which keeps the tunnel up and lets the popup show tunnel status / one-click reconnect via native messaging. The extension ID is now fixed (`eaclmdmcljfnhljgbdlcejepnpnfahie`). See [../cmd/password-safe-client/README.md](../cmd/password-safe-client/README.md).

## Export / change device

When unlocked, the popup **Export / change device** section offers:
- **Encrypted bundle**: JSON with your passphrase; on the new device run `password-safe import -f`
- **CSV (Google format)**: plaintext — delete the file right after import
- **Server backup**: create a backup under `~/.password-safe/backups/` on the server now
