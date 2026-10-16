# Manual test: multi-step login / 手动测试：分步登录

Content scripts do not run on `file://`, so serve this folder over HTTP:
内容脚本不在 `file://` 上运行，请用 HTTP 打开：

```bash
cd extension/test && python3 -m http.server 8000
# open http://localhost:8000/multi-step-login.html
```

1. Unlock the extension; save an entry with URL `http://localhost:8000`, username `user@example.com`.
   解锁扩展，先保存一条 URL 为 `http://localhost:8000`、用户名 `user@example.com` 的条目。
2. **SPA mode**: the email field shows 🔒 → pick the entry → only the username is filled. Click *Next*;
   the password field appears and is filled automatically (toast “已为 … 填入密码”).
   **SPA 模式**：邮箱框旁出现 🔒，选条目只填用户名；点「下一步」后密码框出现并自动填入。
3. **Navigation mode** (`?mode=nav`): same as above, but step 2 is a new page load in the same tab.
   **跳转模式**：同上，第 2 步是同一标签页的新页面。
4. **Popup**: on step 1 click the entry in the popup → fills username; on step 2 the remembered entry is listed first, click it → fills password only (the hidden prefilled username is not overwritten).
   **弹窗**：第 1 步点条目只填用户名；第 2 步记住的条目排第一，点击只填密码，不覆盖隐藏的预填用户名。
5. **Save**: with no saved entry, type a new email, Next, type a password, Sign in → the save bar shows that email.
   **保存**：未保存过时，输入新邮箱→下一步→输入密码→登录，保存提示里应显示第 1 步的邮箱。
6. Wait > 5 minutes between steps → no auto-fill (memory expired). / 两步间隔超过 5 分钟不再自动填充。

## Automated smoke test / 自动冒烟测试
Uses a mock API on port 8787 and 8000 — stop your SSH tunnel first. / 使用 8787、8000 端口的模拟服务，先关闭 SSH 隧道。
```bash
npm i playwright-core && npx playwright-core install chromium
node extension/test/e2e.js   # expects: step1 user=…, step2 pw=…, save bar with the step-1 email
```
