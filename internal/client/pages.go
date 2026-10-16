package client

import "html/template"

const css = `<style>body{font:15px/1.6 system-ui,"Microsoft YaHei",sans-serif;max-width:760px;margin:32px auto;padding:0 16px;color:#222}
h1{font-size:22px}label{display:block;margin:10px 0 2px;font-weight:600}input{width:100%;padding:7px;box-sizing:border-box;font:inherit}
button{padding:7px 14px;margin:4px 6px 4px 0;border:0;border-radius:6px;background:#2563eb;color:#fff;cursor:pointer;font:inherit}
button.sec{background:#e5e7eb;color:#111}.ok{color:#15803d}.err{color:#b91c1c}.muted{color:#666;font-size:13px}
code{background:#f3f4f6;padding:2px 5px;border-radius:4px;word-break:break-all}li{margin:8px 0}.card{border:1px solid #e5e7eb;border-radius:8px;padding:12px 16px;margin:12px 0}</style>`

var setupTpl = template.Must(template.New("s").Parse(`<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>Password Safe 客户端设置</title>` + css + `</head><body>
<h1>Password Safe 客户端 · 连接设置 <span class="muted">Connection settings</span></h1>
<div class="card">状态 / Status: <b id="st">{{.S.State}}</b> <span class="err" id="se">{{.S.Error}}</span></div>
{{if .Msg}}<p class="ok">{{.Msg}}</p>{{end}}{{if .Err}}<p class="err">{{.Err}}</p>{{end}}
<form method="post" action="{{.Pre}}setup">
<label>服务器地址 / Server host</label><input name="host" value="{{.C.Host}}" placeholder="例如 203.0.113.10 或 vault.example.com" required>
<label>SSH 端口 / SSH port</label><input name="port" value="{{.C.Port}}" inputmode="numeric">
<label>SSH 用户名 / SSH user</label><input name="user" value="{{.C.User}}" required>
<label>私钥文件路径 / Private key path</label><input name="key" value="{{.C.KeyPath}}" placeholder="C:\Users\你的用户名\.ssh\your-key.pem" required>
<p class="muted">私钥只在本机读取，不会上传；客户端不保存私钥口令。/ The key never leaves this PC.</p>
<label>本地端口 / Local port</label><input name="lport" value="{{.C.LocalPort}}">
<label>服务器端口 / Remote port</label><input name="rport" value="{{.C.RemotePort}}">
<p><button>保存并连接 / Save &amp; connect</button></p></form>
{{if .S.NeedsPass}}<div class="card"><form method="post" action="{{.Pre}}passphrase"><label>私钥口令（仅保存在内存，退出后需重新输入）/ Key passphrase (memory only)</label>
<input type="password" name="passphrase" autocomplete="off"><p><button>解锁私钥 / Unlock key</button></p></form></div>{{end}}
<p class="muted">配置文件 / Config: <code>{{.Dir}}\config.json</code>。首次连接会记录服务器指纹（known_hosts），之后指纹变化会拒绝连接。</p>
<script>setInterval(async()=>{try{const s=await (await fetch('{{.Pre}}status')).json();document.getElementById('st').textContent=s.state+(s.api_ok?' · API OK':'');document.getElementById('se').textContent=s.error||''}catch(e){}},2000)</script>
</body></html>`))

var guideTpl = template.Must(template.New("g").Parse(`<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>安装浏览器插件</title>` + css + `</head><body>
<h1>安装浏览器插件 <span class="muted">Install the browser extension</span></h1>
<p>插件已解压到 / Extension extracted to:</p>
<p><code id="p">{{.Path}}</code> <button class="sec" id="copy">复制路径 / Copy</button> <button class="sec" data-b="folder">打开文件夹 / Open folder</button></p>
<ol>
<li>打开扩展页面 / Open the extensions page:
 <button data-b="chrome">打开 Chrome 扩展页</button> <button data-b="edge">打开 Edge 扩展页</button>
 <div class="muted">打不开时手动在地址栏输入 <code>chrome://extensions</code> 或 <code>edge://extensions</code>。</div></li>
<li>打开「开发者模式」（Chrome 在右上角，Edge 在左侧栏）。/ Turn on <b>Developer mode</b>.</li>
<li>点「加载已解压的扩展程序」，选择上面的文件夹（里面有 manifest.json）。/ Click <b>Load unpacked</b> and choose the folder above.</li>
<li>确认扩展 ID 为 <code>{{.ID}}</code>，客户端才能和插件通信。/ The extension ID should be <code>{{.ID}}</code>.</li>
<li>把插件固定到工具栏，点开后输入主密码解锁。/ Pin it and unlock with your master password.</li>
</ol>
<div class="card"><b>以后升级 / Upgrading:</b> 新版客户端会原地更新这个文件夹，只要在扩展页点插件卡片上的刷新按钮即可。<br>
If you previously loaded the extension from another folder, remove that copy first so there is only one.</div>
<p class="muted">Password Safe client {{.Ver}}</p><p id="m" class="err"></p>
<script>
document.getElementById('copy').onclick=()=>navigator.clipboard.writeText(document.getElementById('p').textContent).then(()=>{document.getElementById('m').className='ok';document.getElementById('m').textContent='已复制 / Copied'});
document.querySelectorAll('button[data-b]').forEach(b=>b.onclick=async()=>{const r=await (await fetch('{{.Pre}}open?b='+b.dataset.b,{method:'POST'})).json();document.getElementById('m').className=r.ok?'ok':'err';document.getElementById('m').textContent=r.ok?'':r.error});
</script></body></html>`))
