# Password Safe — Self-hosted password vault (beta v0.2.1-beta)

**English** | [简体中文](README.zh-CN.md)

> ⚠️ **Beta, not security-audited.** Do not use it as your only password store and always keep an encrypted backup (`password-safe export`). A forgotten master password cannot be recovered.

A self-hosted password vault written in Go: server/CLI + browser extension (autofill, strong password generation on sign-up, saving new accounts).

## Features
- Argon2id-derived KEK → wraps a random DEK → entries encrypted with AES-256-GCM; changing the master password only re-wraps the DEK
- CLI: `init/add/get/list/search/tree/update/delete/generate/import/export/passwd/migrate/verify/totp/shell`
- Exponential back-off lockout after failed unlocks; TOTP; clipboard auto-clear; memorable passphrase generator
- Loopback-only HTTP API (`password-safe serve`, default `127.0.0.1:8787`)
- CSV import with automatic detection of the Google Password Manager export format (`name,url,username,password,note`)
- Browser extension: Chrome / Edge / Brave / Opera / Vivaldi / Firefox, including multi-step logins (username page first, password page next — e.g. Google / Microsoft / AWS / Apple)

## Architecture
```
Browser extension ──▶ local 127.0.0.1:8787 ══SSH tunnel══▶ server 127.0.0.1:8787 (password-safe serve) ──▶ vault.dat (encrypted)
```
The server never listens on a public interface; only SSH port 22 needs to be open.

## Quick start
```bash
# Server (Linux): download the archive for your architecture from Releases, or build it
make build && sudo make install          # or: go build ./cmd/password-safe
password-safe --version
password-safe init                        # set the master password
password-safe add --name work/github --username alice
password-safe get work/github
# Start on boot (user-level systemd)
mkdir -p ~/.config/systemd/user && cp deploy/password-safe.service ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now password-safe
sudo loginctl enable-linger $USER

# Your computer: open the tunnel (keep the window open)
ssh -i ~/key.pem -N -L 8787:127.0.0.1:8787 ubuntu@<your-server-ip>
```
Then install the browser extension, see [extension/README.md](extension/README.md).

## Migrating from Google Password Manager
1. Open https://passwords.google.com/options → "Export passwords" to get a CSV.
2. `scp -i ~/key.pem passwords.csv ubuntu@<your-server-ip>:~/google.csv`
3. On the server: `password-safe import --file ~/google.csv && shred -u ~/google.csv`
4. Delete the local CSV and empty the trash.

## Security model
- The vault file contains only ciphertext; the master password is never written to disk and never goes to any third party
- The API binds only to 127.0.0.1/::1; remote access relies on SSH tunnel encryption
- The extension token lives only in `storage.session` (memory)
- Not defended against: root compromise of the server, malware on your computer, a compromised browser

## Building release artifacts
```bash
make test
make release     # archives for each platform, extension zips and SHA256SUMS in dist/
```

---

## Details

A local account/password vault in Go: **CLI + an HTTP API bound to localhost only**.

- Master password → **Argon2id** → **KEK** → wraps random **DEK** → entries encrypted with **AES-256-GCM(DEK)**
- Stored at `~/.password-safe/vault.dat` by default (mode `0600`)
- Targets **Ubuntu 24.04** (and other Linux). No cloud sync, no GUI.

## Reference projects

Ideas were borrowed from these open-source projects (independent implementation, no copied source):

