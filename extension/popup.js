// 兼容 Firefox：缺少 chrome 命名空间时回退到 browser.*
if (typeof globalThis.chrome === 'undefined' && typeof globalThis.browser !== 'undefined') globalThis.chrome = globalThis.browser;
'use strict';
const $ = id => document.getElementById(id);
const send = m => new Promise(r => chrome.runtime.sendMessage(m, x => r(x || { ok: false, error: '无响应' })));
let tab = null;
function showErr(e) { $('msg').textContent = e === 'LOCKED' ? '会话已过期，请重新解锁' : (e || ''); if (e === 'LOCKED') render(); }
function li(e) {
  const el = document.createElement('li');
  el.textContent = e.name; const s = document.createElement('small'); s.textContent = e.username || '(无用户名)'; el.appendChild(s);
  el.onclick = async () => { const r = await send({ type: 'fillEntry', id: e.id, tabId: tab.id }); if (r.ok) window.close(); else showErr(r.error.includes('Receiving end') ? '此页面无法填充' : r.error); };
  return el;
}
function list(ul, entries, empty) { ul.textContent = ''; if (!entries.length) { const x = document.createElement('li'); x.className = 'muted'; x.textContent = empty; ul.appendChild(x); } entries.forEach(e => ul.appendChild(li(e))); }
async function render() {
  [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  const st = await send({ type: 'status' });
  const s = st.data || {};
  $('state').textContent = !s.reachable ? '⚠ 服务不可达' : s.unlocked ? '🔓 已解锁' : '🔒 已锁定';
  if (!s.reachable) { $('msg').textContent = '连不上 ' + (s.base || '') + '，请确认 SSH 隧道和服务端 serve 已运行。'; }
  $('unlockView').hidden = !!s.unlocked; $('mainView').hidden = !s.unlocked;
  if (s.unlocked) {
    const m = await send({ type: 'matchesFor', url: tab?.url });
    let host = ''; try { host = new URL(tab.url).hostname; } catch (_) {}
    $('siteTitle').textContent = '当前网站 ' + host;
    if (m.ok) list($('siteList'), m.data.entries, '没有匹配的条目'); else showErr(m.error);
    $('q').focus();
  } else $('master').focus();
}
$('unlockBtn').onclick = async () => {
  const password = $('master').value; if (!password) return;
  $('unlockBtn').disabled = true;
  const r = await send({ type: 'unlock', password });
  $('master').value = ''; $('unlockBtn').disabled = false;
  if (!r.ok) return showErr('解锁失败：' + r.error);
  $('msg').textContent = ''; render();
};
$('master').addEventListener('keydown', e => { if (e.key === 'Enter') $('unlockBtn').click(); });
$('lockBtn').onclick = async () => { await send({ type: 'lock' }); render(); };
let timer;
$('q').addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(async () => { const q = $('q').value.trim(); if (!q) { $('searchList').textContent = ''; return; } const r = await send({ type: 'search', q }); if (r.ok) list($('searchList'), r.data, '无结果'); else showErr(r.error); }, 250); });
$('genBtn').onclick = async () => {
  const g = await send({ type: 'generate' }); if (!g.ok) return showErr(g.error);
  try { await chrome.tabs.sendMessage(tab.id, { type: 'pvGenerateFill', password: g.data.password }); $('genOut').textContent = '已填入（来源：' + (g.data.source === 'server' ? '服务端' : '本地') + '）。提交表单后会询问是否保存。'; }
  catch (_) { showErr('此页面无法填充'); }
};
$('exportBtn').onclick = () => { $('exportBox').hidden = !$('exportBox').hidden; $('exPass').focus(); };
$('exCancel').onclick = () => { $('exPass').value = $('exPass2').value = ''; $('exportBox').hidden = true; };
$('exGo').onclick = async () => {
  const p = $('exPass').value, p2 = $('exPass2').value;
  if (p.length < 8) return showErr('导出口令至少 8 位');
  if (p !== p2) return showErr('两次输入的口令不一致');
  $('exGo').disabled = true;
  const r = await send({ type: 'exportBundle', passphrase: p });
  $('exGo').disabled = false; $('exPass').value = $('exPass2').value = '';
  if (!r.ok) return showErr('导出失败：' + r.error);
  const ts = new Date().toISOString().replace(/[-:]/g, '').replace('T', '-').slice(0, 15);
  const url = URL.createObjectURL(new Blob([r.data.json], { type: 'application/json' }));
  const a = document.createElement('a'); a.href = url; a.download = 'password-safe-export-' + ts + '.json';
  document.body.appendChild(a); a.click(); a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 60000);
  $('exportBox').hidden = true; $('msg').textContent = '';
  $('exOut').textContent = '已下载加密备份（' + r.data.entries + ' 条）。请妥善保存并记住口令。';
};
$('backupBtn').onclick = async () => {
  const r = await send({ type: 'backupNow' });
  if (!r.ok) return showErr('备份失败：' + r.error);
  $('exOut').textContent = '服务端已备份：' + r.data.file;
};
render();
