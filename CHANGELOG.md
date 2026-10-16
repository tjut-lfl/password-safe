# 更新日志

## v0.2.0-beta（2026-10-16）
重点：导出选项与升级防丢失。
- `export --format`：encrypted（默认）、json-encrypted（可移植加密包，导出口令或 `--use-master`）、csv / csv-full / json / bitwarden（明文，需 `--plaintext` 并确认，文件 0600）
- `import` 自动识别以上全部格式及 Google/Chrome CSV，`--skip-duplicates` 按域名+用户名去重
- 自动加密备份：新版本首次运行、每日首次写入、migrate 前、restore 前；每类保留 10 份（`--keep-backups`）
- 新命令：`backup`、`backup list` / `backups`、`restore <文件>`（先校验再覆盖）
- 前向兼容：更新版本格式的保险箱只读打开、拒绝写入；保留未知 JSON 字段；检测外部修改，避免旧会话覆盖
- HTTP API：`POST /api/v1/export`（只返回加密 JSON 包）、`POST /api/v1/backup`；移除旧的 `GET /api/v1/export`
- 扩展：弹窗新增「导出备份」「服务端备份」
- 测试：v1 迁移、各格式往返、去重、备份轮转、恢复、只读保护、API 导出

## v0.1.0-beta（2026-10-16）
首个测试版，尚未经过安全审计。
- Argon2id KEK + AES-256-GCM DEK 加密保险箱，v1→v2 迁移
- CLI：增删改查、搜索、树形列表、TOTP、剪贴板、交互 shell、改主密码、校验、加密导出
- CSV 导入，兼容 Google 密码管理器导出格式
- 失败解锁指数退避锁定
- 仅 loopback 的 HTTP API 与 systemd 用户服务示例
- 浏览器扩展（MV3）：Chrome/Edge/Brave/Opera/Vivaldi 与 Firefox 115+
- `--version`、交叉编译脚本、GitHub Actions CI
