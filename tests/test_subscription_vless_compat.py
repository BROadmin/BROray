"""Regex-free JSON adapter and exact generator function; no HTTP or router calls.

BRORAY_TEST_ROOT points to a repo root (runtime/app/lib). Default: this repo.
STAGE05_REPRO=1 runs only the pre-fix regression requirements.
The separate Linux pipeline suite executes the actual extractor/importer/generator.
"""
from __future__ import annotations
import copy
import ipaddress
import json
import os
from pathlib import Path
import random
import shutil
import subprocess
import unittest
from urllib.parse import parse_qs, unquote, urlsplit

ROOT = Path(os.environ.get('BRORAY_TEST_ROOT', Path(__file__).resolve().parents[1]))
ADAPTER = ROOT / 'runtime/app/lib/subscription-xray-json.jq'
GENERATOR = ROOT / 'runtime/app/lib/server-config-generator.sh'
JQ = os.environ.get('BRORAY_TEST_JQ', shutil.which('jq') or '')
MARKER = 'broray-json-error://unsupported-node'
UUID = '11111111-2222-4333-8444-555555555555'
KEY = 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'


def profile(network='tcp', security='tls', flat=False, address='vpn.example.invalid', **options):
    user = {'id': UUID, 'encryption': 'none', 'flow': ''}
    settings = dict(address=address, port=443, **user) if flat else {'vnext': [dict(address=address, port=443, users=[user])]}
    s = {'network': network, 'security': security}
    if network in ('tcp', 'raw'): s['rawSettings' if network == 'raw' else 'tcpSettings'] = {}
    elif network in ('ws', 'websocket'): s['wsSettings'] = {'host': 'front.example.invalid', 'path': '/a+b%20?q=x&n=1'}
    elif network in ('httpupgrade','httpUpgrade'): s['httpupgradeSettings'] = {'host': 'front.example.invalid', 'path': '/up?q=1'}
    elif network in ('xhttp','splithttp'): s['xhttpSettings'] = {'host': 'front.example.invalid', 'path': '/xh', 'mode': 'auto'}
    elif network == 'grpc': s['grpcSettings'] = {'serviceName': 'api/a+b%20?c&d', 'authority': 'front.example.invalid', 'multiMode': True}
    if security == 'tls': s['tlsSettings'] = {'serverName': 'sni.example.invalid', 'alpn': ['h2','http/1.1'], 'fingerprint': 'firefox'}
    elif security == 'reality': s['realitySettings'] = {'serverName': 'sni.example.invalid', 'publicKey': KEY, 'shortId': '01234567', 'spiderX': '/a+b%2F'}
    result = {'remarks': 'Тест + # % / 東京', 'outbounds': [{'protocol':'vless', 'tag':'proxy', 'settings':settings, 'streamSettings':s}]}
    if options: result.update(options)
    return result


def outbound(p): return p['outbounds'][0]
def stream(p): return outbound(p)['streamSettings']
def user(p):
    s = outbound(p)['settings']
    return s if 'vnext' not in s else s['vnext'][0]['users'][0]
def changed(p, fn): fn(p); return p


