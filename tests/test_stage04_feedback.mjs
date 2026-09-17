/** Real source functions in a VM; fake DOM/clock. No router or network. */
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import {fileURLToPath} from 'node:url';
const root=process.env.BRORAY_TEST_ROOT || path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const js=fs.readFileSync(path.join(root,'runtime/app/web-new/assets/js/dns.js'),'utf8');
const now=1800000000000;
function fixture(){
 const servers=[['a','8.8.8.8','dns.google'],['b','1.1.1.1','cloudflare-dns.com'],['c','9.9.9.9','dns.quad9.net']].map(([id,address,sni])=>({id,name:id,address,sni,port:853,effectivePort:853,spki:'',interface:'',domain:'',present:true,test:{ok:true,testedEpoch:now/1000,latencyMs:0}}));
 const dot=servers.map(s=>({...s,valid:true,unknownTokenCount:0,deleteEligible:true,ownership:s.id==='a'?'external':'broray'}));
 return {servers,actual:{dot,determinate:true,dohCount:0,totalSecure:3},selectedIds:['a','b'],requestedIds:['a','b'],deleteEligible:true,writeProtocolEnabled:true,mutationAvailable:true,runningConfigAvailable:true,installationState:'installed',maxServers:8};
}
function dns(){
 const cut=js.indexOf('    if (document.readyState');assert.ok(cut>0);
 const context={window:{},document:{},Date:{now:()=>now},isFinite,JSON,Number,Object,Array,Boolean,String,Error,Promise};
 vm.createContext(context);
 const exportCode=`window.qa={testPresentation,testFresh,deleteAvailable,${js.includes('function deletionPreview')?'deletionPreview,':''}set:(data,ids)=>{status=data;selected=Object.create(null);ids.forEach(id=>selected[id]=true);}};})();`;
 vm.runInContext(js.slice(0,cut)+exportCode,context);
 return context.window.qa;
}
function presentation(value,props={}){return dns().testPresentation({test:{ok:true,testedEpoch:now/1000,latencyMs:value,...props}});}
function stateCheck(change,ids=['a','b']){const q=dns(),s=fixture();if(change)change(s);q.set(s,ids);return q;}
const repro=process.env.STAGE04_REPRO==='1';
test('regression: whole-second zero is not millisecond ping',()=>{assert.doesNotMatch(presentation(0).text,/0 мс/);assert.match(presentation(0).text,/< 1 с/);});
test('regression: recent TLS failure is not labelled stale',()=>{assert.match(presentation(0,{ok:false,status:'failed'}).text,/ошибка/);});
test('regression: unsaved checkbox choice cannot delete saved entries',()=>{assert.equal(stateCheck(null,['a']).deleteAvailable(),false);});
if(!repro){
 for(const value of [1000,2000,10000])test(`coarse duration ${value}`,()=>{assert.match(presentation(value).text,new RegExp('≈ '+value/1000+' с'));assert.doesNotMatch(presentation(value).text,/мс/);});
 for(const [name,value] of [['missing',undefined],['null',null],['negative',-1000],['NaN',NaN],['infinite',Infinity],['string','0'],['fractional',250]])test('invalid duration '+name,()=>{assert.equal(presentation(value).text,'TLS/SNI: OK');});
 test('missing TLS test',()=>assert.equal(dns().testPresentation({}).text,'Не проверен'));
 test('unavailable TLS tool',()=>assert.match(presentation(null,{ok:false,status:'unavailable'}).text,/недоступна/));
 test('old success stale',()=>assert.match(presentation(0,{testedEpoch:now/1000-601}).text,/устарела/));
 test('future success does not enable apply',()=>assert.equal(dns().testFresh({ok:true,testedEpoch:now/1000+1}),false));
 test('TTL boundary accepted',()=>assert.equal(dns().testFresh({ok:true,testedEpoch:now/1000-600}),true));
 test('invalid epoch not trusted',()=>assert.equal(dns().testFresh({ok:true,testedEpoch:String(now/1000)}),false));
 test('eligible exact selected external record remains visible',()=>{const p=stateCheck().deletionPreview();assert.equal(p.entries.length,2);assert.equal(p.entries[0].address,'8.8.8.8');});
 test('unselected record excluded',()=>assert.equal(stateCheck().deletionPreview().entries.some(e=>e.address==='9.9.9.9'),false));
 test('empty selection disabled',()=>assert.equal(stateCheck(s=>s.selectedIds=[],[]).deleteAvailable(),false));
 test('choice order irrelevant',()=>assert.equal(stateCheck(null,['b','a']).deleteAvailable(),true));
 for(const [name,change] of [
  ['global denied',s=>s.deleteEligible=false],['mutation denied',s=>s.mutationAvailable=false],['read unavailable',s=>s.runningConfigAvailable=false],['ambiguous observation',s=>s.actual.determinate=false],
  ['duplicate endpoint',s=>s.actual.dot.push({...s.actual.dot[0]})],['SNI mismatch',s=>s.actual.dot[0].sni='wrong.test'],['extra SPKI',s=>s.actual.dot[0].spki='pin'],['unknown token',s=>s.actual.dot[0].unknownTokenCount=1],['invalid record',s=>s.actual.dot[0].valid=false],['entry denied',s=>s.actual.dot[0].deleteEligible=false],['catalog id absent',s=>s.servers.shift()],['missing saved IDs',s=>delete s.selectedIds],['all selected missing',s=>s.actual.dot=s.actual.dot.slice(2)]
 ])test('deletion fail-closed: '+name,()=>assert.equal(stateCheck(change).deleteAvailable(),false));
 test('one missing selected record is skipped as backend does',()=>{const p=stateCheck(s=>s.actual.dot.shift()).deletionPreview();assert.equal(p.entries.length,1);});
 test('implicit effective port uses 853',()=>assert.equal(stateCheck(s=>{delete s.actual.dot[0].port;s.actual.dot[0].effectivePort=853;}).deleteAvailable(),true));
 const feedback=fs.readFileSync(path.join(root,'runtime/app/web-new/assets/js/action-feedback.js'),'utf8');const ctx={window:{},document:{},JSON};vm.createContext(ctx);vm.runInContext(feedback,ctx);const describe=ctx.window.BROrayActionFeedback.describe;
 test('no DOM access or requests just loading feedback helper',()=>assert.ok(ctx.window.BROrayActionFeedback));
 test('unknown cause does not invent another running job',()=>assert.doesNotMatch(describe({message:'lock unavailable'},'preflight').message,/уже выполняется/));
 test('only explicit busy code says busy',()=>assert.match(describe({code:'ROUTES_OPERATION_BUSY'},'preflight').message,/уже выполняется/));
 test('preflight scope is this window, not global router unchanged',()=>assert.match(describe({},'preflight').consequence,/из этого окна не отправлен/));
 test('mutation does not claim unchanged settings',()=>assert.doesNotMatch(describe({},'mutation').consequence,/не отправлен|не изменен|не изменён/));
 test('error code and raw details retained',()=>{const p=describe({code:'ROUTES_PREFLIGHT_PLAN_FAILED',message:'source',details:{reason:'fixture'}},'preflight');assert.match(p.details,/ROUTES_PREFLIGHT_PLAN_FAILED/);assert.match(p.details,/fixture/);});
 test('DNS HTML unifies deletion wording and collapses technical sections',()=>{const h=fs.readFileSync(path.join(root,'runtime/app/web-new/dns.html'),'utf8');assert.match(h,/Удалить выбранные DoT-записи/);assert.doesNotMatch(h,/R14C01 receipt|legacy receipts|Внешние и legacy-записи сохраняются/);assert.match(h,/<details[^>]+id="dns-actual-details"/);assert.match(h,/<details[^>]+id="dns-technical-details"/);});
 for(const name of ['routes-catalog.js','routes-custom-page.js'])test('route catch integration '+name,()=>{const s=fs.readFileSync(path.join(root,'runtime/app/web-new/assets/js',name),'utf8');assert.match(s,/showPreparationFeedback\(error, mutationStarted \? "mutation" : "preflight"\)/);assert.match(s,/mutationStarted = true/);});
}
