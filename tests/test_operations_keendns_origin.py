"""BROray-01 regression tests. Run on a developer Linux host, NOT on a router.

Real changed shell files run under BusyBox ash (or BRORAY_TEST_SHELL).
Only /opt/broray path literals are rebased into a private temporary fixture.
Auth/body/coordinator/publication backends are explicit test doubles: these
checks prove the origin gate and its CGI wiring, NOT hardware acceptance or
the correctness of the unchanged live publication/receipt implementation.
No real sessions, network requests, router commands or production paths used.
"""
import copy
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import tempfile
import time
import unittest

SOURCE = Path(os.environ.get('BRORAY_TEST_SOURCE', Path(__file__).resolve().parents[1]))
BUSYBOX = shutil.which('busybox')
SHELL = shlex.split(os.environ.get('BRORAY_TEST_SHELL', f'{BUSYBOX} ash' if BUSYBOX else '/bin/sh'))
REAL_JQ = shutil.which('jq')
REAL_TIMEOUT = shutil.which('timeout')
ORIGIN = 'https://broray.qa-router.keenetic.link'
VALID_OPERATION = 'op-20260917000000-1234-abcdef012345'
PUBLICATION = {
    'schemaVersion': 1, 'state': 'enabled', 'enabled': True,
    'consistent': True, 'recoveryRequired': False,
    'address': ORIGIN + '/', 'localAddress': 'http://192.168.1.1:8080/',
    'keenDns': {'available': True, 'name': 'qa-router', 'domain': 'keenetic.link'},
    'ownership': {'liveBlockPresent': True, 'receiptPresent': True,
                  'receiptValid': True, 'liveBlockOwnedExact': True},
}

AUTH = r'''
broray_api_print_json_headers() { printf '%s\r\n' 'Content-Type: application/json' 'Cache-Control: no-store'; }
broray_api_error() {
 printf 'Status: %s\r\n' "$1"; broray_api_print_json_headers; printf '\r\n'
 printf '{"success":false,"error":{"code":"%s"}}\n' "$2"; exit 0
}
broray_api_require_method() {
 [ "${REQUEST_METHOD:-}" = "$1" ] || broray_api_error '405 Method Not Allowed' METHOD_NOT_ALLOWED
}
broray_api_require_session() {
 [ "${HTTP_COOKIE:-}" = 'BRORAY_SESSION=fixture-valid' ] || broray_api_error '401 Unauthorized' AUTH_REQUIRED
}
'''
CLIENT = r'''
broray_ops_call() {
 printf '%s\n' "$*" >>"$FIXTURE_ROOT/dispatch"
 printf '%s\n' "${FIXTURE_RESPONSE:-}"; return "${FIXTURE_RC:-0}"
}
'''
BODY = r'''
broray_web_request_body_to_file() {
 dd bs=1 count="$CONTENT_LENGTH" of="$1" 2>/dev/null || return 1
 [ "$(wc -c <"$1" | tr -d ' ')" = "$CONTENT_LENGTH" ]
}
'''
PUBLISH = r'''
broray_web_publish_status_json() {
 local w name domain
 w="$BRORAY_WEB_PUBLISH_ROOT/tmp/web-publish-status.$$"
 # Exercise the real helper's read allowlist, timeout and strict identity reader.
 broray_web_publish_ndmc 'show ndns' >"$w/ndns" 2>"$w/ndns.err" || return 1
 name="$(broray_web_publish_ndns_field name "$w/ndns")" || return 1
 domain="$(broray_web_publish_ndns_field domain "$w/ndns")" || return 1
 [ -n "$name" ] && [ -n "$domain" ] || return 1
 broray_web_publish_ndmc 'show running-config' >"$w/running-config" 2>"$w/running-config.err" || return 1
 [ "${FIXTURE_PUBLISH_MODE:-}" != write ] || broray_web_publish_ndmc 'system configuration save' || return 1
 cat "$FIXTURE_ROOT/publication.json"
}
'''
NDMC = r'''#!/bin/sh
printf '%s\n' "$*" >>"$FIXTURE_ROOT/ndmc-calls"
[ "$1" = '-c' ] && [ "$#" = 2 ] || exit 126
case "${FIXTURE_NDMC_MODE:-ok}" in
 fail) exit 1 ;;
 hang) exec sleep 30 ;;
esac
case "$2" in
 'show ndns') cat "$FIXTURE_ROOT/ndns.txt" ;;
 'show running-config')
  case "${FIXTURE_NDMC_MODE:-ok}" in
   slow-running) sleep 12 ;;
   hang-running) exec sleep 60 ;;
  esac
  printf '%s\n' 'ip http proxy broray' ' upstream http 192.168.1.1 8080' '!' ;;
 *) exit 126 ;;
esac
'''