class Adapter(unittest.TestCase):
    def run_adapter(self, data, limit=500, raw=False):
        self.assertTrue(JQ, 'jq is required; missing dependency is not a pass')
        p = subprocess.run([JQ, '-rs', '--argjson','max_nodes',str(limit),'-f',str(ADAPTER)],
                           input=data if raw else json.dumps(data, ensure_ascii=False), text=True,
                           encoding='utf-8', capture_output=True, timeout=10)
        return p
    def accepted(self, data):
        p = self.run_adapter(data)
        self.assertEqual(p.returncode,0,p.stderr)
        lines = p.stdout.splitlines()
        self.assertEqual(len(lines),1,p.stdout)
        self.assertTrue(lines[0].startswith('vless://'),p.stdout)
        uri = urlsplit(lines[0]); q = {k:v[0] for k,v in parse_qs(uri.query,keep_blank_values=True).items()}
        return uri,q
    def rejected(self,data):
        p = self.run_adapter(data)
        self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual(p.stdout.strip(), MARKER, p.stdout)
        self.assertNotIn('PRIVATE_CANARY',p.stdout+p.stderr)
    def test_regression_ws(self):
        uri,q=self.accepted(profile('ws')); self.assertEqual(q['path'],'/a+b%20?q=x&n=1');self.assertEqual(q['host'],'front.example.invalid')
    def test_regression_httpupgrade(self):
        _,q=self.accepted(profile('httpupgrade'));self.assertEqual(q['type'],'httpupgrade')
    def test_regression_xhttp(self):
        _,q=self.accepted(profile('xhttp'));self.assertEqual(q['host'],'front.example.invalid');self.assertEqual(q['type'],'xhttp')
    def test_regression_flat(self): self.accepted(profile(flat=True))
    def test_regression_password_alias(self):
        p=profile(security='reality');r=stream(p)['realitySettings'];r['password']=r.pop('publicKey');_,q=self.accepted(p);self.assertEqual(q['pbk'],KEY)
    def test_regression_ipv6(self):
        uri,_=self.accepted(profile(address='2001:db8::1'));self.assertEqual(uri.hostname,'2001:db8::1');self.assertIn('@[2001:db8::1]:443',uri.geturl())
    def test_regression_no_partial_outbound(self):
        p=profile();user2=copy.deepcopy(user(p));user2['encryption']='PRIVATE_CANARY';outbound(p)['settings']['vnext'][0]['users'].append(user2);self.rejected(p)
    def test_regression_generator_xhttp_host(self):
        source=GENERATOR.read_text(encoding='utf-8')
        part=source[source.index('        def xhttp_settings($s):'):source.index('        def raw_settings($s):')]
        node={'protocol':'vless','transport':{'host':'front.example.invalid'},'xhttp':{'path':'/abc','mode':'auto','extra':{'noSSEHeader':True}}}
        p=subprocess.run([JQ,'-n','--argjson','s',json.dumps(node),part+' xhttp_settings($s)'],capture_output=True,text=True,timeout=10)
        self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual(json.loads(p.stdout),{'host':'front.example.invalid','path':'/abc','mode':'auto','extra':{'noSSEHeader':True}})
    def test_flat_and_legacy_are_identical(self):
        for net in ['tcp','grpc','ws','httpupgrade','xhttp']:
            a=self.run_adapter(profile(net));b=self.run_adapter(profile(net,flat=True));self.assertEqual(a.stdout,b.stdout)
    def test_password_alias_identical_to_publicKey(self):
        p=profile(security='reality');a=self.run_adapter(p);r=stream(p)['realitySettings'];r['password']=r.pop('publicKey');b=self.run_adapter(p);self.assertEqual(a.stdout,b.stdout)
    def test_unicode_name_and_percent_not_double_decoded(self):
        uri,q=self.accepted(profile('ws'));self.assertEqual(unquote(uri.fragment),'Тест + # % / 東京');self.assertIn('%20',q['path']);self.assertIn('+',q['path'])
    def test_grpc_authority_service_multimode(self):
        _,q=self.accepted(profile('grpc'));self.assertEqual(q['serviceName'],'api/a+b%20?c&d');self.assertEqual(q['mode'],'multi');self.assertEqual(q['host'],'front.example.invalid')
    def test_grpc_legacy_mode_false(self):
        p=profile('grpc');stream(p)['grpcSettings']={'mode':False};_,q=self.accepted(p);self.assertEqual(q['mode'],'gun')
    def test_reality_aliases_agree(self):
        p=profile(security='reality');stream(p)['realitySettings']['password']=KEY;self.accepted(p)
    def test_tls_default_and_no_security(self):
        self.accepted(profile(security='none')); p=profile();del outbound(p)['streamSettings'];self.accepted(p)
    def test_defaults_no_transport_objects(self):
        for n in ['tcp','grpc','ws','httpupgrade','xhttp']:
            p=profile(n);stream(p).pop(next(k for k in stream(p) if k.endswith('Settings') and k!='tlsSettings'));self.accepted(p)
    def test_vision_only_raw_reality(self):
        for n in ['tcp','raw']:
            p=profile(n,'reality');user(p)['flow']='xtls-rprx-vision';_,q=self.accepted(p);self.assertEqual(q['flow'],'xtls-rprx-vision')
    def test_ws_legacy_header(self):
        p=profile('ws');stream(p)['wsSettings']={'headers':{'Host':'legacy.example.invalid'},'path':'/w'};_,q=self.accepted(p);self.assertEqual(q['host'],'legacy.example.invalid')
    def test_ws_equal_host_aliases(self):
        p=profile('ws');stream(p)['wsSettings']['headers']={'Host':'front.example.invalid','host':'front.example.invalid'};self.accepted(p)
    def test_xhttp_nested_and_direct_options_equal(self):
        extra={'headers':{'X-Test':'value+% "テスト"'},'xPaddingBytes':'100-200','noSSEHeader':False,'noGRPCHeader':True,'scMaxEachPostBytes':1000000,'scMinPostsIntervalMs':'10-20','scMaxBufferedPosts':20,'scStreamUpServerSecs':'20-40','xmux':{'maxConcurrency':'4-8','cMaxReuseTimes':'0-5','hMaxRequestTimes':'100-200','hMaxReusableSecs':600,'hKeepAlivePeriod':30}}
        p=profile('xhttp');stream(p)['xhttpSettings']['extra']=extra;_,q=self.accepted(p);self.assertEqual(json.loads(q['extra']),extra)
        del stream(p)['xhttpSettings']['extra'];stream(p)['xhttpSettings'].update(extra);_,q=self.accepted(p);self.assertEqual(json.loads(q['extra']),extra)
    def test_multiple_outbounds_and_expanded_limit(self):
        p=profile();outbound(p)['settings']['vnext'].append({'address':'second.example.invalid','port':8443,'users':[{'id':UUID}]})
        out=self.run_adapter([p,profile('grpc')],limit=1);self.assertEqual(out.returncode,0);self.assertEqual(len(out.stdout.splitlines()),2)
    def test_direct_blackhole_and_profile_settings_not_emitted(self):
        p=profile();p.update(dns={'servers':['PRIVATE_CANARY']},inbounds=[{'port':999}],routing={'rules':[]});p['outbounds'] += [{'protocol':'freedom'},{'protocol':'blackhole'}]
        a=self.run_adapter(p);b=self.run_adapter(profile());self.assertEqual(a.stdout,b.stdout);self.assertNotIn('PRIVATE_CANARY',a.stdout+a.stderr)
    def test_malformed_documents(self):
        for raw in ['null','123','"x"','{}','{"servers":[]}','[{"outbounds":[]},0]','{} {}','{"outbounds":']:
            p=self.run_adapter(raw,raw=True);self.assertNotEqual(p.returncode,0);self.assertEqual(p.stdout,'')
    def test_empty_profile_list_is_empty(self): self.assertEqual(self.run_adapter([]).stdout,'')
    def test_no_regex_dependency(self):
        # Tokens in executable jq, not incidental words in comments.
        source='\n'.join(x.split('#',1)[0] for x in ADAPTER.read_text().splitlines())
        import re
        self.assertIsNone(re.search(r'\b(test|match|capture|scan|sub|gsub|splits)\s*\(',source))
    def test_deterministic_ipv6_sample(self):
        rng=random.Random(505)
        for _ in range(24):
            ip=ipaddress.IPv6Address(rng.getrandbits(128));uri,_=self.accepted(profile(address=ip.compressed));self.assertEqual(ipaddress.ip_address(uri.hostname),ip)
    def test_generator_non_vless_unchanged_and_no_host_default(self):
        source=GENERATOR.read_text();part=source[source.index('        def xhttp_settings($s):'):source.index('        def raw_settings($s):')]
        for node,expected in [({'protocol':'vless','xhttp':{'path':'/v','mode':'auto'}},{'path':'/v','mode':'auto'}),({'protocol':'trojan','transport':{'host':'t.example','path':'/t','mode':'stream-up'}},{'host':'t.example','path':'/t','mode':'stream-up'})]:
            p=subprocess.run([JQ,'-n','--argjson','s',json.dumps(node),part+' xhttp_settings($s)'],capture_output=True,text=True,timeout=10)
            self.assertEqual(p.returncode,0,p.stderr);self.assertEqual(json.loads(p.stdout),expected)


