// Automated smoke test with a mock vault API on 127.0.0.1:8787 (stop the real tunnel first).
// 自动冒烟测试：npm i playwright-core && npx playwright-core install chromium && node extension/test/e2e.js
const http=require('http');const {chromium}=require('playwright-core');
const E=[{id:'1',name:'localhost',username:'user@example.com',url:'http://localhost:8000'}];
let saved=null;
http.createServer((q,r)=>{let b='';q.on('data',d=>b+=d);q.on('end',()=>{r.setHeader('content-type','application/json');
 if(q.url=='/health')return r.end('{"status":"ok"}');
 if(q.url=='/api/v1/unlock')return r.end('{"token":"t"}');
 if(q.url.startsWith('/api/v1/entries/1'))return r.end(JSON.stringify({...E[0],password:'S3cret!'}));
 if(q.url.startsWith('/api/v1/entries')&&q.method=='POST'){saved=JSON.parse(b);return r.end('{}');}
 if(q.url.startsWith('/api/v1/entries'))return r.end(JSON.stringify({entries:E}));
 r.statusCode=404;r.end('{}');});}).listen(8787);
const st=http.createServer((q,r)=>{r.setHeader('content-type','text/html');r.end(require('fs').readFileSync(__dirname+'/multi-step-login.html'));}).listen(8000);
(async()=>{const ext=require('path').resolve(__dirname,'..');
const ctx=await chromium.launchPersistentContext(require('os').tmpdir()+'/pv-e2e-'+Date.now(),{channel:'chromium',headless:false,ignoreDefaultArgs:['--disable-extensions','--disable-component-extensions-with-background-pages'],args:['--headless=new','--disable-features=DisableLoadExtensionCommandLineSwitch',`--disable-extensions-except=${ext}`,`--load-extension=${ext}`]});
let sw;for(let i=0;i<50&&!sw;i++){sw=ctx.serviceWorkers().find(w=>w.url().endsWith('/background.js'));if(!sw)await new Promise(r=>setTimeout(r,200));}console.log(sw&&sw.url());
await sw.evaluate(()=>chrome.storage.session.set({token:'t',tokenAt:Date.now()}));
for(const mode of ['spa','nav']){
 const p=await ctx.newPage();await p.goto('http://localhost:8000/?mode='+mode);
 await p.waitForSelector('.pv-icon',{timeout:5000});
 await p.dispatchEvent('.pv-icon','mousedown');await p.waitForSelector('.pv-menu div:has-text("user@example.com")');
 await p.click('.pv-menu div:has-text("user@example.com")');await p.waitForTimeout(500);
 console.log(mode,'step1 user=',await p.inputValue('#identifierId'));
 await p.click('button');await p.waitForSelector('input[type=password]');await p.waitForTimeout(1500);
 console.log(mode,'step2 pw=',await p.inputValue('input[type=password]'),'hidden=',await p.inputValue('input[name=username]'));
 await p.close();}
// save flow: new user
await sw.evaluate(()=>chrome.storage.session.clear().then(()=>chrome.storage.session.set({token:'t',tokenAt:Date.now()})));
E[0].url='http://other.example';E[0].name='other';
const p=await ctx.newPage();await p.goto('http://localhost:8000/?mode=spa');
await p.fill('#identifierId','new@example.com');await p.click('button');await p.waitForSelector('input[type=password]');
await p.fill('input[type=password]','N3wPass!');await p.click('button');await p.waitForTimeout(2500);
console.log('bar:',await p.locator('.pv-bar').allInnerTexts());
await ctx.close();process.exit(0);})().catch(e=>{console.error(e);process.exit(1)});
