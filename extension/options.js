// 兼容 Firefox：缺少 chrome 命名空间时回退到 browser.*
if (typeof globalThis.chrome === 'undefined' && typeof globalThis.browser !== 'undefined') globalThis.chrome = globalThis.browser;
'use strict';
const $ = id => document.getElementById(id);
chrome.storage.local.get('serverUrl').then(r => { $('url').value = r.serverUrl || 'http://127.0.0.1:8787'; });
function norm() { const u = new URL($('url').value.trim()); if (!/^https?:$/.test(u.protocol)) throw new Error('只支持 http/https'); return u.origin; }
async function ensurePerm(origin) {
  const h = new URL(origin).hostname;
  if (h === '127.0.0.1' || h === 'localhost') return true;
  return chrome.permissions.request({ origins: [origin + '/*'] });
}
$('save').onclick = async () => {
  try { const o = norm(); if (!(await ensurePerm(o))) throw new Error('未授予访问该地址的权限'); await chrome.storage.local.set({ serverUrl: o }); await chrome.storage.session.remove(['token', 'tokenAt']); $('out').textContent = '已保存：' + o + '（已清除当前会话，请重新解锁）'; }
  catch (e) { $('out').textContent = '错误：' + e.message; }
};
$('test').onclick = async () => {
  $('out').textContent = '测试中…';
  try { const o = norm(); await ensurePerm(o); const r = await new Promise(res => chrome.runtime.sendMessage({ type: 'testConnection', url: o }, res));
    if (!r || !r.ok) throw new Error(r ? r.error : '无响应');
    $('out').textContent = '连接成功 ✓ 保险箱' + (r.data.initialized ? '已初始化' : '未初始化（请先在服务器上 password-safe init）') + '，格式 v' + r.data.format_version;
  } catch (e) { $('out').textContent = '连接失败：' + e.message + '。请检查 SSH 隧道与服务端。'; }
};