# Each vector is a distinct test; sub-samples above are not counted as separate tests.
def add_case(name, payload, ok, check=None):
    def case(self):
        if not ok: self.rejected(payload)
        else:
            uri,q=self.accepted(payload)
            if check: check(self,uri,q)
    setattr(Adapter,'test_'+name,case)

for net in ['raw','tcp','grpc','ws','websocket','httpupgrade','httpUpgrade','xhttp','splithttp']:
    add_case('network_'+net,profile(net),True)
for mode in ['auto','packet-up','stream-up','stream-one','']:
    add_case('xhttp_mode_'+(mode or 'default'),changed(profile('xhttp'),lambda p,m=mode: stream(p)['xhttpSettings'].update(mode=m)),True)
for i,ip in enumerate(['::','::1','2001:db8::1','2001:0db8:0000:0000:0000:0000:0000:0001','::ffff:192.0.2.1','[2001:db8::1]','0:0:0:0:0:ffff:192.0.2.1']):
    add_case('ipv6_valid_'+str(i),profile(address=ip),True)
for i,ip in enumerate([':::1','1::2::3','1:2:3:4:5:6:7','1:2:3:4:5:6:7:8:9','12345::1','gggg::1','fe80::1%eth0','[abc]','[::1','::1]','::ffff:999.0.0.1','::ffff:192.000.2.1','[::1]:80','a@b','host/path','x?x','x#x','x\nx','x\x00x','x\\x']):
    add_case('address_invalid_'+str(i),profile(address=ip),False)
