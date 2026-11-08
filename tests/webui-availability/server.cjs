// Loopback-only browser fixture: real HTML/scripts, synthetic API, no router.
const http=require('node:http'),fs=require('node:fs'),path=require('node:path');
const root=path.resolve(__dirname,'../../webui/static');
let mode='session-error',calls=[],held=new Set();
const types={'.html':'text/html; charset=utf-8','.js':'application/javascript; charset=utf-8','.css':'text/css; charset=utf-8','.svg':'image/svg+xml','.png':'image/png','.ico':'image/x-icon','.json':'application/json'};
const server=http.createServer((req,res)=>{
  const url=new URL(req.url,'http://localhost');
  res.setHeader('Cache-Control','no-store');
  function json(value,status=200){res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(value));}
  if(url.pathname==='/__fixture__/state'){return json({fixture:'broray-webui-availability-351',mode,calls});}
  if(url.pathname==='/__fixture__/mode'&&req.method==='POST'){
    const next=url.searchParams.get('value');
    if(!['session-error','ready','login-headers','login-body','expired'].includes(next))return json({},400);
    for(const r of held)r.destroy();held.clear();mode=next;calls=[];return json({mode});
  }
  if(url.pathname==='/__fixture__/shutdown'&&req.method==='POST'){
    json({closed:true});for(const r of held)r.destroy();server.close();server.closeAllConnections();return;
  }
  if(url.pathname.startsWith('/api/')){
    calls.push({path:url.pathname,method:req.method,at:Date.now()});
    if(url.pathname==='/api/session.cgi'){
      if(mode==='session-error')return json({error:'TEMPORARY_UNAVAILABLE',message:'Временная ошибка связи (локальный тест)'},503);
      if(mode.startsWith('login-')||mode==='expired')return json({error:'SESSION_REQUIRED'},401);
      return json({ok:true,user:'synthetic'});
    }
    if(url.pathname==='/api/login.cgi'){
      req.resume(); // deliberately do not retain submitted synthetic credentials
      if(mode.startsWith('login-')){
        held.add(res);res.on('close',()=>held.delete(res));
        if(mode==='login-body'){res.writeHead(200,{'Content-Type':'application/json'});res.write('{"ok":');}
        return;
      }
      return json({ok:true,user:'synthetic',redirect:'/home.html'});
    }
    return json({success:true,data:{installed:false,running:false,health:{availability:'available',severity:'warning',reasons:[],freshness:{state:'fresh'}},operationRunning:false}});
  }
  const pathname=url.pathname==='/'?'/index.html':url.pathname;
  const file=path.resolve(root,'.'+decodeURIComponent(pathname));
  if(!file.startsWith(root+path.sep)){res.writeHead(404);res.end();return;}
  fs.readFile(file,(err,b)=>{if(err){res.writeHead(404);res.end();return;}res.writeHead(200,{'Content-Type':types[path.extname(file)]||'application/octet-stream'});res.end(b);});
});
server.listen(28451,'127.0.0.1',()=>console.log('broray-webui-availability-351 http://127.0.0.1:28451'));
