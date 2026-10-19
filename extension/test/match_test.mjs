// 纯 Node 单测：与 background.js 中 host 匹配逻辑保持同步
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

let fail = 0;
function assert(cond, msg) {
  if (!cond) { fail++; console.error('FAIL:', msg); }
  else console.log('ok:', msg);
}

const tabHost = hostOf('https://www.tjut-lfl.work/');
assert(tabHost === 'tjut-lfl.work', 'hostOf strips www');
assert(matchesSite({ name: 'tjut-lfl.work', url: 'https://www.tjut-lfl.work' }, tabHost), 'www url + apex name matches www tab');
assert(matchesSite({ name: 'tjut-lfl.work', url: 'https://tjut-lfl.work' }, tabHost), 'apex url matches www tab');
assert(matchesSite({ name: 'portal', url: 'https://www.tjut-lfl.work' }, tabHost), 'custom name + url matches');
assert(!matchesSite({ name: 'other', url: 'https://example.com' }, tabHost), 'other site no match');
assert(matchesSite({ name: 'login.live.com', url: 'https://login.live.com' }, hostOf('https://www.office.com/')), 'microsoft alias');

if (fail) { console.error(fail + ' failed'); process.exit(1); }
console.log('all match tests passed');
