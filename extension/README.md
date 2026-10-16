# 密码保险箱 浏览器扩展（Chrome / Edge）

本扩展是 `password-safe` 服务的浏览器客户端：
- 登录页自动填充账号密码（输入框右侧的 🔒 图标，或工具栏弹窗中点击条目）
- 注册/改密页一键生成强密码，同时填入「密码」和「确认密码」
- 提交表单后询问「保存到密码保险箱？」，**确认后**才新建/更新条目
- 主密码只用于解锁；会话令牌只保存在浏览器内存（`chrome.storage.session`），关闭浏览器即失效，30 分钟后自动锁定

```
浏览器扩展 ──http──▶ 127.0.0.1:8787（本机） ══SSH 隧道══▶ EC2 上的 127.0.0.1:8787（password-safe serve）
```
服务端始终只监听 `127.0.0.1`，**不要**在 AWS 安全组开放 8787 端口，只需开放 22。

---

## 一、服务器端：用 systemd 常驻运行 serve

`serve` 启动时**不需要**主密码，保险箱处于锁定状态；由扩展（或 curl）调用 `/api/v1/unlock` 解锁。所以可以放心交给 systemd。

1. SSH 登录服务器，先初始化保险箱（只做一次）：
   ```bash
   password-safe init
   ```
2. 创建用户级服务：
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
   sudo loginctl enable-linger ubuntu      # 让服务在你退出 SSH 后、开机后也运行
   systemctl --user status password-safe
   curl -s http://127.0.0.1:8787/health     # 应返回 {"status":"ok",...}
   ```
   查看日志：`journalctl --user -u password-safe -f`

> 注意：服务端同一时间只有一个会话令牌。在命令行用 curl 解锁、或在另一台电脑的扩展里解锁，都会让当前扩展的会话失效，重新解锁即可。点「锁定」会把服务端整个保险箱锁上。

---

## 二、本机：建立 SSH 隧道

把下面的 `key.pem` 换成你的私钥路径。隧道建立后，本机 `http://127.0.0.1:8787` 就等于服务器上的服务。

### Windows（PowerShell，系统自带 OpenSSH）
```powershell
ssh -i C:\Users\你\key.pem -N -L 8787:127.0.0.1:8787 -o ServerAliveInterval=30 -o ExitOnForwardFailure=yes ubuntu@<your-server-ip>
```
私钥权限报错时：`icacls key.pem /inheritance:r /grant:r "$($env:USERNAME):R"`

保持常驻（断线自动重连）：新建 `tunnel.ps1`
```powershell
while ($true) {
  ssh -i C:\Users\你\key.pem -N -L 8787:127.0.0.1:8787 -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -o ExitOnForwardFailure=yes ubuntu@<your-server-ip>
  Start-Sleep 5
}
```
再用「任务计划程序」→ 创建任务 → 触发器「登录时」→ 操作：`powershell -WindowStyle Hidden -File C:\路径\tunnel.ps1`。

### macOS / Linux
```bash
chmod 600 ~/key.pem
ssh -i ~/key.pem -N -L 8787:127.0.0.1:8787 -o ServerAliveInterval=30 -o ExitOnForwardFailure=yes ubuntu@<your-server-ip>
```
保持常驻：安装 autossh（`brew install autossh` / `sudo apt install autossh`）
```bash
autossh -M 0 -f -N -i ~/key.pem -L 8787:127.0.0.1:8787 \
  -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -o ExitOnForwardFailure=yes ubuntu@<your-server-ip>
```
Linux 开机自启可写成 systemd 用户服务（`ExecStart=/usr/bin/autossh -M 0 -N ...`，去掉 `-f`）；macOS 可用 launchd 或放进「登录项」脚本。

推荐在 `~/.ssh/config` 里简化：
```
Host vault
  HostName <your-server-ip>
  User ubuntu
  IdentityFile ~/key.pem
  LocalForward 8787 127.0.0.1:8787
  ServerAliveInterval 30
  ExitOnForwardFailure yes
```
之后只需 `ssh -N vault`。

本机 8787 被占用时，可改用 `-L 18787:127.0.0.1:8787`，并在扩展「设置」里把地址改为 `http://127.0.0.1:18787`。

---

## 三、安装扩展（加载已解压的扩展）

1. 解压 `password-safe-extension.zip` 到一个固定目录（以后别删）
2. **Chrome**：地址栏打开 `chrome://extensions` → 右上角打开「开发者模式」→「加载已解压的扩展程序」→ 选该目录
3. **Edge**：打开 `edge://extensions` → 左侧打开「开发人员模式」→「加载解压缩的扩展」→ 选该目录
4. 把扩展图标固定到工具栏；右键图标 →「选项」→ 点「测试连接」，显示「连接成功」即可

---

## 四、使用

- **解锁**：点击工具栏图标，输入主密码 →「解锁」
- **登录**：在登录页点击密码框右侧的 🔒 → 选择账号；或在弹窗「当前网站」列表里点击条目
- **搜索**：弹窗顶部输入关键词，点击结果即填充到当前页
- **注册新账号**：在注册页点 🔒 →「生成强密码并填入」（优先用服务端生成，连不上时用浏览器本地安全随机数生成），密码和确认密码同时填好
- **保存**：提交登录/注册表单后，页面右上角出现「保存到密码保险箱？」，点「保存」才会写入。条目名为网站域名（同域多个账号时为 `域名/用户名`）；同站同用户名已存在则改为「更新」密码
- **锁定**：弹窗点「锁定」；30 分钟后也会自动锁定

## 五、安全说明与限制
- 扩展不含任何远程代码，不记录密码；与服务端的通信只在后台脚本中进行，网页拿不到令牌
- 隧道内是明文 HTTP，但只在本机回环和加密的 SSH 通道中传输
- 由于扩展后台带有 `127.0.0.1` 的主机权限，服务端**无需**开启 CORS
- 匹配规则基于域名（条目的 URL 或名称与当前网站域名一致或为其子域）
- iframe 内的登录框、Shadow DOM 中的输入框、多步登录（先用户名后密码）支持有限；不提供 TOTP 自动填充（可在命令行 `password-safe totp 名称`）
- 若生成密码后没有提交表单，保险箱里不会有这个密码——请务必在保存提示中点「保存」

## 各浏览器安装（v0.2.0-beta）

从 Release 下载对应 zip 并解压到固定目录（安装后不要删除该目录）：

| 浏览器 | 包 | 安装方式 |
|---|---|---|
| Chrome | chromium | `chrome://extensions` → 开发者模式 → 加载已解压的扩展程序 |
| Edge | chromium | `edge://extensions` → 开发人员模式 → 加载解压缩的扩展 |
| Brave | chromium | `brave://extensions` → 开发者模式 → 加载已解压的扩展程序 |
| Opera | chromium | `opera://extensions` → 开发者模式 → 加载已解压的扩展程序 |
| Vivaldi | chromium | `vivaldi://extensions` → 开发者模式 → 加载已解压的扩展程序 |
| Firefox 115+ | firefox | `about:debugging#/runtime/this-firefox` → 临时载入附加组件 → 选择 zip 或 `manifest.json`（重启后失效）；长期使用需 Developer Edition/Nightly 中将 `xpinstall.signatures.required` 设为 false，或自行提交 AMO 签名 |

Firefox 首次使用时，请在「附加组件 → 权限」中允许访问 `127.0.0.1`。

**Safari**：未提供构建。需在 macOS 上用 Xcode 的 `xcrun safari-web-extension-converter extension/` 转换并签名。

自行打包：`bash scripts/build-extensions.sh`（输出到 `dist/`）。
