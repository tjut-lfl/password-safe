// 兼容 Firefox：缺少 chrome 命名空间时回退到 browser.*
if (typeof globalThis.chrome === 'undefined' && typeof globalThis.browser !== 'undefined') globalThis.chrome = globalThis.browser;
// 内容脚本：检测登录/注册表单，提供填充、生成、保存提示。从不记录密码。
(() => {
  'use strict';
  if (window.__pvLoaded) return; window.__pvLoaded = true;
  const host = location.hostname.replace(/^www\./, '').toLowerCase();
  const send = (msg) => new Promise(r => { try { chrome.runtime.sendMessage(msg, res => r(chrome.runtime.lastError ? { ok: false } : res || { ok: false })); } catch (_) { r({ ok: false }); } });

  const visible = el => !!(el && el.offsetParent !== null && !el.disabled && !el.readOnly && el.getBoundingClientRect().width > 0);
  const pwFields = (root = document) => [...root.querySelectorAll('input[type=password]')].filter(visible);
  function userFieldFor(pw) {
    const scope = pw.form || document;
    const inputs = [...scope.querySelectorAll('input')].filter(visible);
    const idx = inputs.indexOf(pw);
    for (let i = idx - 1; i >= 0; i--) {
      const t = (inputs[i].type || 'text').toLowerCase();
      if (['text', 'email', 'tel', ''].includes(t)) return inputs[i];
    }
    return scope.querySelector('input[autocomplete=username],input[type=email]');
  }
  function isSignup(pw) {
    const scope = pw.form || document;
    if (pw.autocomplete === 'new-password') return true;
    if (pwFields(scope).length >= 2) return true;
    const txt = ((scope.id || '') + ' ' + (scope.className || '') + ' ' + (scope.action || '') + ' ' + location.pathname + ' ' + (scope.innerText || '').slice(0, 500)).toLowerCase();
    return /sign.?up|register|registration|create.?account|join|注册|创建账号|新建账户/.test(txt) && !/sign.?in|log.?in|登录/.test(location.pathname.toLowerCase());
  }
  // 兼容 React/Vue：用原生 setter 赋值并派发事件
  function setValue(el, v) {
    if (!el) return;
    el.focus();
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
    setter.call(el, v);
    el.dispatchEvent(new Event('input', { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
    el.blur();
  }
  function fill(username, password) {
    const pws = pwFields();
    const pw = pws.find(p => !isSignup(p)) || pws[0];
    if (!pw) return false;
    const u = userFieldFor(pw);
    if (u && username) setValue(u, username);
    setValue(pw, password);
    return true;
  }
  function fillNew(password) {
    const pw = document.activeElement?.type === 'password' ? document.activeElement : pwFields()[0];
    if (!pw) return;
    const group = pwFields(pw.form || document);
    const targets = group.filter(p => p.autocomplete !== 'current-password');
    (targets.length ? targets : [pw]).forEach(p => setValue(p, password));
  }

  // ---- 内联图标与下拉菜单 ----
  let menu = null;
  const closeMenu = () => { menu?.remove(); menu = null; };
  document.addEventListener('click', e => { if (menu && !menu.contains(e.target) && !e.target.classList?.contains('pv-icon')) closeMenu(); }, true);
  function item(text, onClick, muted) { const d = document.createElement('div'); d.textContent = text; if (muted) d.className = 'pv-muted'; else d.addEventListener('click', e => { e.stopPropagation(); onClick(); closeMenu(); }); return d; }
  async function openMenu(pw, icon) {
    closeMenu();
    menu = document.createElement('div'); menu.className = 'pv-menu';
    const r = icon.getBoundingClientRect();
    menu.style.left = (r.left + scrollX - 200) + 'px'; menu.style.top = (r.bottom + scrollY + 4) + 'px';
    document.body.appendChild(menu);
    menu.appendChild(item('加载中…', null, true));
    const signup = isSignup(pw);
    const res = await send({ type: 'matchesFor', url: location.href });
    if (!menu) return;
    menu.textContent = '';
    if (signup) menu.appendChild(item('🔑 生成强密码并填入', async () => { const g = await send({ type: 'generate' }); if (g.ok) fillNew(g.data.password); }));
    if (!res.ok) menu.appendChild(item('无法连接保险箱服务', null, true));
    else if (res.data.locked) menu.appendChild(item('保险箱已锁定：点击工具栏图标解锁', null, true));
    else if (!res.data.entries.length) { if (!signup) menu.appendChild(item('此站点没有已保存的账号', null, true)); }
    else res.data.entries.forEach(e => menu.appendChild(item('👤 ' + (e.username || '(无用户名)') + ' — ' + e.name, () => send({ type: 'fillEntry', id: e.id }))));
  }
  const decorated = new WeakSet();
  function decorate() {
    pwFields().forEach(pw => {
      if (decorated.has(pw)) return; decorated.add(pw);
      const icon = document.createElement('div'); icon.className = 'pv-icon'; icon.textContent = '🔒'; icon.title = '密码保险箱';
      const place = () => { if (!pw.isConnected || !visible(pw)) { icon.style.display = 'none'; return; } const r = pw.getBoundingClientRect(); icon.style.display = ''; icon.style.left = (r.right + scrollX - 24) + 'px'; icon.style.top = (r.top + scrollY + (r.height - 18) / 2) + 'px'; };
      icon.addEventListener('mousedown', e => { e.preventDefault(); e.stopPropagation(); openMenu(pw, icon); });
      document.body.appendChild(icon); place();
      addEventListener('resize', place); addEventListener('scroll', place, true);
      setInterval(place, 1500);
    });
  }

  // ---- 提交检测与保存提示 ----
  let lastCapture = null;
  function capture(scope) {
    const pws = pwFields(scope || document).concat(scope ? [] : []);
    const all = pws.filter(p => p.value);
    if (!all.length) return;
    const signup = isSignup(all[0]);
    // 改密表单：取最后一个（新密码）；登录：取第一个
    const pw = signup ? all[all.length - 1] : all[0];
    const u = userFieldFor(all[0]);
    const cred = { host, username: u ? u.value.trim() : '', password: pw.value, url: location.origin };
    if (lastCapture && lastCapture.password === cred.password && lastCapture.username === cred.username) return;
    lastCapture = cred;
    send({ type: 'offerSave', ...cred });
  }
  document.addEventListener('submit', e => capture(e.target), true);
  document.addEventListener('click', e => {
    const b = e.target.closest?.('button,input[type=submit],[role=button]');
    if (b && pwFields().some(p => p.value)) capture(b.form || null);
  }, true);
  document.addEventListener('keydown', e => { if (e.key === 'Enter' && e.target?.tagName === 'INPUT' && pwFields().some(p => p.value)) capture(e.target.form || null); }, true);

  function showSaveBar(cred) {
    document.querySelector('.pv-bar')?.remove();
    const bar = document.createElement('div'); bar.className = 'pv-bar';
    const t = document.createElement('div');
    t.textContent = (cred.isUpdate ? '更新' : '保存') + '到密码保险箱？' + (cred.username ? '（' + cred.username + ' @ ' + cred.host + '）' : '（' + cred.host + '）');
    const ok = document.createElement('button'); ok.textContent = cred.isUpdate ? '更新' : '保存';
    const no = document.createElement('button'); no.textContent = '不了'; no.className = 'pv-sec';
    const msg = document.createElement('div');
    ok.onclick = async () => {
      ok.disabled = true;
      const r = await send({ type: 'confirmSave', ...cred });
      if (r.ok) { msg.textContent = r.data.updated ? '已更新：' + r.data.name : '已保存：' + r.data.name; setTimeout(() => bar.remove(), 2000); }
      else { msg.textContent = r.error === 'LOCKED' ? '保险箱已锁定，请先点击工具栏图标解锁后再点保存。' : '保存失败：' + r.error; ok.disabled = false; }
    };
    no.onclick = () => bar.remove();
    bar.append(t, ok, no, msg); document.body.appendChild(bar);
    setTimeout(() => bar.isConnected && !ok.disabled && bar.remove(), 60000);
  }
  // 页面跳转后（同标签页）取回待保存的凭据并提示
  async function checkPending() { const r = await send({ type: 'takePending' }); if (r.ok && r.data) showSaveBar(r.data); }
  // 单页应用不跳转时也提示
  const origSend = send;
  chrome.runtime.onMessage.addListener((m, sender, reply) => {
    if (sender.id !== chrome.runtime.id) return;
    if (m.type === 'pvFill') reply({ ok: fill(m.username, m.password) });
    if (m.type === 'pvGenerateFill') { fillNew(m.password); reply({ ok: true }); }
  });
  setTimeout(() => { if (lastCapture) checkPending(); }, 0);
  document.addEventListener('submit', () => setTimeout(checkPending, 1500), true);
  addEventListener('click', () => { if (lastCapture) setTimeout(checkPending, 2000); }, true);

  checkPending();
  decorate();
  new MutationObserver(() => decorate()).observe(document.documentElement, { childList: true, subtree: true });
  void origSend;
})();
