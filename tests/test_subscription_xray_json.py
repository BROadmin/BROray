"""Xray JSON subscriptions through the actual extractor, importer and updater."""
import base64
import copy
import ctypes
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path
from test_subscription_jobs import ROOT, SubscriptionJobs


def profile(address='vpn.example.invalid', network='grpc', security='reality', name='Fixture #1'):
    stream = {'network': network, 'security': security}
    if network == 'grpc':
        stream['grpcSettings'] = {'serviceName': 'api/a?b&c', 'authority': 'authority.example.invalid', 'mode': False}
    else:
        stream['tcpSettings'] = {}
    if security == 'reality':
        stream['realitySettings'] = {'serverName': 'sni.example.invalid', 'fingerprint': 'firefox',
            'publicKey': 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA', 'shortId': 'aabbccdd'}
    else:
        stream['tlsSettings'] = {'serverName': 'tls.example.invalid', 'fingerprint': 'chrome', 'alpn': ['h2', 'http/1.1']}
    return {'remarks': name, 'dns': {'servers': ['https://dns.example.invalid/dns-query']},
        'inbounds': [], 'routing': {'rules': []}, 'outbounds': [
            {'protocol': 'vless', 'tag': 'proxy', 'settings': {'vnext': [{'address': address, 'port': 443,
                'users': [{'id': '11111111-2222-4333-8444-555555555555', 'encryption': 'none', 'flow': ''}]}]},
             'streamSettings': stream, 'fragment': {'packets': 'tlshello', 'length': '100-200', 'interval': '10-20'}},
            {'protocol': 'freedom', 'tag': 'direct'}, {'protocol': 'blackhole', 'tag': 'block'}]}


class SubscriptionXrayJson(unittest.TestCase):
    def setUp(self):
        self.temp = Path(tempfile.mkdtemp(prefix='subscription-json-'))
        self.app = self.temp/'app'
        shutil.copytree(ROOT/'implementation/runtime/app/lib', self.app/'lib')
        (self.app/'tmp').mkdir()
        self.env = os.environ | {'BRORAY_ROOT': str(self.app), 'BRORAY_BASE': str(self.app),
            'PATH': '/usr/bin:/bin:/usr/sbin:/sbin'}

    def tearDown(self):
        assert self.temp.resolve().parent == Path('/tmp') and self.temp.name.startswith('subscription-json-')
        shutil.rmtree(self.temp)

    def extract(self, payload, stage=True, limit=500):
        data = payload if isinstance(payload, bytes) else json.dumps(payload, ensure_ascii=False).encode()
        (self.app/'input').write_bytes(data)
        script = '''. "$BRORAY_ROOT/lib/subscription-service.sh"
rc=0
broray_subscription_extract_nodes "$BRORAY_ROOT/input" "$BRORAY_ROOT/nodes" || rc=$?
if [ "$rc" = 0 ] && [ "$TEST_STAGE" = 1 ]; then
 broray_subscription_stage_nodes fixture "$BRORAY_ROOT/nodes" "$BRORAY_ROOT/stage" true || rc=$?
fi
jq -nc --argjson rc "$rc" --arg code "${BRORAY_SUB_ERROR_CODE:-}" \
 --argjson received "${BRORAY_SUB_RECEIVED:-0}" --argjson accepted "${BRORAY_SUB_ACCEPTED:-0}" \
 --argjson rejected "${BRORAY_SUB_REJECTED:-0}" \
 '{rc:$rc,errorCode:$code,received:$received,accepted:$accepted,rejected:$rejected}'
'''
        p = subprocess.run(['/bin/ash', '-c', script], env=self.env | {
            'TEST_STAGE': '1' if stage else '0', 'BRORAY_SUB_MAX_NODES': str(limit)}, capture_output=True, timeout=45)
        self.assertEqual(p.returncode, 0, p.stderr)
        result = json.loads(p.stdout)
        servers = [json.loads(f.read_bytes()) for f in sorted((self.app/'stage').glob('*.json'))]
        warnings = ''.join(f.read_text() for f in (self.app/'tmp').glob('subscription-warnings.*.txt'))
        return result, servers, warnings

    def test_array_imports_grpc_reality_tls_tcp_and_preserves_fields(self):
        data = [profile(), profile('tls.example.invalid', security='tls', name='TLS'),
                profile('tcp.example.invalid', network='tcp', name='TCP')]
        data[2]['outbounds'][0]['settings']['vnext'][0]['users'][0]['flow'] = 'xtls-rprx-vision'
        result, servers, warnings = self.extract(data)
        self.assertEqual(result['rc'], 0, result)
        self.assertEqual(result['accepted'], 3)
        by_name = {s['name']: s for s in servers}
        grpc = by_name['Fixture #1']
        self.assertEqual(grpc['transport']['serviceName'], 'api/a?b&c')
        self.assertEqual(grpc['transport']['host'], 'authority.example.invalid')
        self.assertEqual(grpc['reality']['shortId'], 'aabbccdd')
        self.assertEqual(grpc['reality']['fingerprint'], 'firefox')
        self.assertEqual(by_name['TLS']['tls']['alpn'], ['h2', 'http/1.1'])
        self.assertEqual(by_name['TCP']['flow'], 'xtls-rprx-vision')
        self.assertEqual(by_name['TCP']['network'], 'raw')
        self.assertTrue(all(s['source']['subscriptionId'] == 'fixture' for s in servers))
        self.assertIn('балансиров', warnings)

    def test_balancer_nodes_are_deduplicated_with_same_subscription(self):
        one = profile(name='First')
        balance = profile(name='Balanced')
        balance['outbounds'].insert(1, copy.deepcopy(profile('second.example.invalid')['outbounds'][0]))
        balance['outbounds'][1]['tag'] = 'second'
        balance['routing']['balancers'] = [{'tag': 'balance', 'selector': ['proxy', 'second']}]
        balance['observatory'] = {'subjectSelector': ['proxy', 'second']}
        result, servers, _ = self.extract([one, balance])
        self.assertEqual(result['rc'], 0, result)
        self.assertEqual((result['received'], result['accepted'], result['rejected']), (3, 2, 1))
        self.assertEqual({s['address'] for s in servers}, {'vpn.example.invalid', 'second.example.invalid'})

    def test_single_object_and_base64_json(self):
        for payload in [profile(), base64.b64encode(json.dumps([profile()]).encode())]:
            with self.subTest(encoded=isinstance(payload, bytes)):
                result, servers, _ = self.extract(payload)
                self.assertEqual(result['rc'], 0, result)
                self.assertEqual(len(servers), 1)

    def test_malformed_and_unknown_json_never_produce_nodes(self):
        for payload in [b'[{"outbounds":', {'servers': []}, [profile(), 'bad'], b'{} {}']:
            with self.subTest(payload_type=type(payload).__name__):
                result, _, _ = self.extract(payload, stage=False)
                self.assertNotEqual(result['rc'], 0)
                self.assertFalse((self.app/'nodes').exists())

    def test_unsupported_connection_settings_are_rejected_without_secrets_in_warnings(self):
        node = profile()
        node['outbounds'][0]['streamSettings']['sockopt'] = {'dialerProxy': 'PRIVATE_CANARY'}
        result, servers, warnings = self.extract([node])
        self.assertEqual(result['errorCode'], 'NO_VALID_NODES')
        self.assertEqual(result['rejected'], 1)
        self.assertEqual(servers, [])
        self.assertNotIn('PRIVATE_CANARY', warnings)

    def test_json_node_limit_counts_expanded_endpoints(self):
        result, _, _ = self.extract([profile(), profile('other.example.invalid')], stage=False, limit=1)
        self.assertEqual(result['errorCode'], 'CONTENT_TOO_LARGE')
        self.assertFalse((self.app/'nodes').exists())

    def test_text_and_base64_uri_formats_still_work_without_json_warning(self):
        uri = b'vless://11111111-2222-4333-8444-555555555555@vpn.example.invalid:443?security=tls&type=tcp#Text'
        for payload in [uri, base64.b64encode(uri)]:
            result, servers, warnings = self.extract(payload)
            self.assertEqual(result['rc'], 0, result)
            self.assertEqual(servers[0]['name'], 'Text')
            self.assertEqual(warnings, '')


class SubscriptionXrayJsonUpdate(unittest.TestCase):
    setUp = SubscriptionJobs.setUp
    clean_fixture = SubscriptionJobs.clean_fixture
    record = SubscriptionJobs.record
    states = SubscriptionJobs.states
    shell = SubscriptionJobs.shell
    job_script = SubscriptionJobs.job_script

    def update(self, data, expected=0):
        self.payload.write_text(json.dumps(data))
        return self.shell(self.job_script('broray_subscription_update test manual'), expected=expected, timeout=120)

    def test_repeated_update_keeps_ids_and_other_sources(self):
        path = self.record()
        self.update([profile()])
        servers = self.app/'servers'
        first = next(servers.glob('*.json'))
        original = json.loads(first.read_bytes())
        manual = servers/'manual-fixture.json'
        manual.write_text(json.dumps(original | {'id': 'manual-fixture', 'source': {'type': 'manual'}}))
        other = servers/'subscription-other-fixture.json'
        other.write_text(json.dumps(original | {'id': 'subscription-other-fixture',
            'source': original['source'] | {'subscriptionId': 'other'}}))
        foreign_before = [manual.read_bytes(), other.read_bytes()]
        changed = profile(name='Renamed')
        changed['outbounds'][0]['settings']['vnext'][0]['users'][0]['id'] = 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee'
        self.update([changed, profile('new.example.invalid', name='New')])
        self.assertEqual([manual.read_bytes(), other.read_bytes()], foreign_before)
        current = json.loads(first.read_bytes())
        self.assertEqual(current['id'], original['id'])
        self.assertEqual(current['name'], 'Renamed')
        self.assertEqual(current['uuid'], 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee')
        owned = [json.loads(f.read_bytes()) for f in servers.glob('*.json')
            if json.loads(f.read_bytes())['source'].get('subscriptionId') == 'test']
        self.assertEqual(len(owned), 2)
        self.assertEqual(json.loads(path.read_bytes())['lastUpdateStatus'], 'success')
        self.assertFalse((self.temp/'global.lock').is_symlink())

    def test_invalid_json_update_preserves_previous_servers(self):
        path = self.record()
        self.update([profile()])
        before = {f.name: f.read_bytes() for f in (self.app/'servers').glob('*.json')}
        self.update({'servers': []}, expected=1)
        self.assertEqual({f.name: f.read_bytes() for f in (self.app/'servers').glob('*.json')}, before)
        self.assertEqual(json.loads(path.read_bytes())['lastUpdateStatus'], 'error')


if __name__ == '__main__':
    if os.name == 'nt': raise SystemExit('Run in isolated Linux guest')
    assert ctypes.CDLL(None).prctl(36, 1, 0, 0, 0) == 0
    suite = unittest.TestSuite(unittest.defaultTestLoader.loadTestsFromTestCase(c)
        for c in [SubscriptionXrayJson, SubscriptionXrayJsonUpdate])
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    (ROOT/'docs/evidence/subscription-xray-json-tests.json').write_text(json.dumps({
        'status': 'PASS' if result.wasSuccessful() else 'FAIL', 'testsRun': result.testsRun,
        'environment': 'Real Linux extractor/importer; protected update with fixture HTTP transport',
        'routerAccessed': False, 'providerAccessed': False})+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
