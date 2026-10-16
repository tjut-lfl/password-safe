# 更新日志

## v0.1.0-beta（2026-10-16）
首个测试版，尚未经过安全审计。
- Argon2id KEK + AES-256-GCM DEK 加密保险箱，v1→v2 迁移
- CLI：增删改查、搜索、树形列表、TOTP、剪贴板、交互 shell、改主密码、校验、加密导出
- CSV 导入，兼容 Google 密码管理器导出格式
- 失败解锁指数退避锁定
- 仅 loopback 的 HTTP API 与 systemd 用户服务示例
- 浏览器扩展（MV3）：Chrome/Edge/Brave/Opera/Vivaldi 与 Firefox 115+
- `--version`、交叉编译脚本、GitHub Actions CI