for i,value in enumerate([0,65536,-1,1.5,'443',None,True]):
    add_case('port_invalid_'+str(i),changed(profile(flat=True),lambda p,v=value: outbound(p)['settings'].update(port=v)),False)
neg=[]
def bad(name,p,fn): neg.append((name,changed(p,fn)))
bad('conflicting_keys',profile(security='reality'),lambda p: stream(p)['realitySettings'].update(password='PRIVATE_CANARY'))
bad('empty_password',profile(security='reality'),lambda p: stream(p)['realitySettings'].update(password=''))
bad('null_reality_key',profile(security='reality'),lambda p: stream(p)['realitySettings'].update(publicKey=None))
bad('allow_insecure',profile(),lambda p: stream(p)['tlsSettings'].update(allowInsecure=True))
bad('alpn_empty_token',profile(),lambda p: stream(p)['tlsSettings'].update(alpn=['h2','']))
bad('alpn_comma',profile(),lambda p: stream(p)['tlsSettings'].update(alpn=['x,y']))
bad('alpn_wrong_type',profile(),lambda p: stream(p)['tlsSettings'].update(alpn='h2'))
bad('alpn_control',profile(),lambda p: stream(p)['tlsSettings'].update(alpn=['a\nb']))
bad('ws_extra_header',profile('ws'),lambda p: stream(p)['wsSettings'].update(headers={'X-Token':'PRIVATE_CANARY'}))
bad('ws_host_conflict',profile('ws'),lambda p: stream(p)['wsSettings'].update(headers={'Host':'other.invalid'}))
bad('ws_host_header_duplicate_conflict',profile('ws'),lambda p: stream(p)['wsSettings'].update(headers={'Host':'front.example.invalid','host':'other.invalid'}))
bad('ws_early_data',profile('ws'),lambda p: stream(p)['wsSettings'].update(maxEarlyData=2048))
bad('http_headers',profile('httpupgrade'),lambda p: stream(p)['httpupgradeSettings'].update(headers={'X-A':'x'}))
bad('unknown_transport',profile(),lambda p: stream(p).update(network='quic'))
bad('null_stream',profile(),lambda p: outbound(p).update(streamSettings=None))
bad('false_network',profile(),lambda p: stream(p).update(network=False))
bad('null_ws_settings',profile('ws'),lambda p: stream(p).update(wsSettings=None))
bad('irrelevant_grpc',profile('ws'),lambda p: stream(p).update(grpcSettings={}))
bad('sockopt_chain',profile(),lambda p: stream(p).update(sockopt={'dialerProxy':'PRIVATE_CANARY'}))
bad('proxy_chain',profile(),lambda p: outbound(p).update(proxySettings={'tag':'PRIVATE_CANARY'}))
bad('mixed_settings',profile(),lambda p: outbound(p)['settings'].update(address='other.invalid'))
bad('raw_aliases',profile('raw'),lambda p: stream(p).update(tcpSettings={}))
bad('xhttp_aliases',profile('xhttp'),lambda p: stream(p).update(splithttpSettings={}))
bad('xhttp_extra_conflict',profile('xhttp'),lambda p: stream(p)['xhttpSettings'].update(extra={},noGRPCHeader=True))
bad('xhttp_bad_mode',profile('xhttp'),lambda p: stream(p)['xhttpSettings'].update(mode='bad'))
bad('xhttp_download',profile('xhttp'),lambda p: stream(p)['xhttpSettings'].update(extra={'downloadSettings':{'address':'PRIVATE_CANARY'}}))
bad('xhttp_unknown_extra',profile('xhttp'),lambda p: stream(p)['xhttpSettings'].update(extra={'unknown':'PRIVATE_CANARY'}))
bad('xhttp_extra_false',profile('xhttp'),lambda p: stream(p)['xhttpSettings'].update(extra=False))
bad('xhttp_bad_headers',profile('xhttp'),lambda p: stream(p)['xhttpSettings'].update(extra={'headers':{'host':'x'}}))
bad('xhttp_header_control',profile('xhttp'),lambda p: stream(p)['xhttpSettings'].update(extra={'headers':{'X-A':'a\r\nb'}}))
bad('xmux_both_limits',profile('xhttp'),lambda p: stream(p)['xhttpSettings'].update(extra={'xmux':{'maxConcurrency':4,'maxConnections':5}}))
bad('xmux_unknown',profile('xhttp'),lambda p: stream(p)['xhttpSettings'].update(extra={'xmux':{'unknown':4}}))
bad('range_reversed',profile('xhttp'),lambda p: stream(p)['xhttpSettings'].update(extra={'xPaddingBytes':'200-100'}))
bad('range_negative',profile('xhttp'),lambda p: stream(p)['xhttpSettings'].update(extra={'scMinPostsIntervalMs':-1}))
bad('range_overflow',profile('xhttp'),lambda p: stream(p)['xhttpSettings'].update(extra={'xPaddingBytes':'2147483648'}))
bad('range_zero_to_positive',profile('xhttp'),lambda p: stream(p)['xhttpSettings'].update(extra={'xPaddingBytes':'0-100'}))
bad('xhttp_extra_too_large',profile('xhttp'),lambda p: stream(p)['xhttpSettings'].update(extra={'headers':{'X-A':'a'*4097}}))
bad('missing_users',profile(),lambda p: outbound(p)['settings']['vnext'][0].update(users=[]))
bad('wrong_encryption',profile(),lambda p: user(p).update(encryption='PRIVATE_CANARY'))
for n in ['ws','httpupgrade','grpc','xhttp']:
    bad('vision_'+n,profile(n,'reality'),lambda p: user(p).update(flow='xtls-rprx-vision'))
for n in ['ws','httpupgrade']:
    neg.append(('reality_'+n,profile(n,'reality')))
for name,p in neg: add_case(name,p,False)

if __name__=='__main__':
    loader=unittest.TestLoader()
    if os.environ.get('STAGE05_REPRO')=='1':
        suite=unittest.TestSuite(Adapter(n) for n in loader.getTestCaseNames(Adapter) if 'regression_' in n)
    else: suite=loader.loadTestsFromTestCase(Adapter)
    result=unittest.TextTestRunner(verbosity=2).run(suite)
    print(json.dumps({'testsRun':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),'skipped':len(result.skipped),'routerAccessed':False}))
    raise SystemExit(0 if result.wasSuccessful() else 1)
