// Execute the actual pure rendering functions; no DOM/browser/network claim.
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import test from 'node:test';
import assert from 'node:assert/strict';
import {fileURLToPath} from 'node:url';
const root=process.env.BRORAY_TEST_ROOT||path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const source=fs.readFileSync(path.join(root,'runtime/app/web-new/assets/js/subscriptions.js'),'utf8');
function section(begin,end){const a=source.indexOf(begin);assert(a>=0);const b=source.indexOf(end,a+begin.length);assert(b>a);return source.slice(a,b);}
const ctx=vm.createContext({});
vm.runInContext(section('    function escapeHtml(', '    function toast(')+section('    function renderResult(', '    function renderNodes('),ctx);
const show=(result)=>ctx.renderResult({lastUpdateResult:result});
const base={received:3,accepted:2,added:1,updated:1,removed:0,rejected:1};
test('partial result distinguishes accepted from retained',()=>{const s=show({...base,retained:4,catalogTotal:6});assert(s.includes('принято 2'));assert(s.includes('сохранено прежних 4'));assert(s.includes('удалено 0'));});
test('legacy result unchanged',()=>assert.equal(show(base),'получено 3 · принято 2 · добавлено 1 · обновлено 1 · удалено 0 · отклонено 1'));
for(const [name,value] of [['zero',0],['negative',-1],['fraction',1.5],['string','2'],['html','<img src=x onerror=alert(1)>'],['null',null],['undefined',undefined],['NaN',NaN],['Infinity',Infinity],['unsafe',9007199254740992]]){
 test('invalid retained count is not rendered: '+name,()=>assert.equal(show({...base,retained:value}),show(base)));
}
test('error has no success/retention summary',()=>assert.equal(show({...base,retained:5,errorCode:'PARTIAL_UPDATE_LIMIT'}),'Код: PARTIAL_UPDATE_LIMIT'));
test('error code remains escaped',()=>assert.equal(show({errorCode:'<blocked>'}),'Код: &lt;blocked&gt;'));
test('never updated keeps existing message',()=>assert.equal(show(null),'Обновление ещё не выполнялось.'));
