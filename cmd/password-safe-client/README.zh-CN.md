# Password Safe 桌面客户端（测试版 v0.3.0-beta）

[English](README.md) | 中文

一个 Windows 托盘小程序（Linux 也有托盘，macOS 为无托盘模式），打开后：

- **维持 SSH 隧道**：纯 Go 实现（不依赖 `ssh.exe`），本机 `127.0.0.1:8787` → 服务器 `127.0.0.1:8787`。保活、指数退避自动重连，并通过隧道请求 `/api/v1/status` 做健康检查。
- **托盘图标显示状态**：绿色=已连接且服务端正常；黄色=连接中/服务端未响应；灰色=未连接；红色=出错（鼠标悬停看原因）。
- **引导安装浏览器插件**：插件已内置在 exe 里，会解压到 `%LOCALAPPDATA%\password-safe-client\extension`，并打开一步步的安装引导页。
- **与插件通信（原生消息）**：插件弹窗里显示隧道状态，有「一键重连」；客户端没开时提示「请启动 Password Safe 客户端」。

> ⚠️ 测试版，尚未经过安全审计。

## 首次运行

1. 解压 `password-safe-client-v0.3.0-beta-windows-amd64.zip`，把 `password-safe-client.exe` 放到一个固定位置（例如 `%LOCALAPPDATA%\Programs\password-safe-client\`），之后不要再移动。
2. 双击运行。浏览器会打开两个本地页面：
   - **连接设置**：填服务器地址、SSH 端口（22）、用户名（如 `ubuntu`）、私钥路径（如 `C:\Users\<你的用户名>\.ssh\<your-key>.pem`）。点「保存并连接」。
   - **插件安装引导**：点「打开 Chrome/Edge 扩展页」→ 打开「开发者模式」→「加载已解压的扩展程序」→ 选页面上显示的文件夹（可一键复制路径）。
3. 托盘图标变绿即连上。点插件图标，输入主密码解锁。

如果之前已从别的文件夹加载过插件，请先移除旧的那个，只保留引导页里这个（扩展 ID 固定为 `eaclmdmcljfnhljgbdlcejepnpnfahie`，原生消息只认这个 ID）。

也请关掉手动开的 `ssh -L 8787:...` 隧道或之前建的计划任务，否则本地端口 8787 会被占用，托盘会提示「本地端口已被占用」。

## 托盘菜单

| 菜单 | 作用 |
|---|---|
| 状态 | 显示连接状态与保险箱锁定状态 |
| 重新连接 | 立即断开并重连（跳过退避等待） |
| 打开插件安装引导 | 重新解压插件并打开引导页 |
| 连接设置 | 打开设置页（也可输入加密私钥的口令） |
| 立即服务端备份 | 备份接口需要登录令牌，而令牌只在插件里；这里会提示你在插件中点「服务端备份」 |
| 打开配置 / 打开日志 | 用记事本打开 `config.json` / `client.log` |
| 开机自启 | 写入/删除 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` |
| 退出 | 关闭隧道并退出 |

## 命令行

```
password-safe-client.exe --install     注册原生消息主机（Chrome + Edge）并开启开机自启
password-safe-client.exe --uninstall   删除上述注册表项与开机自启
password-safe-client.exe --version
```
不带参数运行即为托盘模式（每次启动都会自动注册原生消息主机，exe 路径变了也能跟上）。再次双击时，如果已有实例在运行，会打开插件引导页而不是再起一个。

## 文件位置

| 路径 | 内容 |
|---|---|
| `%APPDATA%\password-safe-client\config.json` | 连接配置（不含任何密码） |
| `%APPDATA%\password-safe-client\known_hosts` | 服务器主机密钥指纹（首次连接记录） |
| `%APPDATA%\password-safe-client\client.log` | 日志（1 MB 滚动，保留 3 份；不记录秘密） |
| `%APPDATA%\password-safe-client\control.json` | 本地控制端口与随机令牌（供原生消息使用） |
| `%APPDATA%\password-safe-client\com.tjut_lfl.password_safe.json` | 原生消息主机清单 |
| `%LOCALAPPDATA%\password-safe-client\extension` | 解压出来的插件（新版本原地更新，在扩展页点刷新即可） |

## 安全说明

- **主机密钥 TOFU**：第一次连接时记录服务器指纹；之后指纹变化会拒绝连接并提示。确认服务器确实重装过，再删掉 `known_hosts` 里对应那行。
- 私钥只在本机读取。加密的私钥需要在设置页输入口令，口令只在内存里，退出后要重新输入。
- 本地网页和控制接口只监听 `127.0.0.1` 的随机端口，并需要随机令牌；原生消息只接受固定 ID 的插件。
- 客户端不持有保险箱主密码，也不持有会话令牌。

## 限制

- 浏览器不允许程序静默安装扩展，第一次需要你按引导手动加载一次。
- Windows 的托盘界面没有在真实 Windows 上测试过（隧道逻辑在 Linux 上用自动化测试验证过）。
- macOS 版本无托盘（托盘需要 cgo），以无托盘模式运行，用设置页管理。
- Firefox 插件不含原生消息功能，弹窗里不显示隧道状态。
