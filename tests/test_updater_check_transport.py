"""Read-only release checks must work without a usable active VPN."""
import json,os,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]

class CheckTransport(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name);(self.root/'lib').mkdir();(self.root/'bin').mkdir()
        (self.root/'lib/xray.sh').write_text('broray_xray_pid(){ echo 123; }\nbroray_xray_config_path(){ echo "$BRORAY_ROOT/config.json"; }\n')
        (self.root/'config.json').write_text(json.dumps({'inbounds':[{'protocol':'socks','listen':'127.0.0.1','port':2080,'settings':{'auth':'noauth'}}],'outbounds':[{'protocol':'blackhole'}]}))
        self.executable('bin/ip','#!/bin/ash\nprintf "    inet 127.0.0.1/8 scope host lo\\n"\n')
        self.executable('ctl','''#!/bin/ash
printf '%s|%s|%s|%s\n' "$*" "${HTTPS_PROXY:-}" "${https_proxy:-}" "${ALL_PROXY:-}" >>"$BRORAY_ROOT/calls"
if [ -n "${HTTPS_PROXY:-}${https_proxy:-}${ALL_PROXY:-}" ]; then result="$PROXY_RESULT"; else result="$DIRECT_RESULT"; fi
case "$result" in
 success) printf '{"ok":true,"updateAvailable":false}\n';exit 0 ;;
 malformed) printf 'broken';exit 1 ;;
 *) printf '{"ok":false,"error":{"code":"%s","message":"fixture"}}\n' "$result";exit 1 ;;
esac
''')
    def executable(self,name,text):
        p=self.root/name;p.write_text(text);p.chmod(0o755)
    def call(self,direct='success',proxy='UPDATE_CHECK_FAILED',command='check',inherited=True):
        env={k:v for k,v in os.environ.items() if k.lower() not in ['http_proxy','https_proxy','all_proxy','no_proxy']}
        env.update(BRORAY_ROOT=str(self.root),BRORAY_API_TMP_ROOT=str(self.root/'tmp'),BRORAY_UPDATER_CTL=str(self.root/'ctl'),BRORAY_PLATFORM_HANDOFF=str(self.root/'absent'),DIRECT_RESULT=direct,PROXY_RESULT=proxy,PATH=str(self.root/'bin')+':'+env['PATH'])
        if inherited:env.update(HTTPS_PROXY='http://untrusted:9',https_proxy='http://untrusted:9',ALL_PROXY='http://untrusted:9',NO_PROXY='*')
        script='. "'+str(ROOT/'runtime/app/web-new/api/broray/updater-api-common.sh')+'"\nbroray_api_print_json_headers(){ printf "Content-Type: application/json\\r\\n"; }\nbroray_api_error(){ exit 99; }\nbroray_updater_api_call "200 OK" '+command+'\n'
        p=subprocess.run(['/bin/ash','-c',script],env=env,capture_output=True,timeout=15)
        self.assertEqual(p.returncode,0,p.stderr)
        self.calls=[s.split('|') for s in (self.root/'calls').read_text().splitlines()]
        return json.loads(p.stdout.split(b'\r\n\r\n',1)[1])
    def test_direct_check_with_blackhole_xray_uses_no_proxy(self):
        j=self.call();self.assertTrue(j['ok']);self.assertEqual(self.calls,[['check','','','']])
    def test_network_failure_uses_verified_local_fallback(self):
        j=self.call(direct='UPDATE_CHECK_FAILED',proxy='success');self.assertTrue(j['ok'])
        self.assertEqual(self.calls,[['check','','',''],['check','socks5h://127.0.0.1:2080','socks5h://127.0.0.1:2080','']])
    def test_signature_transport_failure_can_fallback(self):
        j=self.call(direct='UPDATE_SIGNATURE_MISSING',proxy='success');self.assertTrue(j['ok']);self.assertEqual(len(self.calls),2)
    def test_signature_failure_never_fallback(self):
        j=self.call(direct='UPDATE_SIGNATURE_INVALID',proxy='success');self.assertEqual(j['error']['code'],'UPDATE_SIGNATURE_INVALID');self.assertEqual(len(self.calls),1)
    def test_invalid_index_never_fallback(self):
        j=self.call(direct='UPDATE_INDEX_INVALID',proxy='success');self.assertEqual(j['error']['code'],'UPDATE_INDEX_INVALID');self.assertEqual(len(self.calls),1)
    def test_malformed_backend_reply_never_fallback(self):
        j=self.call(direct='malformed',proxy='success');self.assertEqual(j['error']['code'],'UPDATER_BACKEND_FAILED');self.assertEqual(len(self.calls),1)
    def test_unverified_local_endpoint_never_fallback(self):
        self.executable('bin/ip','#!/bin/ash\nexit 1\n')
        j=self.call(direct='UPDATE_CHECK_FAILED',proxy='success');self.assertEqual(j['error']['code'],'UPDATE_CHECK_FAILED');self.assertEqual(len(self.calls),1)
    def test_both_transports_fail_preserves_error(self):
        j=self.call(direct='UPDATE_CHECK_FAILED',proxy='UPDATE_CHECK_FAILED');self.assertEqual(j['error']['code'],'UPDATE_CHECK_FAILED');self.assertEqual(len(self.calls),2)
    def test_mutating_request_never_replayed_or_proxy_overridden(self):
        j=self.call(proxy='success',command='request update');self.assertTrue(j['ok']);self.assertEqual(self.calls,[['request update','http://untrusted:9','http://untrusted:9','http://untrusted:9']])

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
