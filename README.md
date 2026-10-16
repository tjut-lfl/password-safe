# Password Safe — 自建密码保险箱（测试版 v0.1.0-beta）

> ⚠️ **测试版，尚未经过安全审计。** 请勿作为唯一的密码存储，务必保留加密备份（`password-safe export`）。主密码遗忘后数据无法找回。

用 Go 编写的自托管密码保险箱：服务端/CLI + 浏览器扩展（自动填充、注册时生成强密码、保存新账号）。

## 功能
- Argon2id 派生 KEK → 包装随机 DEK → AES-256-GCM 加密条目；改主密码只重包 DEK
- CLI：`init/add/get/list/search/tree/update/delete/generate/import/export/passwd/migrate/verify/totp/shell`
- 连续输错主密码指数退避锁定；TOTP；剪贴板定时清空；可记忆口令生成
- 仅监听 loopback 的 HTTP API（`password-safe serve`，默认 `127.0.0.1:8787`）
- 支持导入 CSV，自动识别 Google 密码管理器导出格式（`name,url,username,password,note`）
- 浏览器扩展：Chrome / Edge / Brave / Opera / Vivaldi / Firefox

## 架构
```
浏览器扩展 ──▶ 本机 127.0.0.1:8787 ══SSH 隧道══▶ 服务器 127.0.0.1:8787（password-safe serve）──▶ vault.dat（加密）
```
服务端永不对公网监听，只需开放 SSH 22 端口。

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
然后安装浏览器扩展，见 [extension/README.md](extension/README.md)。

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
| `import` / `export` | CSV 导入 / 加密文件导出 |
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
work/mail,a@b.com,secret,https://mail,备注,work,
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
| GET | `/api/v1/export` | 下载加密 vault |

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
├── Makefile
└── README.md
```

## 许可证

按需自用；请自行决定分发条款。