| Project | What we borrowed |
|------|--------|
| [1001api/vault](https://github.com/1001api/vault) | Argon2id + AES-GCM, KEK/DEK, memory zeroing, interactive CLI |
| [gtchakama/lockr](https://github.com/gtchakama/lockr) | Hierarchical names, Cobra CLI |
| [mojoaar/krypt](https://github.com/mojoaar/krypt) | Failed-unlock lockout, clipboard, TOTP, encrypted export |
| [avahowell/masterkey](https://github.com/avahowell/masterkey) | Interactive vault session |
| [MRivSilv/perkbox](https://github.com/MRivSilv/perkbox) | Timed clipboard clearing |
| [gopasspw/gopass](https://github.com/gopasspw/gopass) | `get --clip`, tree listing, search UX |

## Security model (v2)

```
master password ──Argon2id(salt)──► KEK
                                      │
random DEK ◄──── AES-GCM wrap ───────┘
   │
   └── AES-GCM encrypts the entry plaintext (JSON)
```

- **Changing the master password (`passwd` / `rekey`)** only re-wraps the DEK; entries are **not** re-encrypted.
- **Failed-unlock lockout**: after ~5 wrong attempts, exponential back-off (from 2s up to ~15 min); state in `vault.dat.lockout`, cleared on success.
- **Key zeroing**: sensitive buffers are zeroed on `Lock` and at command exit where possible.
- **Network**: HTTP is loopback only.

### Format versions and migration

| Version | Description |
|------|------|
| v1 | Derived key encrypts data directly (legacy) |
| **v2** (current) | KEK wraps DEK; `init` creates v2 |

For an existing v1 vault:

```bash
password-safe migrate   # enter the master password to upgrade to v2
```

Alternatively: `export` a backup → `init` a new vault → `import` a CSV.

## Backup & upgrade

### Automatic backups (no action needed)

Backups are **encrypted** (same master password) and stored in `~/.password-safe/backups/` (dir 0700, files 0600):

| When | File name |
|------|--------|
| First run of a new program version | `vault-<time>-<version>-upgrade.dat` |
| Before the first change of each day | `vault-<time>-<version>-daily.dat` |
| Before `migrate` | `vault-<time>-<version>-pre-migrate.dat` |
| Before `restore` overwrites | `vault-<time>-<version>-pre-restore.dat` |
| `backup` command / extension "Server backup" | `vault-<time>-<version>-manual.dat` |

The latest 10 of each kind are kept; change with `--keep-backups N` or `PASSWORD_SAFE_BACKUP_KEEP=N`.

Two more safeguards:
- A vault created by a **newer** version is opened read-only by an older program, which **refuses to write** (no downgrade corruption);
- If the vault file is changed externally (e.g. by `restore`), a running `serve` refuses to overwrite it with stale in-memory data; unlock again.

### Export

```bash
password-safe export                                    # encrypted vault copy (same master password)
password-safe export --format json-encrypted -o b.json  # portable encrypted bundle with its own passphrase (recommended for long-term)
password-safe export --format json-encrypted --use-master -o b.json  # encrypted with the master password
password-safe export --format csv --plaintext -o x.csv  # plaintext, Google/Chrome format name,url,username,password,note
password-safe export --format csv-full --plaintext      # plaintext, plus tags,totp_secret
password-safe export --format json --plaintext          # plaintext JSON
password-safe export --format bitwarden --plaintext     # plaintext Bitwarden JSON
```

Plaintext export requires `--plaintext` and typing `yes`; `shred -u file` right after use.

Import detects the format automatically:

```bash
password-safe import -f b.json --skip-duplicates   # skip entries with the same domain+username
```

In the extension popup, "Export backup" downloads an encrypted JSON bundle (you set a passphrase) and "Server backup" creates a backup on the server immediately.

### Recommended upgrade steps

```bash
password-safe backup                             # 1. manual backup
password-safe export --format json-encrypted -o ~/pre-upgrade.json   # optional portable bundle
sudo install -m 755 password-safe /usr/local/bin/password-safe       # 2. replace the binary
password-safe --version
systemctl --user restart password-safe           # 3. restart (an upgrade backup is made automatically)
password-safe verify && password-safe list       # 4. verify + check entries
```

Then unlock the extension again.

### Restore from backup

```bash
password-safe backups                                   # list backups
systemctl --user stop password-safe
password-safe restore vault-20261016-143000-v0.2.0-beta-manual.dat   # file name or full path
systemctl --user start password-safe
password-safe verify
```

`restore` first checks that the backup decrypts with your master password, backs up the current vault as `pre-restore`, then overwrites. A wrong restore can be undone by restoring the `pre-restore` file.

## Commands

| Command | Description |
|------|------|
| `init` | Create a v2 vault |
| `unlock` / `verify` | Check master password / integrity |
| `migrate` | v1 → v2 |
| `passwd` / `rekey` | Change master password (re-wrap DEK only) |
| `add` / `list` / `get` / `update` / `delete` | Entry CRUD |
| `search` / `list -q` | Substring search |
| `tree` | Tree view of hierarchical names (e.g. `work/github`) |
| `get -c` / `--copy` | Copy password to clipboard, cleared after ~30s (not echoed) |
| `totp <name>` | Print the current 6-digit TOTP |
| `generate` | Random password; `--memorable` passphrase |
| `export --format …` | encrypted / json-encrypted / csv / csv-full / json / bitwarden |
| `import` | Auto-detects all formats above, `--skip-duplicates` |
| `backup` / `backups` / `restore` | Manual backup / list / restore |
| `shell` | Interactive REPL (unlock once) |
| `serve` | localhost HTTP API |
| `status` | Paths and format version |

## Dependencies

- Go 1.22+
- `golang.org/x/crypto`, `golang.org/x/term`, `github.com/spf13/cobra`
- `github.com/pquerna/otp` (TOTP)
- Clipboard (optional): `wl-clipboard` or `xclip` / `xsel`

```bash
sudo apt install -y golang-go wl-clipboard   # or xclip
```

## Build & install

```bash
cd /path/to/password-safe
make test
make build
sudo make install          # installs to /usr/local/bin
# or: go build -o bin/password-safe ./cmd/password-safe
```

## Usage examples

```bash
password-safe init
password-safe add --name work/github --username alice --url https://github.com --tags dev
password-safe list
password-safe tree
password-safe search github
password-safe get work/github -c          # copy password, never printed
password-safe totp work/github            # if added with --totp
password-safe generate -l 24
password-safe generate --memorable --words 5
password-safe shell                       # interactive session
password-safe verify
password-safe passwd                      # change master password
password-safe export -o ~/vault-backup.dat
```

CSV import format (first row may be a header):

```text
name,username,password,url,notes,tags,totp_secret
work/mail,user@example.com,secret,https://mail.example.com,note,work,
```

```bash
password-safe import -f entries.csv
```

Custom path:

```bash
password-safe --vault /secure/path/vault.dat init
export PASSWORD_SAFE_VAULT=/secure/path/vault.dat
```

## HTTP API (`serve`)

```bash
password-safe serve
# password-safe serve --unlock
```

| Method | Path | Description |
|------|------|------|
| GET | `/health` | Health check |
| GET | `/api/v1/status` | Initialized / unlocked / format version |
| POST | `/api/v1/unlock` | Unlock, returns a token |
| POST | `/api/v1/lock` | Lock |
| GET/POST | `/api/v1/entries` | List (`?q=` search) / create |
| GET | `/api/v1/search?q=` | Search |
| GET/PUT/DELETE | `/api/v1/entries/{id}` | Get / update / delete |
| GET | `/api/v1/entries/{id}/password` | Password only (session required) |
| GET | `/api/v1/totp/{id}` | Current TOTP |
| POST | `/api/v1/verify` | In-session integrity check |
| POST | `/api/v1/generate` | Generate password / passphrase |
| POST | `/api/v1/export` | Encrypted JSON bundle (body `{"passphrase":"…"}`, never plaintext) |
| POST | `/api/v1/backup` | Create an encrypted backup on the server now |

## systemd

```bash
mkdir -p ~/.config/systemd/user
cp deploy/password-safe.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now password-safe.service
```

The service starts **locked**; call `/api/v1/unlock`. Never put the master password in the unit file.

## Project layout

```
password-safe/
├── cmd/password-safe/
├── internal/
│   ├── crypto/      # Argon2id, AES-GCM, KEK/DEK, password/passphrase generation
│   ├── vault/       # v2 storage, CRUD, search, tree, import, migration
│   ├── lockout/     # failed-unlock back-off
│   ├── clipboard/   # xclip/wl-copy clipboard
│   ├── server/      # HTTP API
│   └── cli/         # Cobra commands
├── extension/       # browser extension (test/ = manual test page)
├── deploy/password-safe.service
├── Makefile
├── README.md        # English
└── README.zh-CN.md  # 简体中文
```

## License

For personal use; decide distribution terms yourself.