class OperationsOrigin(unittest.TestCase):
    def setUp(self):
        if os.name == 'nt' or not REAL_JQ or not REAL_TIMEOUT:
            self.fail('Requires developer Linux, jq and timeout; do not run on router.')
        self.temp = tempfile.TemporaryDirectory(prefix='broray-origin-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for d in ['lib', 'web-new/api/operations', 'tmp', 'bin']:
            (self.root / d).mkdir(parents=True, exist_ok=True)
        for rel in ['lib/operations-origin.sh', 'web-new/api/operations/common.sh']:
            text = (SOURCE / 'runtime/app' / rel).read_text(encoding='utf-8')
            (self.root / rel).write_text(text.replace('/opt/broray', str(self.root)), encoding='utf-8')
        self.helper = self.root / 'lib/operations-origin.sh'
        for rel, text in [('web-new/api/auth-common.sh', AUTH), ('lib/operation-client.sh', CLIENT),
                          ('lib/web-request-body.sh', BODY),
                          ('lib/operation-public.jq', (SOURCE / 'runtime/app/lib/operation-public.jq').read_text(encoding='utf-8')),
                          ('lib/web-publish.sh', PUBLISH), ('bin/ndmc', NDMC)]:
            (self.root / rel).write_text(text, encoding='utf-8')
        (self.root / 'bin/ndmc').chmod(0o700)
        dispatcher = self.root / 'bin/broray-system-ndmc'
        dispatcher.write_text((SOURCE / 'runtime/app/bin/broray-system-ndmc').read_text().replace('/bin/ndmc', str(self.root / 'bin/ndmc')))
        dispatcher.chmod(0o700)
        if os.environ.get('BRORAY_TEST_BUSYBOX_APPLETS') == '1':
            if not BUSYBOX: self.fail('BusyBox is required for its applet matrix')
            for name in ['awk','timeout','mkdir','rm','rmdir','cat','dd','wc','tr']:
                wrapper=self.root/'bin'/name
                wrapper.write_text('#!/bin/sh\nexec '+shlex.quote(BUSYBOX)+' '+name+' "$@"\n')
                wrapper.chmod(0o700)
        self.save_publication(PUBLICATION)
        (self.root / 'ndns.txt').write_text(' name: qa-router\n domain: keenetic.link\n access: cloud\n')
        self.env = {**os.environ, 'PATH': str(self.root / 'bin') + ':' + os.environ['PATH'],
                    'FIXTURE_ROOT': str(self.root), 'HTTP_HOST': '192.168.1.1:8080',
                    'HTTP_ORIGIN': ORIGIN, 'HTTP_COOKIE': 'BRORAY_SESSION=fixture-valid',
                    'HTTP_X_BRORAY_REQUEST': 'operations', 'QUERY_STRING': '',
                    'CONTENT_TYPE': 'application/json', 'FIXTURE_RC': '0',
                    'FIXTURE_RESPONSE': '{"ok":true}', 'FIXTURE_NDMC_MODE': 'ok',
                    'FIXTURE_PUBLISH_MODE': ''}

    def save_publication(self, state):
        (self.root / 'publication.json').write_text(json.dumps(state), encoding='utf-8')

    def run_shell(self, code, env=None, stdin='', timeout=12):
        return subprocess.run(SHELL + ['-c', code], input=stdin.encode(), capture_output=True,
                              env={**self.env, **(env or {})}, timeout=timeout)

    def normalize(self, value):
        return self.run_shell('. "$HELPER"; broray_operations_normalize_origin "$VALUE"',
                              {'HELPER': str(self.helper), 'VALUE': value})

    def calls(self, name='dispatch'):
        p = self.root / name
        return p.read_text().splitlines() if p.exists() else []

    def request(self, verb='automation', body=None, method='POST', env=None, expected=200, timeout=12):
        if body is None and method == 'POST':
            body = {'paused': False}
        raw = '' if body is None else body if isinstance(body, str) else json.dumps(body)
        before = len(self.calls())
        result = self.run_shell('. "$COMMON"; broray_operations_api "$METHOD" "$VERB"', {
            'COMMON': str(self.root / 'web-new/api/operations/common.sh'),
            'METHOD': 'GET' if verb in ['status', 'report', 'journal'] else 'POST',
            'VERB': verb, 'REQUEST_METHOD': method,
            'CONTENT_LENGTH': str(len(raw.encode())), **(env or {})}, raw, timeout=timeout)
        self.assertEqual(result.returncode, 0, result.stderr.decode(errors='replace'))
        self.assertEqual(result.stderr, b'', result.stderr.decode(errors='replace'))
        headers, payload = result.stdout.split(b'\r\n\r\n', 1)
        self.assertIn(f'Status: {expected} '.encode(), headers, result.stdout.decode())
        if expected in [400, 401, 403, 405, 413, 415]:
            self.assertEqual(len(self.calls()), before, 'Rejected request reached coordinator')
        self.assertEqual(list((self.root / 'tmp').iterdir()), [], 'Read workspace leaked')
        return headers, json.loads(payload)

    def test_proxy_post_automation_requires_exact_live_origin(self):
        self.request()
        self.assertEqual(self.calls(), ['resume'])
        self.assertEqual(self.calls('ndmc-calls'), ['-c show ndns', '-c show running-config'])

    def test_large_running_config_completes_before_origin_decision(self):
        self.request(env={'FIXTURE_NDMC_MODE':'slow-running'}, timeout=25)
        self.assertEqual(self.calls(), ['resume'])
        self.assertEqual(self.calls('ndmc-calls'), ['-c show ndns', '-c show running-config'])

    def test_hung_running_config_is_bounded_without_dispatch(self):
        self.request(env={'FIXTURE_NDMC_MODE':'hang-running'}, expected=403, timeout=45)
        self.assertEqual(self.calls(), [])

    def test_proxy_stripped_origin_uses_explicit_page_origin_and_live_proof(self):
        self.request(env={'HTTP_ORIGIN': '', 'HTTP_X_BRORAY_ORIGIN': ORIGIN})
        self.assertEqual(self.calls(), ['resume'])
        self.assertEqual(self.calls('ndmc-calls'), ['-c show ndns', '-c show running-config'])

    def test_proxy_stripped_origin_without_page_origin_is_rejected(self):
        self.request(env={'HTTP_ORIGIN': '', 'HTTP_X_BRORAY_ORIGIN': ''}, expected=403)

    def test_proxy_page_origin_cannot_replace_present_invalid_origin(self):
        for value in ['null', 'broken', 'https://evil.invalid']:
            with self.subTest(value=value):
                self.request(env={'HTTP_ORIGIN': value, 'HTTP_X_BRORAY_ORIGIN': ORIGIN}, expected=403)

    def test_proxy_page_origin_requires_exact_publication(self):
        for value in ['http://192.168.1.1:8080', ORIGIN+':8443', ORIGIN+'/dns.html',
                      'https://broray.other.keenetic.link', ORIGIN+',https://evil.invalid']:
            with self.subTest(value=value):
                self.request(env={'HTTP_ORIGIN': '', 'HTTP_X_BRORAY_ORIGIN': value}, expected=403)

    def test_proxy_page_origin_requires_live_ownership(self):
        state=copy.deepcopy(PUBLICATION)
        state['ownership']['liveBlockOwnedExact']=False
        self.save_publication(state)
        self.request(env={'HTTP_ORIGIN': '', 'HTTP_X_BRORAY_ORIGIN': ORIGIN}, expected=403)

    def test_proxy_page_origin_still_requires_session_and_request_header(self):
        for headers in [{'HTTP_COOKIE': ''}, {'HTTP_X_BRORAY_REQUEST': ''}]:
            with self.subTest(headers=headers):
                self.request(env={'HTTP_ORIGIN': '', 'HTTP_X_BRORAY_ORIGIN': ORIGIN, **headers},
                             expected=401 if 'HTTP_COOKIE' in headers else 403)

    def test_origin_and_page_origin_disagreement_is_rejected(self):
        self.request(env={'HTTP_X_BRORAY_ORIGIN': 'https://broray.other.keenetic.link'}, expected=403)

    def test_proxy_pause_stop_cancel_and_recovery_dispatch(self):
        self.request(body={'paused': True})
        self.request('stop-background', {'pauseAutomation': True}, expected=202)
        self.request('cancel', {'operationId': VALID_OPERATION}, expected=202)
        self.request('recover', {}, expected=202)
        self.assertEqual(self.calls(), ['pause', 'stop-background', 'cancel ' + VALID_OPERATION, 'recover'])

    def test_unauthenticated_read_and_write_are_rejected_before_lookup(self):
        self.request(env={'HTTP_COOKIE': ''}, expected=401)
        self.request('status', method='GET', env={'HTTP_COOKIE': ''}, expected=401)
        self.assertEqual(self.calls('ndmc-calls'), [])

    def test_missing_custom_header_is_rejected_before_lookup(self):
        self.request(env={'HTTP_X_BRORAY_REQUEST': ''}, expected=403)
        self.assertEqual(self.calls('ndmc-calls'), [])

    def test_same_host_does_not_need_publication_or_ndmc(self):
        (self.root / 'lib/web-publish.sh').unlink()
        self.request(env={'HTTP_ORIGIN': 'http://192.168.1.1:8080'})
        self.assertEqual(self.calls('ndmc-calls'), [])

    def test_get_does_not_need_origin_helper(self):
        self.helper.unlink()
        self.request('status', method='GET', env={'HTTP_ORIGIN': '', 'HTTP_HOST': ''})
        self.assertEqual(self.calls('ndmc-calls'), [])

    def test_missing_helper_fails_closed(self):
        self.helper.unlink()
        self.request(expected=503)
        self.assertEqual(self.calls(), [])

    def test_symlink_helper_fails_closed(self):
        dest=self.helper.with_suffix('.real'); self.helper.rename(dest); self.helper.symlink_to(dest)
        self.request(expected=503)
        self.assertEqual(self.calls(), [])

    def test_options_and_get_cannot_mutate(self):
        self.request(method='GET', expected=405)
        self.request(method='OPTIONS', expected=405)
        self.assertEqual(self.calls('ndmc-calls'), [])

    def test_forwarded_headers_cannot_establish_trust(self):
        self.request(env={'HTTP_ORIGIN': 'https://broray.attacker.keenetic.link',
            'HTTP_X_FORWARDED_HOST': 'broray.attacker.keenetic.link',
            'HTTP_X_FORWARDED_PROTO': 'https',
            'HTTP_FORWARDED': 'host=broray.attacker.keenetic.link;proto=https'}, expected=403)

    def test_forwarded_garbage_does_not_override_confirmed_origin(self):
        self.request(env={'HTTP_X_FORWARDED_HOST':'evil.invalid, internal',
                          'HTTP_X_FORWARDED_PROTO':'http', 'HTTP_FORWARDED':'bad'})

    def test_backend_query_and_origin_never_become_commands(self):
        self.request(env={'QUERY_STRING': 'command=system+reboot'}, expected=400)
        self.request(env={'HTTP_ORIGIN': ORIGIN + ';reboot'}, expected=403)
        self.assertEqual(self.calls('ndmc-calls'), [])

    def test_exact_published_origin_does_not_allow_siblings_or_other_ports(self):
        for origin in [ORIGIN+':8443', ORIGIN.replace('https:', 'http:'),
                       ORIGIN+'.attacker.invalid', 'https://broray.other.keenetic.link',
                       'https://other.qa-router.keenetic.link']:
            with self.subTest(origin=origin):
                self.request(env={'HTTP_ORIGIN':origin},expected=403)

    def test_https_default_port_and_hostname_case_are_normalized(self):
        self.request(env={'HTTP_ORIGIN':'https://BRORAY.QA-ROUTER.KEENETIC.LINK:443'})
        self.assertEqual(self.calls(), ['resume'])

    def test_empty_and_malformed_host_are_rejected(self):
        for host in ['', '192.168.1.1:8080,evil.invalid', 'admin@192.168.1.1:8080',
                     '192.168.1.1:8080\n', '192.168.1.1/path', ':8080']:
            with self.subTest(host=host):
                self.request(env={'HTTP_HOST':host},expected=403)
        self.assertEqual(self.calls('ndmc-calls'), [])

    def test_internally_consistent_but_malformed_publication_address_is_rejected(self):
        for name,domain in [('-bad','keenetic.link'),('qa-router','keenetic.link/evil'),
                            ('qa-router\n','keenetic.link'),('qa-router','evil..invalid'),
                            ('qa-router','keenetic.link:8443'),('qa-router','keenetic.link:443')]:
            state=copy.deepcopy(PUBLICATION)
            state['keenDns'].update(name=name,domain=domain)
            state['address']='https://broray.'+name+'.'+domain+'/'
            self.save_publication(state)
            with self.subTest(name=name,domain=domain):
                self.request(expected=403)

    def test_missing_publication_fields_cannot_establish_trust(self):
        for key in ['ownership','keenDns','recoveryRequired','consistent']:
            state=copy.deepcopy(PUBLICATION); state.pop(key); self.save_publication(state)
            with self.subTest(key=key): self.request(expected=403)

    def test_body_validation_still_applies(self):
        for body, code in [('broken',400), ('{}{}',400), ('[]',400), ('null',400),
                           (' ' * 4097,413), ({'paused':'false'},400), ({'paused':False,'pid':99},400)]:
            with self.subTest(body=str(body)[:40]): self.request(body=body, expected=code)
        self.request(env={'CONTENT_TYPE':'text/plain'}, expected=415)
        self.request(body='{}', env={'CONTENT_LENGTH':'4'}, expected=400)

    def test_timeout_is_bounded_and_leaves_no_workspace(self):
        start=time.monotonic()
        self.request(env={'FIXTURE_NDMC_MODE':'hang'}, expected=403)
        self.assertLess(time.monotonic()-start, 6)
        self.assertEqual(self.calls(), [])

    def test_timeout_uses_busybox_applet_too(self):
        if not BUSYBOX: self.fail('BusyBox required for this test')
        (self.root/'bin/timeout').write_text('#!/bin/sh\nexec '+shlex.quote(BUSYBOX)+' timeout "$@"\n')
        (self.root/'bin/timeout').chmod(0o700)
        self.request()

    def test_read_commands_are_allowlisted(self):
        self.request(env={'FIXTURE_PUBLISH_MODE':'write'}, expected=403)
        self.assertEqual(self.calls('ndmc-calls'), ['-c show ndns', '-c show running-config'])

    def test_missing_publication_library_fails_closed(self):
        (self.root/'lib/web-publish.sh').unlink(); self.request(expected=403)

    def test_symlink_publication_library_fails_closed(self):
        p=self.root/'lib/web-publish.sh'; q=p.with_suffix('.real'); p.rename(q); p.symlink_to(q)
        self.request(expected=403)

    def test_duplicate_ndns_name_is_not_first_match_wins(self):
        with (self.root/'ndns.txt').open('a') as f: f.write(' name: attacker\n')
        self.request(expected=403)

    def test_duplicate_ndns_domain_is_rejected(self):
        with (self.root/'ndns.txt').open('a') as f: f.write(' domain: keenetic.link\n')
        self.request(expected=403)

    def test_ndmc_failure_does_not_dispatch(self):
        self.request(env={'FIXTURE_NDMC_MODE':'fail'}, expected=403)

    def test_ambiguous_or_invalid_json_is_rejected(self):
        for raw in ['', '{}{}', 'null', '[]', 'broken']:
            (self.root/'publication.json').write_text(raw)
            self.request(expected=403)

    def test_jq_filters_do_not_need_optional_regex(self):
        # A portability guard, not a claim to have run Entware's exact jq binary.
        filters=re.findall(r"jq -ers '([^']*)'", self.helper.read_text(), flags=re.S)
        self.assertEqual(len(filters),1, 'Expected one publication jq filter')
        self.assertNotRegex(filters[0], r'\b(?:test|match|capture|sub|gsub|scan|splits)\s*\(')
        self.request()

    def test_report_remains_attachment_without_origin_lookup(self):
        headers,_=self.request('report', method='GET', env={'HTTP_ORIGIN':''})
        self.assertIn(b'Content-Disposition: attachment;',headers)
        self.assertEqual(self.calls('ndmc-calls'), [])

    def test_downstream_busy_response_is_not_changed(self):
        self.request(env={'FIXTURE_RESPONSE':'{"ok":false,"errorCode":"OPERATION_BUSY"}',
                          'FIXTURE_RC':'1'}, expected=409)


# Each generated item is a separate unittest, so reported test counts are real.
ACCEPT_ORIGINS = {
 'http_local':('http://192.168.1.1:8080','http://192.168.1.1:8080'),
 'https_case_default':('https://BRORAY.QA-ROUTER.keenetic.link:443',ORIGIN),
 'http_default':('http://router.example:80','http://router.example'),
 'ipv6_loopback':('http://[::1]:8080','http://[::1]:8080'),
 'ipv6_full':('https://[2001:DB8:1:2:3:4:5:6]:443','https://[2001:db8:1:2:3:4:5:6]'),
 'dns_single_label':('http://keenetic','http://keenetic'),
}
REJECT_ORIGINS = {
 'empty':'', 'null':'null', 'trailing_slash':ORIGIN+'/', 'path':ORIGIN+'/page',
 'userinfo':'https://admin@broray.qa-router.keenetic.link',
 'backslash':'https://broray.qa-router.keenetic.link\\evil',
 'newline':ORIGIN+'\n', 'carriage_return':ORIGIN+'\r', 'tab':ORIGIN+'\t',
 'space':ORIGIN+' ', 'two_origins':ORIGIN+' https://evil.invalid',
 'comma_list':ORIGIN+',https://evil.invalid', 'query':ORIGIN+'?x=1', 'fragment':ORIGIN+'#x',
 'percent':'https://%62roray.qa-router.keenetic.link', 'wildcard':'https://*.keenetic.link',
 'bad_label':'https://broray.-qa.keenetic.link', 'double_dot':'https://broray..keenetic.link',
 'trailing_dot':'https://broray.qa-router.keenetic.link.', 'unicode':'https://брорей.keenetic.link',
 'empty_port':ORIGIN+':', 'zero_port':ORIGIN+':0', 'oversize_port':ORIGIN+':65536',
 'ambiguous_port':ORIGIN+':0443', 'ipv6_broken':'http://[:::1]:8080',
 'ipv6_two_compressions':'http://[1::2::3]:8080', 'ipv6_short':'http://[1:2:3]:8080',
 'ipv6_zone':'http://[fe80::1%25eth0]:8080', 'label_length':'https://'+('a'*64)+'.example',
 'unbounded':'https://'+('x'*321), 'protocol':'ftp://broray.qa-router.keenetic.link',
}
PROOF_FAILURES = {
 'disabled':('enabled',False), 'state':('state','disabled'),
 'inconsistent':('consistent',False), 'recovery':('recoveryRequired',True),
 'keenDns_unavailable':('keenDns.available',False), 'no_block':('ownership.liveBlockPresent',False),
 'no_receipt':('ownership.receiptPresent',False), 'invalid_receipt':('ownership.receiptValid',False),
 'different_live_block':('ownership.liveBlockOwnedExact',False), 'schema':('schemaVersion',2),
 'string_boolean':('enabled','true'), 'untrusted_url':('address','https://evil.invalid/'),
 'path_in_domain':('keenDns.domain','keenetic.link/evil'), 'name_with_dot':('keenDns.name','qa.router'),
}

def accept_test(value, expected):
    def test(self):
        r=self.normalize(value); self.assertEqual(r.returncode,0,r.stderr.decode())
        self.assertEqual(r.stdout.decode().strip(),expected)
    return test

def reject_test(value):
    def test(self):
        r=self.normalize(value); self.assertNotEqual(r.returncode,0); self.assertEqual(r.stdout,b'')
    return test

def proof_test(path, value):
    def test(self):
        state=copy.deepcopy(PUBLICATION); target=state; parts=path.split('.')
        for key in parts[:-1]: target=target[key]
        target[parts[-1]]=value; self.save_publication(state); self.request(expected=403)
    return test

for name,(value,expected) in ACCEPT_ORIGINS.items():
    setattr(OperationsOrigin,'test_normalize_accept_'+name,accept_test(value,expected))
for name,value in REJECT_ORIGINS.items():
    setattr(OperationsOrigin,'test_normalize_reject_'+name,reject_test(value))
for name,(path,value) in PROOF_FAILURES.items():
    setattr(OperationsOrigin,'test_proof_reject_'+name,proof_test(path,value))

if __name__ == '__main__':
    unittest.main(verbosity=2, failfast=True)
