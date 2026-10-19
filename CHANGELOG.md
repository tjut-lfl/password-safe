# 更新日志 / Changelog

## v0.3.4-beta（2026-10-19）
**中文**
- 托盘客户端：支持 **备用主机**（`fallback_host` / `fallback_port`）。每次重连先试主主机，超时/拒绝后再自动试备用；日志与托盘会标明当前经哪条路径（「经 10.x…」或「经备用主机 …」）
- 设置页新增「备用主机 / 备用端口」；配置里可把主主机填 WireGuard 内网，备用填服务器公网 IP（勿把公网 IP 写进仓库文档示例以外的隐私位置——用户本机 config.json 即可）
- SSH 保活默认由 30s 改为 **15s**，超时检测 10s，更快发现假在线隧道
- 仅客户端变更，服务端与浏览器扩展无需升级

**English**
- Tray client: optional **fallback host** (`fallback_host` / `fallback_port`). Each reconnect tries the primary first, then the fallback on timeout/refused; logs and tray show the active path (“via …” / “via fallback …”)
- Setup page adds Fallback host/port; put a WireGuard/VPN address as primary and the server public IP as fallback in local `config.json`
- SSH keepalive default **15s** (was 30s), timeout 10s, so dead tunnels are detected sooner
- Client-only; no server or extension upgrade required


## v0.3.3-beta（2026-10-19）
**中文**
- 修复：登录表单先 `hidden`/后显示时，内容脚本不重新扫描，密码框不出现 🔒（MutationObserver 增加 class/style 监听 + 定时补扫；改进 `visible()` 判断）
- 修复：SPA/fetch 登录时「保存到密码保险箱」提示经常出不来——`offerSave` 先写 pending 再比对条目；内容脚本 await 后立即弹出（不再只依赖 1.5s 后的 takePending 竞态）
- 单测：`extension/test/match_test.mjs` 覆盖 www/apex 与同站别名匹配（匹配逻辑本身已正确，本问题不是 www 误判）
- 仅扩展变更，服务端无需升级

**English**
- Fix: password fields that start `hidden` and become visible later never got the 🔒 icon (observe class/style attributes + periodic rescan; more reliable `visible()`)
- Fix: save prompt often missing after SPA/fetch logins — `offerSave` writes pending before slow entry lookup; content script awaits and shows the bar immediately (no longer races a 1.5s takePending)
- Test: `extension/test/match_test.mjs` for www/apex and site-alias matching (matching was already correct; this was not a www mismatch)
- Extension-only; no server upgrade required

## v0.3.2-beta（2026-10-18）
**中文**
- 会话默认有效期由 30 分钟改为 **6 小时**；可用环境变量 `PASSWORD_SAFE_SESSION_TTL`（如 `6h`）或 `serve --ttl 6h` 配置
- unlock 响应的 `expires_in` / `expires_in_sec` 反映实际 TTL；浏览器扩展优先按服务端返回值锁定
- HTTP `POST /api/v1/export`：默认仍为加密 JSON；新增明文 CSV（`{"format":"csv","confirm":"EXPORT"}`），需已解锁会话
- 扩展弹窗「导出 / 更换设备」：加密备份包、CSV（谷歌密码格式，明文警告）、服务端备份
- CLI `export` 说明补充换设备迁移提示

**English**
- Default session TTL changed from 30 minutes to **6 hours**; configurable via `PASSWORD_SAFE_SESSION_TTL` (e.g. `6h`) or `serve --ttl 6h`
- Unlock response `expires_in` / `expires_in_sec` reflects the real TTL; the extension prefers the server value for auto-lock
- HTTP `POST /api/v1/export`: default remains encrypted JSON; plaintext CSV via `{"format":"csv","confirm":"EXPORT"}` (unlocked session required)
- Extension popup **Export / change device**: encrypted bundle, Google-format CSV (with plaintext warning), server backup
- CLI `export` docs mention device migration

## v0.3.1-beta（2026-10-16）
**中文 / English**
- 修复 Windows 客户端设置页提交报 `bad origin`（浏览器发送 `Origin: null`）/ fix setup page rejecting `Origin: null` on save

## v0.3.0-beta（2026-10-16）
**中文**
- 新增 Windows 托盘客户端 `password-safe-client.exe`（`cmd/password-safe-client`）：纯 Go SSH 隧道（golang.org/x/crypto/ssh，不依赖 ssh.exe），保活、指数退避重连、经隧道的 `/api/v1/status` 健康检查；主机密钥首次信任（known_hosts），不一致即拒绝；本地端口被占用时提示
- 托盘图标颜色/提示显示状态；菜单：状态、重新连接、插件安装引导、连接设置、立即服务端备份（提示）、打开配置、打开日志、开机自启（HKCU Run）、退出；中文界面，非中文系统显示英文
- 首次运行打开本地设置页（127.0.0.1 随机端口 + 随机令牌）；配置 `%APPDATA%\password-safe-client\config.json`，日志滚动且不记录秘密；支持带口令的私钥（口令仅在内存）
- 内置 Chromium 插件，解压到 `%LOCALAPPDATA%\password-safe-client\extension` 并原地更新；引导页可复制路径、直接用 chrome.exe / msedge.exe 打开扩展页
- 原生消息主机 `com.tjut_lfl.password_safe`（Chrome + Edge，HKCU 注册），`--install` / `--uninstall`
- 扩展：manifest 加入固定公钥，扩展 ID 固定为 `eaclmdmcljfnhljgbdlcejepnpnfahie`；Chromium 版新增 `nativeMessaging` 权限，弹窗显示隧道状态与「一键重连」，连不上时提示「请启动 Password Safe 客户端」。Firefox 版不含该权限，行为不变
- 也提供 Linux（托盘）与 macOS（无托盘）客户端构建
- 服务端未改动，无需升级

