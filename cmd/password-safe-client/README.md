# Password Safe desktop client (beta v0.3.0-beta)

English | [中文](README.zh-CN.md)

A small Windows tray app (Linux has a tray too; macOS runs without a tray). Once started it:

- **Keeps the SSH tunnel up** — pure Go (no `ssh.exe` needed), local `127.0.0.1:8787` → server `127.0.0.1:8787`, keepalives, exponential-backoff reconnect, and a health check of `/api/v1/status` through the tunnel.
- **Shows status in the tray icon** — green = connected & server OK, yellow = connecting / server not responding, gray = disconnected, red = error (hover for details).
- **Guides the extension install** — the extension is embedded in the exe, extracted to `%LOCALAPPDATA%\password-safe-client\extension`, and a step-by-step guide page opens.
- **Talks to the extension (native messaging)** — the popup shows tunnel status with a one-click reconnect, and says "please start the Password Safe client" when the client is not running.

> ⚠️ Beta, not security-audited.

## First run

1. Unzip `password-safe-client-v0.3.0-beta-windows-amd64.zip` and put `password-safe-client.exe` somewhere permanent (e.g. `%LOCALAPPDATA%\Programs\password-safe-client\`).
2. Double-click it. Two local pages open in your browser:
   - **Connection settings** — server host, SSH port (22), user (e.g. `ubuntu`), private key path (e.g. `C:\Users\<you>\.ssh\<your-key>.pem`). Click *Save & connect*.
   - **Extension guide** — *Open Chrome/Edge extensions page* → enable *Developer mode* → *Load unpacked* → pick the folder shown (copy button provided).
3. When the tray icon turns green you are connected. Open the extension and unlock with your master password.

If you loaded the extension from another folder before, remove that copy. The extension ID is fixed to `eaclmdmcljfnhljgbdlcejepnpnfahie`; native messaging only accepts that ID.

Stop any manual `ssh -L 8787:...` tunnel or scheduled task, otherwise local port 8787 is busy and the tray reports it.

## Tray menu

Status · Reconnect · Extension install guide · Connection settings (also where you enter an encrypted key's passphrase) · Back up server now (the backup API needs the session token, which only the extension holds, so this tells you to use *Server backup* in the extension) · Open config · Open log · Start at login (`HKCU\...\Run`) · Quit.

## Command line

```
password-safe-client.exe --install     register native host (Chrome + Edge) and enable autostart
password-safe-client.exe --uninstall   remove those registry entries and autostart
password-safe-client.exe --version
```
No arguments = tray mode (the native host is re-registered on every start). Launching a second copy opens the guide in the running instance.

## Files

`%APPDATA%\password-safe-client\`: `config.json` (no secrets), `known_hosts` (TOFU fingerprints), `client.log` (1 MB rotation ×3, no secrets), `control.json` (local control port + random token), `com.tjut_lfl.password_safe.json` (native host manifest). `%LOCALAPPDATA%\password-safe-client\extension`: the extracted extension (updated in place; just click reload).

## Security

- Host key TOFU: the first fingerprint is recorded; a later mismatch is refused with a clear error.
- The key is read locally only; an encrypted key's passphrase is held in memory only.
- Local pages / control API listen on a random `127.0.0.1` port and require a random token; native messaging is limited to the fixed extension ID.
- The client never holds the master password or the session token.

## Limits

- Browsers don't allow silent extension installs, so you load it once by hand.
- The Windows tray UI has not been tested on real Windows yet (tunnel logic is covered by automated tests on Linux).
- macOS build has no tray (needs cgo). Firefox extension has no native messaging.
