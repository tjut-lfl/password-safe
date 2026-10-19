// 兼容 Firefox：缺少 chrome 命名空间时回退到 browser.*
if (typeof globalThis.chrome === 'undefined' && typeof globalThis.browser !== 'undefined') globalThis.chrome = globalThis.browser;
// 后台 Service Worker：唯一与保险箱服务通信的地方。令牌只存 chrome.storage.session（内存，不落盘）。
'use strict';
const DEFAULT_BASE = 'http://127.0.0.1:8787';
const TTL_MS = 6 * 60 * 60 * 1000; // 默认与服务端一致；优先用 unlock 返回的 expires_in

chrome.storage.session.setAccessLevel?.({ accessLevel: 'TRUSTED_CONTEXTS' });

async function baseURL() {
  const { serverUrl } = await chrome.storage.local.get('serverUrl');
  return (serverUrl || DEFAULT_BASE).replace(/\/+$/, '');
}
/** 解析 unlock 的 expires_in（Go duration 如 6h / 30m0s，或纯秒数字）。失败返回 null。 */
function parseExpiresInMs(v) {
  if (v == null) return null;
  if (typeof v === 'number' && Number.isFinite(v) && v > 0) return Math.round(v * 1000);
  const s = String(v).trim();
  if (!s) return null;
  if (/^\d+(\.\d+)?$/.test(s)) return Math.round(parseFloat(s) * 1000);
  let ms = 0, matched = false;
  const re = /(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/gi;
  let m;
  while ((m = re.exec(s))) {
    matched = true;
    const n = parseFloat(m[1]);
    switch (m[2].toLowerCase()) {
      case 'h': ms += n * 3600000; break;
      case 'm': ms += n * 60000; break;
      case 's': ms += n * 1000; break;
      case 'ms': ms += n; break;
      default: break; // ns/us 忽略
    }
  }
  return matched && ms > 0 ? Math.round(ms) : null;
}
async function getSession() {
  const s = await chrome.storage.session.get(['token', 'tokenAt', 'ttlMs']);
  if (!s.token) return null;
  const ttl = (typeof s.ttlMs === 'number' && s.ttlMs > 0) ? s.ttlMs : TTL_MS;
  if (Date.now() - s.tokenAt > ttl) { await clearSession(); return null; }
  return s.token;
}
async function clearSession() { await chrome.storage.session.remove(['token', 'tokenAt', 'ttlMs']); }

async function api(path, { method = 'GET', body, auth = true } = {}) {
  const headers = {};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (auth) {
    const t = await getSession();
    if (!t) throw new Error('LOCKED');
    headers['Authorization'] = 'Bearer ' + t;
  }
  let res;
  try {
    res = await fetch((await baseURL()) + path, { method, headers, body: body !== undefined ? JSON.stringify(body) : undefined, credentials: 'omit', cache: 'no-store' });
  } catch (e) { throw new Error('无法连接服务：请启动 Password Safe 客户端（或确认 SSH 隧道已建立）'); }
  let data = {};
  try { data = await res.json(); } catch (_) {}
  if (res.status === 401 && auth) { await clearSession(); throw new Error('LOCKED'); }
  if (!res.ok) throw new Error(data.error || ('HTTP ' + res.status));
  return data;
}

function hostOf(u) { try { return new URL(u).hostname.replace(/^www\./, '').toLowerCase(); } catch (_) { return ''; } }
function matches(entry, host) {
  if (!host) return false;
  const cands = [hostOf(entry.url || ''), (entry.name || '').toLowerCase()];
  return cands.some(c => {
    if (!c) return false;
    const h = c.replace(/^www\./, '');
    return h === host || host.endsWith('.' + h) || h.endsWith('.' + host) || h.split('/').includes(host);
  });
}
// 可注册域名（启发式，无公共后缀表）+ 常见多域名登录的同站别名
const MULTI_TLD = /\.(com|net|org|gov|edu|co|ac)\.[a-z]{2}$|\.(co\.uk|com\.cn|com\.au|co\.jp)$/;
const SITE_ALIASES = {
  'live.com': 'microsoft', 'microsoftonline.com': 'microsoft', 'microsoft.com': 'microsoft', 'office.com': 'microsoft', 'outlook.com': 'microsoft',
  'google.com': 'google', 'youtube.com': 'google', 'gmail.com': 'google',
  'apple.com': 'apple', 'icloud.com': 'apple',
  'amazon.com': 'amazon', 'aws.amazon.com': 'amazon', 'amazonaws.com': 'amazon', 'awsapps.com': 'amazon',
};
function registrable(host) {
  host = (host || '').replace(/^www\./, '').toLowerCase();
  if (!host || /^[\d.]+$/.test(host) || !host.includes('.')) return host;
  const parts = host.split('.');
  const n = MULTI_TLD.test(host) ? 3 : 2;
  return parts.slice(-n).join('.');
}
function siteKey(host) { const r = registrable(host); return SITE_ALIASES[r] || r; }
function matchesSite(entry, host) {
  if (matches(entry, host)) return true;
  const k = siteKey(host); if (!k) return false;
  const eh = hostOf(entry.url || '') || (/^[a-z0-9.-]+\.[a-z]{2,}$/.test((entry.name || '').split('/')[0].toLowerCase()) ? entry.name.split('/')[0].toLowerCase() : '');
  return !!eh && siteKey(eh) === k;
}
async function entriesForHost(host) {
  const { entries = [] } = await api('/api/v1/entries');
  return entries.filter(e => matchesSite(e, host));
}

// 分步登录：记住本标签页在第 1 步选择的条目 / 输入的用户名（只存会话内存，5 分钟过期；不存密码）
const STEP_TTL_MS = 5 * 60 * 1000;
async function setStep(tabId, host, data) {
  if (tabId == null) return;
  const k = 'step_' + tabId;
  const old = (await chrome.storage.session.get(k))[k];
  const site = siteKey(host);
  const base = old && old.site === site && Date.now() - old.at < STEP_TTL_MS ? old : {};
  await chrome.storage.session.set({ [k]: { ...base, ...data, site, at: Date.now() } });
}
async function getStep(tabId, host) {
  if (tabId == null) return null;
  const k = 'step_' + tabId;
  const s = (await chrome.storage.session.get(k))[k];
  if (!s) return null;
  if (Date.now() - s.at > STEP_TTL_MS || s.site !== siteKey(host)) { await chrome.storage.session.remove(k); return null; }
  return s;
}
chrome.tabs?.onRemoved?.addListener(id => chrome.storage.session.remove(['step_' + id, 'pending_' + id]));
function localGenerate(len = 20) {
  const sets = ['abcdefghijkmnopqrstuvwxyz', 'ABCDEFGHJKLMNPQRSTUVWXYZ', '23456789', '!@#$%^&*-_=+?'];
  const all = sets.join('');
  const rnd = n => { const a = new Uint32Array(1); const lim = Math.floor(0x100000000 / n) * n; do crypto.getRandomValues(a); while (a[0] >= lim); return a[0] % n; };
  const out = sets.map(s => s[rnd(s.length)]);
  while (out.length < len) out.push(all[rnd(all.length)]);
  for (let i = out.length - 1; i > 0; i--) { const j = rnd(i + 1); [out[i], out[j]] = [out[j], out[i]]; }
  return out.join('');
}

// 待保存凭据（按标签页），仅在内存会话存储中短暂保留
async function setPending(tabId, cred) { await chrome.storage.session.set({ ['pending_' + tabId]: cred }); }
async function popPending(tabId) { const k = 'pending_' + tabId; const r = await chrome.storage.session.get(k); await chrome.storage.session.remove(k); return r[k]; }

async function saveCredential({ host, username, password, url }) {
  const { entries = [] } = await api('/api/v1/entries');
  const same = entries.find(e => matchesSite(e, host) && (e.username || '') === (username || ''));
  if (same) {
    await api('/api/v1/entries/' + encodeURIComponent(same.id), { method: 'PUT', body: { password, url: same.url || url } });
    return { updated: true, name: same.name };
  }
  let name = host;
  if (entries.some(e => e.name === name)) name = host + '/' + (username || 'account');
  let i = 2; const baseName = name;
  while (entries.some(e => e.name === name)) name = baseName + '-' + (i++);
  await api('/api/v1/entries', { method: 'POST', body: { name, username, password, url, tags: ['browser'] } });
  return { updated: false, name };
}

const NATIVE_HOST = 'com.tjut_lfl.password_safe';
async function nativeCall(cmd) {
  if (!chrome.runtime.sendNativeMessage) return { available: false };
  try {
    const r = await chrome.runtime.sendNativeMessage(NATIVE_HOST, { cmd });
    if (!r || !r.ok) return { available: true, running: false, error: (r && r.error) || 'no_response' };
    return { available: true, running: true, status: r.status };
  } catch (e) {
    // 未安装客户端 / 未注册原生主机
    return { available: false, error: String(e && e.message || e) };
  }
}

const handlers = {
  async status() {
    const base = await baseURL();
    let reachable = false;
    try { const r = await fetch(base + '/health', { cache: 'no-store' }); reachable = r.ok; } catch (_) {}
    return { reachable, unlocked: !!(await getSession()), base };
  },
  async testConnection({ url }) {
    const r = await fetch(url.replace(/\/+$/, '') + '/api/v1/status', { cache: 'no-store' });
    if (!r.ok) throw new Error('HTTP ' + r.status);
    return await r.json();
  },
  async unlock({ password }) {
    const d = await api('/api/v1/unlock', { method: 'POST', body: { password }, auth: false });
    const ttlMs = parseExpiresInMs(d.expires_in_sec) || parseExpiresInMs(d.expires_in) || TTL_MS;
    await chrome.storage.session.set({ token: d.token, tokenAt: Date.now(), ttlMs });
    return { ok: true, expires_in: d.expires_in, ttlMs };
  },
  async lock() {
    try { await api('/api/v1/lock', { method: 'POST' }); } catch (_) {}
    await clearSession();
    return { ok: true };
  },
  async search({ q }) {
    const d = await api(q ? '/api/v1/search?q=' + encodeURIComponent(q) : '/api/v1/entries');
    return d.entries || [];
  },
  async matchesFor({ url }, sender) {
    if (!(await getSession())) return { locked: true, entries: [] };
    return { locked: false, entries: await entriesForHost(hostOf(url || sender.tab?.url)) };
  },
  async fillEntry({ id, tabId, url }, sender) {
    const e = await api('/api/v1/entries/' + encodeURIComponent(id));
    const target = tabId ?? sender.tab?.id;
    const r = await chrome.tabs.sendMessage(target, { type: 'pvFill', username: e.username, password: e.password });
    // 记住所选条目，下一步（密码页）可直接填
    await setStep(target, hostOf(url || sender.tab?.url || ''), { entryId: e.id, username: e.username || '' });
    if (r && r.ok === false) throw new Error('此页面没有可填写的账号/密码输入框');
    return { ok: true, step: r && r.step };
  },
  // 密码页：取第 1 步记住的条目
  async stepInfo({ url, tabId }, sender) {
    const t = tabId ?? sender.tab?.id;
    const s = await getStep(t, hostOf(url || sender.tab?.url || ''));
    return s ? { entryId: s.entryId || null, username: s.username || '' } : null;
  },
  async stepCredential({ url }, sender) {
    const s = await getStep(sender.tab?.id, hostOf(url || sender.tab?.url || ''));
    if (!s || !s.entryId || !(await getSession())) return null;
    const e = await api('/api/v1/entries/' + encodeURIComponent(s.entryId));
    return { username: e.username || '', password: e.password };
  },
  async rememberUsername({ username, url }, sender) {
    if (!username) return { ok: false };
    await setStep(sender.tab?.id, hostOf(url || sender.tab?.url || ''), { typedUsername: username });
    return { ok: true };
  },
  async generate({ length = 20 } = {}) {
    try { const d = await api('/api/v1/generate', { method: 'POST', body: { length, symbols: true } }); return { password: d.password, source: 'server' }; }
    catch (_) { return { password: localGenerate(length), source: 'local' }; }
  },
  async offerSave(cred, sender) {
    // 分步登录：密码页没有用户名时，补上第 1 步输入/选择的用户名
    if (!cred.username) {
      const s = await getStep(sender.tab?.id, cred.host);
      if (s) cred.username = s.typedUsername || s.username || '';
    }
    // 先写入 pending，再做可能较慢的条目比对，避免 SPA 登录时内容脚本抢先 takePending 拿到空
    if (sender.tab?.id != null) await setPending(sender.tab.id, cred);
    if (await getSession()) {
      try {
        const list = await entriesForHost(cred.host);
        const same = list.find(e => (e.username || '') === (cred.username || ''));
        if (same) {
          const full = await api('/api/v1/entries/' + encodeURIComponent(same.id));
          if (full.password === cred.password) {
            if (sender.tab?.id != null) await popPending(sender.tab.id);
            return { skip: true };
          }
          cred.isUpdate = true;
          if (sender.tab?.id != null) await setPending(sender.tab.id, cred);
        }
      } catch (_) {}
    }
    return { ok: true, isUpdate: !!cred.isUpdate };
  },
  async exportBundle({ passphrase }) {
    if (!passphrase || passphrase.length < 8) throw new Error('导出口令至少 8 位');
    const d = await api('/api/v1/export', { method: 'POST', body: { format: 'json-encrypted', passphrase } });
    if (d.format !== 'password-safe-export') throw new Error('服务端返回的不是加密导出包');
    return { json: JSON.stringify(d, null, 2), entries: d.entries || 0 };
  },
  async exportCSV() {
    const t = await getSession();
    if (!t) throw new Error('LOCKED');
    const base = await baseURL();
    let res;
    try {
      res = await fetch(base + '/api/v1/export', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + t },
        body: JSON.stringify({ format: 'csv', confirm: 'EXPORT' }),
        credentials: 'omit',
        cache: 'no-store',
      });
    } catch (_) {
      throw new Error('无法连接服务：请启动 Password Safe 客户端（或确认 SSH 隧道已建立）');
    }
    if (res.status === 401) { await clearSession(); throw new Error('LOCKED'); }
    const textBody = await res.text();
    if (!res.ok) {
      let msg = 'HTTP ' + res.status;
      try { msg = JSON.parse(textBody).error || msg; } catch (_) {}
      throw new Error(msg);
    }
    const lines = textBody.split(/\r?\n/).filter(l => l.length);
    return { csv: textBody, entries: Math.max(0, lines.length - 1) };
  },
  // 桌面客户端（原生消息）：隧道状态 / 一键重连。Firefox 版无此权限时返回 available:false
  async clientStatus() { return await nativeCall('status'); },
  async clientReconnect() { return await nativeCall('reconnect'); },
  async clientGuide() { return await nativeCall('guide'); },
  async backupNow() { return await api('/api/v1/backup', { method: 'POST', body: {} }); },
  async takePending(_m, sender) { return (await popPending(sender.tab.id)) || null; },
  async confirmSave(cred) { return await saveCredential(cred); },
};

chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  // 只接受本扩展自身的消息
  if (sender.id !== chrome.runtime.id) return;
  const h = handlers[msg && msg.type];
  if (!h) return;
  Promise.resolve(h(msg, sender)).then(r => sendResponse({ ok: true, data: r }), e => sendResponse({ ok: false, error: e.message }));
  return true;
});
