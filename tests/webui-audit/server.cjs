// Read-only production snapshots are sanitized before loading. Every mutation
// terminates here; no upstream requests or router access are possible.
const http=require('node:http'),fs=require('node:fs'),path=require('node:path');
const root=path.resolve(__dirname,'../../webui/static');
const fixture=JSON.parse(fs.readFileSync(process.argv[2],'utf8'));
const calls=[];let closed=false,authenticated=true,automationPaused=false;
const types={'.html':'text/html; charset=utf-8','.js':'text/javascript; charset=utf-8','.css':'text/css; charset=utf-8','.svg':'image/svg+xml','.png':'image/png','.ico':'image/x-icon'};
const server=http.createServer(async(req,res)=>{
 const u=new URL(req.url,'http://127.0.0.1');
 res.setHeader('Cache-Control','no-store');
 res.setHeader('Content-Security-Policy',"connect-src 'self'; form-action 'self'; frame-src 'self'");
 const json=(value,status=200)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(value));};
 if(u.pathname==='/__audit/state')return json({id:'broray-dns-webui-audit-20261113',calls});
 if(u.pathname==='/__audit/response'&&req.method==='POST'){
  let raw='';for await(const chunk of req)raw+=chunk;const x=JSON.parse(raw);
  if(!x.path.startsWith('/api/'))return json({},400);
  fixture[x.path]={status:x.status||200,body:x.body,allowPost:true};return json({ok:true});
 }
 if(u.pathname==='/__audit/shutdown'&&req.method==='POST'){json({closed:true});closed=true;server.close();return;}
 if(u.pathname==='/__audit/host'){res.writeHead(200,{'Content-Type':'text/html'});return res.end('<!doctype html><title>BROray isolated audit</title><body style="margin:0"><iframe id="scene" style="border:0;width:390px;height:844px" src="/servers.html"></iframe></body>');}
 if(u.pathname.startsWith('/api/')){
  let raw='';for await(const chunk of req)raw+=chunk;
  calls.push({path:u.pathname,method:req.method,body:u.pathname==='/api/login.cgi'?'[synthetic credentials omitted]':raw,at:Date.now()});
  if(u.pathname==='/api/session.cgi')return json({ok:authenticated,authenticated,user:'QA'},authenticated?200:401);
  if(u.pathname==='/api/logout.cgi'){authenticated=false;return json({ok:true});}
  if(u.pathname==='/api/login.cgi'){authenticated=true;return json({ok:true,authenticated:true,user:'QA'});}
  if(u.pathname==='/api/broray/backup-export.cgi'){res.setHeader('Content-Disposition','attachment; filename="synthetic-backup.json"');return json({fixture:true});}
  if(fixture[u.pathname] && (req.method==='GET'||fixture[u.pathname].allowPost))return json(fixture[u.pathname].body,fixture[u.pathname].status);
  const data=v=>json({success:true,data:v});
  const body=()=>raw?JSON.parse(raw):{};
  if(/^\/api\/routes\/dot-(test|apply|delete|delete-preview|auto-settings)\.cgi$/.test(u.pathname)){
   const d=fixture['/api/routes/dot-status.cgi'].body.data,b=body();
   if(u.pathname.endsWith('auto-settings.cgi')){d.autoCheck.enabled=b.enabled;return data(d.autoCheck);}
   if(u.pathname.endsWith('delete-preview.cgi'))return data({schemaVersion:1,serverIds:d.selectedIds,entries:d.actual.dot,expectedFingerprint:'a'.repeat(64)});
   if(b.serverIds)d.selectedIds=b.serverIds;
   d.servers.forEach(s=>{s.selected=d.selectedIds.includes(s.id);s.test={ok:true,status:'ok',testedEpoch:Math.floor(Date.now()/1000),latencyMs:0};});
   if(u.pathname.endsWith('apply.cgi')){
    d.actual.dot=d.servers.filter(s=>s.selected).map(s=>({...s,valid:true,deleteEligible:true,unknownTokenCount:0}));
    d.managed=d.actual.dot;d.installed=true;d.matchesSelection=true;d.deleteEligible=true;d.installationState='installed';
    d.servers.forEach(s=>s.present=s.selected);
   }
   if(u.pathname.endsWith('/dot-delete.cgi')){d.actual.dot=[];d.managed=[];d.installed=false;d.matchesSelection=false;d.deleteEligible=false;d.installationState='not-installed';d.servers.forEach(s=>s.present=false);}
   d.selectedTestedCount=d.selectedIds.length;d.selectedPresentCount=d.servers.filter(s=>s.selected&&s.present).length;d.actual.totalSecure=d.actual.dot.length;
   return data(d);
  }
  if(req.method==='POST'&&/^\/api\/subscriptions\/(create|update|delete)\.cgi$/.test(u.pathname)){
   const b=body(), list=fixture['/api/subscriptions/list.cgi'].body.data;
   b.id=b.id||u.searchParams.get('id')||'qa-created';
   if(u.pathname.endsWith('/delete.cgi')){const i=list.findIndex(x=>x.id===b.id);if(i>=0)list.splice(i,1);return data({deleted:true});}
   let s=list.find(x=>x.id===b.id);if(!s){s={id:'qa-created',serversCount:0};list.push(s)}Object.assign(s,b);return data(s);
  }
  if(u.pathname==='/api/servers/deactivate.cgi'){const s=fixture['/api/servers/summary.cgi'].body.data;s.activeServer=null;s.connectionState='disconnected';s.servers.forEach(x=>x.active=false);return data(s);}
  if(u.pathname==='/api/servers/delete.cgi'){const s=fixture['/api/servers/summary.cgi'].body.data;s.servers=s.servers.filter(x=>x.id!==body().id);return data(s);}
  if(u.pathname==='/api/servers/details.cgi')return data({id:u.searchParams.get('id'),name:'QA',address:'fixture.example',protocol:'vless',network:'xhttp',importWarnings:['Тестовое предупреждение конфигурации: '+ 'длинный текст '.repeat(12)]});
  if(u.pathname==='/api/subscriptions/details.cgi')return data({...fixture['/api/subscriptions/list.cgi'].body.data.find(x=>x.id===u.searchParams.get('id')),url:'https://example.invalid/fixture'});
  if(u.pathname==='/api/subscriptions/servers.cgi')return data({items:[{id:'qa-node',name:'Тестовый сервер',enabled:true,protocol:'vless',network:'xhttp'}],total:1});
  if(u.pathname==='/api/operations/list.cgi')return data({operations:[],items:[],running:0});
  if(u.pathname==='/api/operations/status.cgi')return json({ok:true,complete:true,queue:[],operations:[],automationPaused});
  if(u.pathname==='/api/operations/stop-background.cgi'){automationPaused=true;return json({ok:true,automationPaused});}
  if(u.pathname==='/api/operations/automation.cgi'){automationPaused=body().paused;return json({ok:true,automationPaused});}
  if(['/api/servers/check.cgi','/api/servers/activate.cgi','/api/subscriptions/refresh.cgi'].includes(u.pathname))return json({ok:true,requestId:'q-'+'1'.repeat(32),state:'completed'});
  if(u.pathname==='/api/xray/check-config.cgi')return data({valid:true,output:'Isolated fixture: configuration valid'});
  if(u.pathname==='/api/xray/diagnostics.cgi')return data({checks:[{id:'fixture',status:'ok',message:'Изолированная проверка'}],summary:{ok:1,warning:0,error:0}});
  if(/\/api\/xray\/(start|stop|restart)\.cgi$/.test(u.pathname)){
   const x=fixture['/api/xray/status.cgi'].body.data;x.running=!u.pathname.endsWith('/stop.cgi');x.socksActive=x.running;return data(x);
  }
  if(u.pathname==='/api/operations/events.cgi')return json({ok:true,complete:true,events:[]});
  if(u.pathname==='/api/operations/automation-status.cgi')return data({paused:false});
  if(/\/api\/servers\/(auto-switch|quality-refresh)-(status|save)\.cgi$/.test(u.pathname))return data({config:{enabled:false,intervalMinutes:60},state:{},service:{running:true},automation:{paused:false}});
  if(u.pathname==='/api/routes/catalog-schedule.cgi')return data({intervalHours:0,maxRemovalPercent:50,autoApply:false,running:false});
  if(/(operation-status|update-status|check-udp-status)\.cgi$/.test(u.pathname))return data({running:false,active:false,operationRunning:false,state:'idle'});
  if(u.pathname==='/api/routes/schedule-status.cgi')return data({enabled:false,intervalHours:6});
  if(u.pathname==='/api/broray/backup-status.cgi')return data({preImportCopies:[]});
  return json({success:false,error:{code:'AUDIT_UNIMPLEMENTED',message:'Изолированная проверка: сценарий API ещё не задан.'}},501);
 }
 const p=path.resolve(root,'.'+decodeURIComponent(u.pathname==='/'?'/index.html':u.pathname));
 if(!p.startsWith(root+path.sep)){res.writeHead(404);return res.end();}
 fs.readFile(p,(e,b)=>{if(e){res.writeHead(404);return res.end();}res.writeHead(200,{'Content-Type':types[path.extname(p)]||'application/octet-stream'});res.end(b)});
});
server.listen(28458,'127.0.0.1',()=>console.log('broray-dns-webui-audit-20261113 ready 28458'));