**English**
- New Windows tray client `password-safe-client.exe` (`cmd/password-safe-client`): pure-Go SSH tunnel (golang.org/x/crypto/ssh, no ssh.exe), keepalives, exponential-backoff reconnect, `/api/v1/status` health check through the tunnel; host-key TOFU (known_hosts) with refusal on mismatch; busy local port is reported
- Tray icon colour/tooltip shows status; menu: Status, Reconnect, Extension guide, Connection settings, Back up server now (hint), Open config, Open log, Start at login (HKCU Run), Quit; Chinese UI with English fallback
- First run opens a local setup page (random 127.0.0.1 port + random token); config in `%APPDATA%\password-safe-client\config.json`; rotating log with no secrets; passphrase-protected keys supported (passphrase in memory only)
- Bundled Chromium extension extracted to `%LOCALAPPDATA%\password-safe-client\extension` and updated in place; guide page with copy-path and buttons that launch chrome.exe / msedge.exe on the extensions page
- Native messaging host `com.tjut_lfl.password_safe` (Chrome + Edge, HKCU), `--install` / `--uninstall`
- Extension: manifest now carries a public key so the ID is fixed (`eaclmdmcljfnhljgbdlcejepnpnfahie`); Chromium build adds `nativeMessaging`, popup shows tunnel status + one-click reconnect and "please start the Password Safe client" when unreachable. Firefox build unchanged
- Linux (tray) and macOS (no tray) client builds also provided
- No server changes; no server upgrade needed

## v0.2.1-beta（2026-10-16）
**中文**
- 扩展：支持分步登录（先用户名页、后密码页，如 Google / Microsoft / AWS / Apple）。用户名页也显示 🔒，选条目只填用户名并按标签页记住（会话内存，5 分钟过期，不存密码）；密码框出现（SPA 同页或跳转到同站子域）后自动填入，也可点 🔒 选择
- 按可注册域名 + 内置别名（microsoft / google / apple / amazon）匹配同站多域名
- 弹窗：用户名页只填用户名；密码页把记住的条目排第一、只填密码
- 保存提示：把第 1 步输入的用户名与第 2 步的密码合并；不覆盖站点在密码页预填（含隐藏）的用户名
- 新增手动测试页 `extension/test/multi-step-login.html`
- 文档：README、扩展 README、发布说明改为中英双语
- 服务端 Go 代码未改动

**English**
- Extension: multi-step login support (username page, then password page — Google / Microsoft / AWS / Apple). 🔒 now also appears on username-only pages; choosing an entry fills only the username and remembers it per tab (session memory, 5-min TTL, no password stored); when the password field appears (same SPA page or after navigating to another subdomain of the same site) it is filled automatically or via 🔒
- Same-site matching across subdomains by registrable domain + built-in aliases (microsoft / google / apple / amazon)
- Popup: fills username only on username pages; on password pages the remembered entry is listed first and only the password is filled
- Save prompt combines the step-1 username with the step-2 password; site-prefilled (incl. hidden) usernames on the password page are not overwritten
- New manual test page `extension/test/multi-step-login.html`
- Docs: README, extension README and release notes are now bilingual (Chinese / English)
- No server-side Go changes

## v0.2.0-beta（2026-10-16）
重点：导出选项与升级防丢失。 / Focus: export options and no data loss on upgrade (see release notes for English).
- `export --format`：encrypted（默认）、json-encrypted（可移植加密包，导出口令或 `--use-master`）、csv / csv-full / json / bitwarden（明文，需 `--plaintext` 并确认，文件 0600）
- `import` 自动识别以上全部格式及 Google/Chrome CSV，`--skip-duplicates` 按域名+用户名去重
- 自动加密备份：新版本首次运行、每日首次写入、migrate 前、restore 前；每类保留 10 份（`--keep-backups`）
- 新命令：`backup`、`backup list` / `backups`、`restore <文件>`（先校验再覆盖）
- 前向兼容：更新版本格式的保险箱只读打开、拒绝写入；保留未知 JSON 字段；检测外部修改，避免旧会话覆盖
- HTTP API：`POST /api/v1/export`（只返回加密 JSON 包）、`POST /api/v1/backup`；移除旧的 `GET /api/v1/export`
- 扩展：弹窗新增「导出备份」「服务端备份」
- 测试：v1 迁移、各格式往返、去重、备份轮转、恢复、只读保护、API 导出

## v0.1.0-beta（2026-10-16）
首个测试版，尚未经过安全审计。 / First beta, not security-audited.
- Argon2id KEK + AES-256-GCM DEK 加密保险箱，v1→v2 迁移
- CLI：增删改查、搜索、树形列表、TOTP、剪贴板、交互 shell、改主密码、校验、加密导出
- CSV 导入，兼容 Google 密码管理器导出格式
- 失败解锁指数退避锁定
- 仅 loopback 的 HTTP API 与 systemd 用户服务示例
- 浏览器扩展（MV3）：Chrome/Edge/Brave/Opera/Vivaldi 与 Firefox 115+
- `--version`、交叉编译脚本、GitHub Actions CI
