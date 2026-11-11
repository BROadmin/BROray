const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
function render(result){const window={},document={readyState:'loading',addEventListener(){},getElementById(){return null;}};let code=fs.readFileSync(process.env.SUBSCRIPTION_JS_FIXTURE || path.join(__dirname,'../../webui/static/assets/js/subscriptions.js'),'utf8');code=code.replace(/\}\)\(\);\s*$/,'window.fixture={renderResult};})();');vm.runInNewContext(code,{window,document,Map});return window.fixture.renderResult({lastUpdateResult:result});}
test('failed subscription update retains counts and escapes error code',()=>{const text=render({received:4,accepted:0,rejected:4,errorCode:'<NO_VALID_NODES>',durationMs:125});for(const expected of ['получено 4','принято 0','отклонено 4','&lt;NO_VALID_NODES&gt;','125 мс'])assert.ok(text.includes(expected),text);});
test('fetch failure shows zero counts without injecting HTML',()=>{const text=render({errorCode:'FETCH_ERROR'});assert.ok(text.includes('получено 0'));assert.ok(text.includes('отклонено 0'));});
