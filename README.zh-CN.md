# Password Safe — 自建密码保险箱（测试版 v0.3.5-beta）

[English](README.md) | **简体中文**

> ⚠️ **测试版，尚未经过安全审计。** 请勿作为唯一的密码存储，务必保留加密备份（`password-safe export`）。主密码遗忘后数据无法找回。

用 Go 编写的自托管密码保险箱：服务端/CLI + 浏览器扩展（自动填充、注册时生成强密码、保存新账号）。

## 功能
- Argon2id 派生 KEK → 包装随机 DEK → AES-256-GCM 加密条目；改主密码只重包 DEK
- CLI：`init/add/get/list/search/tree/update/delete/generate/import/export/passwd/migrate/verify/totp/shell`
- 连续输错主密码指数退避锁定；TOTP；剪贴板定时清空；可记忆口令生成
- 仅监听 loopback 的 HTTP API（`password-safe serve`，默认 `127.0.0.1:8787`）
- 支持导入 CSV，自动识别 Google 密码管理器导出格式（`name,url,username,password,note`）
- 浏览器扩展：Chrome / Edge / Brave / Opera / Vivaldi / Firefox；支持分步登录（先用户名页、后密码页，如 Google / Microsoft / AWS / Apple）

## 架构
```
浏览器扩展 ──▶ 本机 127.0.0.1:8787 ══SSH 隧道══▶ 服务器 127.0.0.1:8787（password-safe serve）──▶ vault.dat（加密）
```
服务端永不对公网监听，只需开放 SSH 22 端口。

## 已知限制：海外服务器与 SSH 隧道

本项目的远程访问依赖本机到服务器的 **SSH 隧道**（托盘客户端或手动 `ssh -L`）。若服务器部署在**海外**（例如 AWS 东京、新加坡等），从国内网络直连 SSH（TCP 22）可能出现：

- 偶发连不上、长时间 `i/o timeout`
- 已连接后 keepalive 超时、隧道中断，托盘显示「未连接」
- WireGuard / VPN 界面显示“已连接”，但内网 SSH（如 `10.77.0.x`）仍不通（假在线）

