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
  // ---- 分步登录：只有用户名、没有密码框的页面 ----
  const USER_HINT = /user|email|e-mail|mail|login|account|acct|phone|mobile|identifier|signin|账号|帐号|用户|邮箱|手机/i;
  const NEXT_HINT = /next|continue|proceed|weiter|suivant|siguiente|下一步|继续|繼續|次へ|다음|sign.?in|log.?in|登录|登入/i;
  function looksLikeUser(el) {
    const t = (el.type || 'text').toLowerCase();
    if (!['text', 'email', 'tel', ''].includes(t)) return false;
    const ac = (el.autocomplete || '').toLowerCase();
    if (/username|email|webauthn/.test(ac)) return true;
    if (/one-time-code|off|search/.test(ac) && !USER_HINT.test(el.name + el.id)) return false;
    if (t === 'email') return true;
    return USER_HINT.test([el.name, el.id, el.placeholder, el.getAttribute('aria-label') || ''].join(' '));
  }
  function hasNextButton(scope) {
    return [...scope.querySelectorAll('button,input[type=submit],input[type=button],[role=button]')].filter(visible)
      .some(b => NEXT_HINT.test((b.innerText || b.value || b.getAttribute('aria-label') || '') + ' ' + (b.id || '')));
  }
  function userOnlyFields() {
    if (pwFields().length) return [];
    return [...document.querySelectorAll('input')].filter(el => visible(el) && looksLikeUser(el))
      .filter(el => el.autocomplete === 'username' || hasNextButton(el.form || document))
      .slice(0, 1);
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
  // 返回 {ok, step}：step = 'both' | 'username'（第 1 步）| 'password'（第 2 步）
  function fill(username, password) {
    const pws = pwFields();
    const pw = pws.find(p => !isSignup(p)) || pws[0];
    if (!pw) {
      const u = userOnlyFields()[0];
      if (!u || !username) return { ok: false };
      setValue(u, username);
      return { ok: true, step: 'username' };
    }
    const u = userFieldFor(pw);
    // 密码页上站点预填的用户名（含隐藏字段）不覆盖；只填可见且为空/不同的用户名框
    if (u && username && visible(u) && u.value.trim() !== username && !(u.value && u.readOnly)) setValue(u, username);
    setValue(pw, password);
    return { ok: true, step: u && visible(u) ? 'both' : 'password' };
  }
  // 密码框出现时：若本标签页第 1 步已选过条目且密码框为空，自动填入
  const autoFilled = new WeakSet();
  async function autoFillStep(pw) {
    if (autoFilled.has(pw) || isSignup(pw)) return;
    autoFilled.add(pw);
    const r = await send({ type: 'stepCredential', url: location.href });
    if (!r.ok || !r.data || !r.data.password) return;
    if (!pw.isConnected || !visible(pw) || pw.value) return;
    setValue(pw, r.data.password);
    toast('已为 ' + (r.data.username || host) + ' 填入密码', 2000);
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
    const signup = pw.type === 'password' && isSignup(pw);
    const [res, st] = await Promise.all([send({ type: 'matchesFor', url: location.href }), send({ type: 'stepInfo', url: location.href })]);
    if (!menu) return;
    menu.textContent = '';
    if (res.ok && st.ok && st.data && st.data.entryId && pw.type === 'password') {
      const e = res.data.entries.find(x => x.id === st.data.entryId);
      if (e) res.data.entries = [e, ...res.data.entries.filter(x => x !== e)];
    }
    if (signup) menu.appendChild(item('🔑 生成强密码并填入', async () => { const g = await send({ type: 'generate' }); if (g.ok) fillNew(g.data.password); }));
    if (!res.ok) menu.appendChild(item('无法连接保险箱服务', null, true));
    else if (res.data.locked) menu.appendChild(item('保险箱已锁定：点击工具栏图标解锁', null, true));
    else if (!res.data.entries.length) { if (!signup) menu.appendChild(item('此站点没有已保存的账号', null, true)); }
    else res.data.entries.forEach(e => menu.appendChild(item('👤 ' + (e.username || '(无用户名)') + ' — ' + e.name, () => send({ type: 'fillEntry', id: e.id }))));
  }
  const decorated = new WeakSet();
  function decorate() {
    pwFields().forEach(pw => autoFillStep(pw));
    pwFields().concat(userOnlyFields()).forEach(pw => {
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
    let u = userFieldFor(all[0]);
    if (!u || !u.value) u = (all[0].form || document).querySelector('input[autocomplete=username],input[name*=user i],input[name*=email i],input[type=email]') || u;
    const cred = { host, username: u ? u.value.trim() : '', password: pw.value, url: location.origin };
    if (lastCapture && lastCapture.password === cred.password && lastCapture.username === cred.username) return;
    lastCapture = cred;
    send({ type: 'offerSave', ...cred });
  }
  function captureUser(scope) {
    if (pwFields().length) return;
    const u = userOnlyFields().find(el => !scope || !el.form || el.form === scope) || userOnlyFields()[0];
    if (u && u.value.trim()) send({ type: 'rememberUsername', username: u.value.trim(), url: location.href });
  }
  document.addEventListener('submit', e => { captureUser(e.target); capture(e.target); }, true);
  document.addEventListener('click', e => { if (e.target.closest?.('.pv-bar,.pv-menu,.pv-icon')) return; if (e.target.closest?.('button,input[type=submit],[role=button]')) captureUser(null); }, true);
  document.addEventListener('keydown', e => { if (e.key === 'Enter' && e.target?.tagName === 'INPUT') captureUser(e.target.form || null); }, true);
  document.addEventListener('click', e => {
    if (e.target.closest?.('.pv-bar,.pv-menu,.pv-icon')) return;
    const b = e.target.closest?.('button,input[type=submit],[role=button]');
    if (b && pwFields().some(p => p.value)) capture(b.form || null);
  }, true);
  document.addEventListener('keydown', e => { if (e.key === 'Enter' && e.target?.tagName === 'INPUT' && pwFields().some(p => p.value)) capture(e.target.form || null); }, true);

  const handled = new Set();
  const credKey = c => c.host + '\u0000' + c.username + '\u0000' + c.password;
  function withTimeout(p, ms) {
    return Promise.race([p, new Promise(r => setTimeout(() => r({ ok: false, error: '超时，请检查 SSH 隧道和服务端' }), ms))]);
  }
  function toast(text, ms) {
    const t = document.createElement('div'); t.className = 'pv-bar'; t.textContent = text;
    document.body.appendChild(t); setTimeout(() => t.remove(), ms || 2500);
  }
  function showSaveBar(cred) {
    const key = credKey(cred);
    if (handled.has(key)) return;
    document.querySelectorAll('.pv-bar').forEach(b => b.remove());
    const bar = document.createElement('div'); bar.className = 'pv-bar';
    const t = document.createElement('div');
    t.textContent = (cred.isUpdate ? '更新' : '保存') + '到密码保险箱？' + (cred.username ? '（' + cred.username + ' @ ' + cred.host + '）' : '（' + cred.host + '）');
    const ok = document.createElement('button'); ok.textContent = cred.isUpdate ? '更新' : '保存';
    const no = document.createElement('button'); no.textContent = '不了'; no.className = 'pv-sec';
    const close = () => { handled.add(key); bar.remove(); };
    ok.onclick = async e => {
      e.stopPropagation();
      ok.disabled = true; no.disabled = true; ok.textContent = '保存中…';
      let r;
      try { r = await withTimeout(send({ type: 'confirmSave', ...cred }), 10000); }
      catch (err) { r = { ok: false, error: String(err && err.message || err) }; }
      if (r && r.ok) {
        close();
        toast(r.data && r.data.updated ? '已更新：' + r.data.name : '已保存：' + ((r.data && r.data.name) || cred.host));
      } else {
        const err = r && r.error;
        if (err === 'LOCKED') {
          // 保持待保存状态，解锁后可再次提示
          bar.remove();
          toast('保险箱已锁定：请点工具栏图标解锁，然后重新提交一次表单即可保存。', 5000);
        } else {
          close();
          toast('保存失败：' + (err || '未知错误'), 5000);
        }
      }
    };
    no.onclick = e => { e.stopPropagation(); close(); };
    bar.append(t, ok, no); document.body.appendChild(bar);
    setTimeout(() => { if (bar.isConnected && !ok.disabled) bar.remove(); }, 60000);
  }
  // 页面跳转后（同标签页）取回待保存的凭据并提示
  async function checkPending() { const r = await send({ type: 'takePending' }); if (r.ok && r.data) showSaveBar(r.data); }
  // 单页应用不跳转时也提示
  const origSend = send;
  chrome.runtime.onMessage.addListener((m, sender, reply) => {
    if (sender.id !== chrome.runtime.id) return;
    if (m.type === 'pvFill') reply(fill(m.username, m.password));
    if (m.type === 'pvGenerateFill') { fillNew(m.password); reply({ ok: true }); }
  });
  setTimeout(() => { if (lastCapture) checkPending(); }, 0);
  document.addEventListener('submit', () => setTimeout(checkPending, 1500), true);
  addEventListener('click', e => { if (e.target.closest?.('.pv-bar,.pv-menu,.pv-icon')) return; if (lastCapture) setTimeout(checkPending, 2000); }, true);

  checkPending();
  decorate();
  new MutationObserver(() => decorate()).observe(document.documentElement, { childList: true, subtree: true });
  void origSend;
})();
