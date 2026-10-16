// 兼容 Firefox：缺少 chrome 命名空间时回退到 browser.*
if (typeof globalThis.chrome === 'undefined' && typeof globalThis.browser !== 'undefined') globalThis.chrome = globalThis.browser;
// 后台 Service Worker：唯一与保险箱服务通信的地方。令牌只存 chrome.storage.session（内存，不落盘）。
'use strict';
const DEFAULT_BASE = 'http://127.0.0.1:8787';
const TTL_MS = 30 * 60 * 1000; // 与服务端 token TTL 一致

chrome.storage.session.setAccessLevel?.({ accessLevel: 'TRUSTED_CONTEXTS' });

async function baseURL() {
  const { serverUrl } = await chrome.storage.local.get('serverUrl');
  return (serverUrl || DEFAULT_BASE).replace(/\/+$/, '');
}
async function getSession() {
  const s = await chrome.storage.session.get(['token', 'tokenAt']);
  if (!s.token) return null;
  if (Date.now() - s.tokenAt > TTL_MS) { await clearSession(); return null; }
  return s.token;
}
async function clearSession() { await chrome.storage.session.remove(['token', 'tokenAt']); }

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
  } catch (e) { throw new Error('无法连接服务（SSH 隧道是否已建立？）'); }
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
async function entriesForHost(host) {
  const { entries = [] } = await api('/api/v1/entries');
  return entries.filter(e => matches(e, host));
}
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
  const same = entries.find(e => matches(e, host) && (e.username || '') === (username || ''));
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
    await chrome.storage.session.set({ token: d.token, tokenAt: Date.now() });
    return { ok: true };
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
  async fillEntry({ id, tabId }, sender) {
    const e = await api('/api/v1/entries/' + encodeURIComponent(id));
    const target = tabId ?? sender.tab?.id;
    await chrome.tabs.sendMessage(target, { type: 'pvFill', username: e.username, password: e.password });
    return { ok: true };
  },
  async generate({ length = 20 } = {}) {
    try { const d = await api('/api/v1/generate', { method: 'POST', body: { length, symbols: true } }); return { password: d.password, source: 'server' }; }
    catch (_) { return { password: localGenerate(length), source: 'local' }; }
  },
  async offerSave(cred, sender) {
    // 已有相同凭据则不打扰
    if (await getSession()) {
      try {
        const list = await entriesForHost(cred.host);
        const same = list.find(e => (e.username || '') === (cred.username || ''));
        if (same) {
          const full = await api('/api/v1/entries/' + encodeURIComponent(same.id));
          if (full.password === cred.password) return { skip: true };
          cred.isUpdate = true;
        }
      } catch (_) {}
    }
    await setPending(sender.tab.id, cred);
    return { ok: true };
  },
  async exportBundle({ passphrase }) {
    if (!passphrase || passphrase.length < 8) throw new Error('导出口令至少 8 位');
    const d = await api('/api/v1/export', { method: 'POST', body: { passphrase } });
    if (d.format !== 'password-safe-export') throw new Error('服务端返回的不是加密导出包');
    return { json: JSON.stringify(d, null, 2), entries: d.entries || 0 };
  },
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