这通常是 **跨境网络路径不稳定**，不是保险箱程序本身故障。可选用 VPN/WireGuard、换网络（如手机热点），或把服务部署在网络更近的地区。需要完全本机、不依赖公网隧道时，可参考独立项目 [password-safe-local](https://github.com/tjut-lfl/password-safe-local)。


## 快速开始
```bash
# 服务器（Linux）：下载 Release 中对应架构的压缩包，或自行编译
make build && sudo make install          # 或 go build ./cmd/password-safe
password-safe --version
password-safe init                        # 设置主密码
password-safe add --name work/github --username alice
password-safe get work/github
# 开机自启（用户级 systemd）
mkdir -p ~/.config/systemd/user && cp deploy/password-safe.service ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now password-safe
sudo loginctl enable-linger $USER

# 本机：建立隧道（窗口保持打开）
ssh -i ~/key.pem -N -L 8787:127.0.0.1:8787 ubuntu@<your-server-ip>
```
然后安装浏览器扩展，见 [extension/README.zh-CN.md](extension/README.zh-CN.md)。


## 桌面客户端（Windows 托盘，v0.3.5-beta）

不想每次手敲 `ssh -L` 命令？到 Releases 下载 `password-safe-client-…-windows-amd64.zip`，运行 `password-safe-client.exe` 即可：后台维持 SSH 隧道（纯 Go、自动重连、可选备用主机、主机密钥首次信任），托盘显示状态，自动解压内置插件并打开安装引导，插件弹窗里能看到隧道状态并一键重连（原生消息）。详见 [cmd/password-safe-client/README.zh-CN.md](cmd/password-safe-client/README.zh-CN.md)。

## 从 Google 密码管理器迁移
1. 打开 https://passwords.google.com/options →「导出密码」，得到 CSV。
2. `scp -i ~/key.pem 密码.csv ubuntu@<your-server-ip>:~/google.csv`
3. 服务器上：`password-safe import --file ~/google.csv && shred -u ~/google.csv`
4. 删除本机 CSV 并清空回收站。

## 安全模型
- 保险箱文件只含密文；主密码不落盘，不经过网络以外的任何第三方
- API 只绑定 127.0.0.1/::1，远程访问依赖 SSH 隧道加密
- 扩展令牌仅存于 `storage.session`（内存）
- 未防御：服务器 root 被攻破、本机恶意软件、浏览器被入侵

## 构建发布产物
```bash
make test
make release     # dist/ 下生成各平台压缩包、扩展 zip 和 SHA256SUMS
```

---

## 详细说明

用 Go 编写的本地账号密码保险箱：**CLI + 仅绑定 localhost 的 HTTP API**。

- 主密码 → **Argon2id** 派生 **KEK** → 包装随机 **DEK** → 条目用 **AES-256-GCM(DEK)** 加密
- 默认存放 `~/.password-safe/vault.dat`（权限 `0600`）
- 适用于 **Ubuntu 24.04**（及其它 Linux）。无云同步、无图形界面。

## 参考项目

设计时参考了下列开源项目的思路（实现为独立代码，未照搬源码）：

| 项目 | 借鉴点 |
|------|--------|
| [1001api/vault](https://github.com/1001api/vault) | Argon2id + AES-GCM、KEK/DEK、内存清零、交互 CLI |
| [gtchakama/lockr](https://github.com/gtchakama/lockr) | 层级路径名、Cobra CLI |
| [mojoaar/krypt](https://github.com/mojoaar/krypt) | 失败解锁锁定、剪贴板、TOTP、加密导出 |
| [avahowell/masterkey](https://github.com/avahowell/masterkey) | 交互式保险箱会话 |
| [MRivSilv/perkbox](https://github.com/MRivSilv/perkbox) | 剪贴板定时清空 |
| [gopasspw/gopass](https://github.com/gopasspw/gopass) | `get --clip`、树形列表、搜索 UX |

## 安全模型（v2）

```
主密码 ──Argon2id(salt)──► KEK
                              │
随机 DEK ◄── AES-GCM 包装 ──┘
   │
   └── AES-GCM 加密条目明文（JSON）
```

- **更换主密码（`passwd` / `rekey`）**：只重新包装 DEK，**不必**用新密钥重加密每一条条目。
- **失败解锁锁定**：连续错误约 5 次后进入指数退避（2s 起，上限约 15 分钟），状态文件为 `vault.dat.lockout`。成功解锁后清除。
- **密钥清零**：`Lock` 与命令结束时尽量 `Zero` 敏感缓冲。
- **网络**：HTTP 仅允许 loopback。

### 格式版本与迁移

| 版本 | 说明 |
|------|------|
| v1 | 派生密钥直接加密 data（旧版） |
| **v2**（当前） | KEK 包装 DEK；`init` 默认创建 v2 |

若已有 v1 保险箱：

```bash
password-safe migrate   # 输入主密码，升级为 v2
```

也可：`export` 备份 → 新库 `init` → `import` CSV（需先自行从旧库导出明文 CSV）。

## 备份与升级

### 自动备份（无需操作）

备份文件都是**加密态**（与保险箱同一主密码），放在 `~/.password-safe/backups/`（目录 0700，文件 0600）：

| 时机 | 文件名 |
|------|--------|
| 新版本程序第一次运行 | `vault-<时间>-<版本>-upgrade.dat` |
| 每天第一次修改保险箱前 | `vault-<时间>-<版本>-daily.dat` |
| `migrate` 之前 | `vault-<时间>-<版本>-pre-migrate.dat` |
| `restore` 覆盖之前 | `vault-<时间>-<版本>-pre-restore.dat` |
| `backup` 命令 / 扩展「服务端备份」 | `vault-<时间>-<版本>-manual.dat` |

每类默认保留最近 10 份，可用 `--keep-backups N` 或环境变量 `PASSWORD_SAFE_BACKUP_KEEP=N` 调整。

另外两道保险：
- 如果保险箱是**更新版本**创建的，旧程序只读打开，**拒绝写入**，不会降级损坏；
- 保险箱文件被外部修改（例如 `restore`）后，正在运行的 `serve` 会拒绝用旧内存数据覆盖，需重新解锁。

### 导出

```bash
password-safe export                                    # 加密保险箱副本（同一主密码）
password-safe export --format json-encrypted -o b.json  # 可移植加密包，单独设导出口令（推荐长期保存）
password-safe export --format json-encrypted --use-master -o b.json  # 用主密码加密
password-safe export --format csv --plaintext -o x.csv  # 明文，Google/Chrome 格式 name,url,username,password,note
password-safe export --format csv-full --plaintext      # 明文，额外 tags,totp_secret
password-safe export --format json --plaintext          # 明文 JSON
password-safe export --format bitwarden --plaintext     # 明文 Bitwarden JSON
```

明文导出必须加 `--plaintext`，终端里还要输入 `yes`；用完立刻 `shred -u 文件`。换设备推荐 `export --format json-encrypted`，到新机器 `import -f`。

导入时自动识别格式：

```bash
password-safe import -f b.json --skip-duplicates   # 按 域名+用户名 跳过已有条目
```

浏览器扩展弹窗里的 **导出 / 更换设备** 提供：加密 JSON 备份包（自设口令）、明文谷歌格式 CSV（需确认，导入后请删除）、以及服务端备份。会话默认 **6 小时** 后自动锁定（可用 `PASSWORD_SAFE_SESSION_TTL` 或 `serve --ttl` 调整）。

### 推荐升级步骤

```bash
password-safe backup                             # 1. 手动备份一份
password-safe export --format json-encrypted -o ~/pre-upgrade.json   # 可选：再导出可移植包
sudo install -m 755 password-safe /usr/local/bin/password-safe       # 2. 替换程序（路径按实际安装位置）
password-safe --version
systemctl --user restart password-safe           # 3. 重启服务（会自动做 upgrade 备份）
password-safe verify && password-safe list       # 4. 校验 + 确认条目都在
```

然后在扩展里重新解锁一次。

### 从备份恢复

```bash
password-safe backups                                   # 列出备份
systemctl --user stop password-safe
password-safe restore vault-20261016-143000-v0.2.0-beta-manual.dat   # 可写文件名或完整路径
systemctl --user start password-safe
password-safe verify
```

`restore` 会先用主密码校验备份可解密，再把当前保险箱备份为 `pre-restore`，最后覆盖。恢复错了还能再 restore 回 `pre-restore` 那份。


## 功能一览

| 命令 | 说明 |
|------|------|
| `init` | 创建 v2 保险箱 |
| `unlock` / `verify` | 校验主密码 / 校验完整性 |
| `migrate` | v1 → v2 |
| `passwd` / `rekey` | 更换主密码（仅重包装 DEK） |
| `add` / `list` / `get` / `update` / `delete` | 条目 CRUD |
| `search` / `list -q` | 子串搜索 |
| `tree` | 层级名称树形列表（如 `work/github`） |
| `get -c` / `--copy` | 复制密码到剪贴板，约 30s 后清空（不回显密码） |
| `totp <name>` | 输出当前 6 位 TOTP |
| `generate` | 随机密码；`--memorable` 口令短语 |
| `export --format …` | encrypted / json-encrypted / csv / csv-full / json / bitwarden |
| `import` | 自动识别以上所有格式，`--skip-duplicates` 去重 |
| `backup` / `backups` / `restore` | 手动备份 / 列出备份 / 从备份恢复 |
| `shell` | 交互 REPL（解锁一次） |
| `serve` | localhost HTTP API |
| `status` | 路径与格式版本 |

## 依赖

- Go 1.22+
- `golang.org/x/crypto`、`golang.org/x/term`、`github.com/spf13/cobra`
- `github.com/pquerna/otp`（TOTP）
- 剪贴板（可选）：`wl-clipboard` 或 `xclip` / `xsel`

```bash
sudo apt install -y golang-go wl-clipboard   # 或 xclip
```

## 编译安装

```bash
cd /path/to/password-safe
make test
make build
sudo make install          # 安装到 /usr/local/bin
# 或: go build -o bin/password-safe ./cmd/password-safe
```

## 快速开始

```bash
password-safe init
password-safe add --name work/github --username alice --url https://github.com --tags dev
password-safe list
password-safe tree
password-safe search github
password-safe get work/github -c          # 复制密码，不打印明文
password-safe totp work/github            # 若添加时带了 --totp
password-safe generate -l 24
password-safe generate --memorable --words 5
password-safe shell                       # 交互会话
password-safe verify
password-safe passwd                      # 改主密码
password-safe export -o ~/vault-backup.dat
```

CSV 导入格式（首行可为表头）：

```text
name,username,password,url,notes,tags,totp_secret
work/mail,user@example.com,secret,https://mail.example.com,备注,work,
```

```bash
password-safe import -f entries.csv
```

自定义路径：

```bash
password-safe --vault /secure/path/vault.dat init
export PASSWORD_SAFE_VAULT=/secure/path/vault.dat
```

## HTTP API（`serve`）

```bash
password-safe serve
# password-safe serve --unlock
# password-safe serve --ttl 6h   # 或 PASSWORD_SAFE_SESSION_TTL=6h
```

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查 |
| GET | `/api/v1/status` | 初始化 / 解锁 / 格式版本 |
| POST | `/api/v1/unlock` | 解锁，返回 token |
| POST | `/api/v1/lock` | 锁定 |
| GET/POST | `/api/v1/entries` | 列表（`?q=` 搜索）/ 新增 |
| GET | `/api/v1/search?q=` | 搜索 |
| GET/PUT/DELETE | `/api/v1/entries/{id}` | 获取 / 更新 / 删除 |
| GET | `/api/v1/entries/{id}/password` | 仅返回密码（需会话） |
| GET | `/api/v1/totp/{id}` | 当前 TOTP |
| POST | `/api/v1/verify` | 会话内完整性确认 |
| POST | `/api/v1/generate` | 生成密码 / 口令短语 |
| POST | `/api/v1/export` | 默认加密 JSON（`{"passphrase":"…"}`）；明文 CSV：`{"format":"csv","confirm":"EXPORT"}`（需会话） |
| POST | `/api/v1/backup` | 在服务端立即创建一份加密备份 |

## systemd

```bash
mkdir -p ~/.config/systemd/user
cp deploy/password-safe.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now password-safe.service
```

服务启动后仍为**锁定**，需调用 `/api/v1/unlock`。不要把主密码写进 unit。

## 项目结构

```
password-safe/
├── cmd/password-safe/
├── internal/
│   ├── crypto/      # Argon2id、AES-GCM、KEK/DEK、密码/口令生成
│   ├── vault/       # v2 存储、CRUD、搜索、树、导入、迁移
│   ├── lockout/     # 失败解锁退避
│   ├── clipboard/   # xclip/wl-copy 剪贴板
│   ├── server/      # HTTP API
│   └── cli/         # Cobra 命令
├── deploy/password-safe.service
├── extension/       # 浏览器扩展（test/ 为手动测试页）
├── Makefile
├── README.md        # English
└── README.zh-CN.md  # 简体中文
```

## 许可证

按需自用；请自行决定分发条款。
