/* Run the actual request wrapper in Node; synthetic responses, no router. */
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../runtime/app/web-new/assets/js/common.js'), 'utf8');
const begin = source.indexOf('    async function apiRequest(');
const end = source.indexOf('\n    // One request nonce', begin);
assert(begin >= 0 && end > begin);
let count = 0;
async function check(status, payload, expectedCode, expectedMessage) {
    const context = vm.createContext({fetch: async () => ({status, ok: status < 400,
        json: async () => { if (payload === null) throw new Error('private HTML'); return payload; }})});
    vm.runInContext(source.slice(begin, end) + '\nthis.request = apiRequest;', context);
    try { await context.request('/api/login.cgi', {method: 'POST'}); assert.fail('error expected'); }
    catch (error) {
        assert.equal(error.status, status);
        assert.equal(error.code, expectedCode);
        assert.equal(error.message, expectedMessage);
        assert(!error.message.includes('private HTML'));
    }
    count++;
}
(async () => {
    await check(401, {ok:false,error:'INVALID_CREDENTIALS',message:'Неверный логин или пароль.'},
        'INVALID_CREDENTIALS','Неверный логин или пароль.');
    await check(503, {ok:false,error:'KEENETIC_UNAVAILABLE',message:'Keenetic недоступен.'},
        'KEENETIC_UNAVAILABLE','Keenetic недоступен.');
    await check(409, {ok:false,error:{code:'OPERATION_CONFLICT',message:'Занято.',details:{busy:true}}},
        'OPERATION_CONFLICT','Занято.');
    await check(502, null, 'HTTP_RESPONSE_INVALID','Ошибка запроса (HTTP 502).');
    await check(500, {error:'BAD_REQUEST'}, 'BAD_REQUEST','Ошибка запроса (HTTP 500).');
    await check(200, null, 'HTTP_RESPONSE_INVALID','Некорректный ответ сервера (HTTP 200).');
    console.log(JSON.stringify({tests:count,status:'PASS',routerAccessed:false}));
})().catch(error => { console.error(error); process.exitCode=1; });
