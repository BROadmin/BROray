"""Actual Linux extraction, node staging, URI parsing, config generation and Xray -test.

All input is synthetic. No HTTP transport, provider, real router or proxy process.
The optional core gate is required when BRORAY_TEST_XRAY is supplied. It only
validates generated JSON (run -test), never starts listening or connects to peers.
"""
from __future__ import annotations
import base64
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from test_subscription_vless_compat import profile, stream, outbound, user

ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
XRAY=os.environ.get('BRORAY_TEST_XRAY','')
CORE_RESULTS=[]

class Pipeline(unittest.TestCase):
    def setUp(self):
        self.temp=Path(tempfile.mkdtemp(prefix='broray-vless-stage05-'))
        self.app=self.temp/'app'
        shutil.copytree(ROOT/'runtime/app/lib',self.app/'lib')
        for d in ['tmp','servers','config/system','logs']: (self.app/d).mkdir(parents=True,exist_ok=True)
        settings={'listenAddress':'127.0.0.1','socksPort':2080,'logLevel':'warning'}
        (self.app/'config/system/settings.json').write_text(json.dumps(settings),encoding='utf-8')
        self.env={**os.environ,'BRORAY_BASE':str(self.app),'BRORAY_ROOT':str(self.app),
            'BRORAY_SUB_BASE':str(self.app),'PATH':'/usr/bin:/bin:/usr/sbin:/sbin'}
    def tearDown(self):
        assert self.temp.parent.resolve()==Path(tempfile.gettempdir()).resolve()
        assert self.temp.name.startswith('broray-vless-stage05-')
        shutil.rmtree(self.temp)
    def shell(self,script,*args):
        return subprocess.run(['/bin/ash','-c',script,'stage05',*map(str,args)],env=self.env,
                              capture_output=True,timeout=45)
    def extract(self,payload):
        raw=payload if isinstance(payload,bytes) else json.dumps(payload,ensure_ascii=False).encode()
        file=self.app/'input';file.write_bytes(raw)
        p=self.shell('''. "$BRORAY_ROOT/lib/subscription-service.sh"
rc=0
broray_subscription_extract_nodes "$BRORAY_ROOT/input" "$BRORAY_ROOT/nodes" || rc=$?
if [ "$rc" = 0 ]; then
 broray_subscription_stage_nodes fixture "$BRORAY_ROOT/nodes" "$BRORAY_ROOT/stage" true || rc=$?
fi
jq -nc --argjson rc "$rc" --arg code "${BRORAY_SUB_ERROR_CODE:-}" \
 --argjson received "${BRORAY_SUB_RECEIVED:-0}" --argjson accepted "${BRORAY_SUB_ACCEPTED:-0}" \
 --argjson rejected "${BRORAY_SUB_REJECTED:-0}" \
 '{rc:$rc,errorCode:$code,received:$received,accepted:$accepted,rejected:$rejected}'
''')
        self.assertEqual(p.returncode,0,p.stderr.decode(errors='replace'))
        result=json.loads(p.stdout)
        nodes=[json.loads(f.read_bytes()) for f in sorted((self.app/'stage').glob('*.json'))]
        return result,nodes
    def generate(self,node):
        (self.app/'servers'/f"{node['id']}.json").write_text(json.dumps(node),encoding='utf-8')
        p=self.shell('. "$BRORAY_ROOT/lib/server-config-generator.sh"; broray_generate_server_config "$1"',node['id'])
        self.assertEqual(p.returncode,0,p.stderr.decode(errors='replace'))
        file=Path(p.stdout.decode().strip());self.assertTrue(file.is_relative_to(self.app))
        config=json.loads(file.read_bytes())
        self.assertEqual(config['inbounds'][0]['listen'],'127.0.0.1')
        self.assertEqual(len(config['outbounds']),1)
        self.assertNotIn('dns',config);self.assertNotIn('routing',config)
        if XRAY:
            core=subprocess.run([XRAY,'run','-test','-config',str(file)],capture_output=True,timeout=20,env=self.env)
            text=(core.stdout+core.stderr).decode(errors='replace')
            self.assertEqual(core.returncode,0,text)
            self.assertIn('Configuration OK',text)
            CORE_RESULTS.append({'case':self.id(),'network':node['network'],'security':node['security'],
                'configurationSha256':hashlib.sha256(file.read_bytes()).hexdigest(),'returncode':0})
        return config['outbounds'][0]
    def one(self,payload):
        result,nodes=self.extract(payload)
        self.assertEqual(result['rc'],0,result);self.assertEqual(result['accepted'],1,result)
        self.assertEqual(len(nodes),1)
        self.assertEqual(nodes[0]['source']['subscriptionId'],'fixture')
        return nodes[0],self.generate(nodes[0])
    def test_tcp_tls(self):
        n,c=self.one(profile());self.assertEqual(c['streamSettings']['network'],'raw')
        self.assertEqual(c['streamSettings']['tlsSettings']['serverName'],'sni.example.invalid')
    def test_grpc_tls(self):
        n,c=self.one(profile('grpc'));g=c['streamSettings']['grpcSettings']
        self.assertEqual(g,{'serviceName':'api/a+b%20?c&d','authority':'front.example.invalid','multiMode':True})
    def test_ws_tls_unicode_percent(self):
        n,c=self.one(profile('ws'));self.assertEqual(n['name'],'Тест + # % / 東京')
        self.assertEqual(c['streamSettings']['wsSettings'],{'host':'front.example.invalid','path':'/a+b%20?q=x&n=1'})
    def test_ws_legacy_host_header(self):
        p=profile('ws');w=stream(p)['wsSettings'];w['headers']={'Host':w.pop('host')}
        _,c=self.one(p);self.assertEqual(c['streamSettings']['wsSettings']['host'],'front.example.invalid')
    def test_httpupgrade_tls(self):
        _,c=self.one(profile('httpupgrade'));self.assertEqual(c['streamSettings']['httpupgradeSettings'],{'host':'front.example.invalid','path':'/up?q=1'})
    def test_xhttp_tls_host_and_tuning(self):
        p=profile('xhttp');stream(p)['xhttpSettings']['extra']={'noSSEHeader':True,'xPaddingBytes':'100-200','xmux':{'maxConnections':'2-3'}}
        n,c=self.one(p);x=c['streamSettings']['xhttpSettings']
        self.assertEqual(x['host'],'front.example.invalid');self.assertEqual(x['extra'],stream(p)['xhttpSettings']['extra'])
    def test_xhttp_direct_tuning_preserved(self):
        p=profile('xhttp');stream(p)['xhttpSettings'].update({'noSSEHeader':True,'scMinPostsIntervalMs':10})
        _,c=self.one(p);self.assertEqual(c['streamSettings']['xhttpSettings']['extra'],{'noSSEHeader':True,'scMinPostsIntervalMs':10})
    def test_reality_password_tcp_vision(self):
        p=profile('tcp','reality');r=stream(p)['realitySettings'];r['password']=r.pop('publicKey');user(p)['flow']='xtls-rprx-vision'
        n,c=self.one(p);self.assertEqual(c['streamSettings']['realitySettings']['publicKey'],r['password'])
        self.assertEqual(c['settings']['vnext'][0]['users'][0]['flow'],'xtls-rprx-vision')
    def test_reality_grpc(self):
        n,c=self.one(profile('grpc','reality'));self.assertEqual(c['streamSettings']['realitySettings']['shortId'],'01234567')
    def test_reality_xhttp(self):
        _,c=self.one(profile('xhttp','reality'));self.assertEqual(c['streamSettings']['xhttpSettings']['host'],'front.example.invalid')
    def test_flat_settings(self):
        _,c=self.one(profile('ws',flat=True));self.assertEqual(c['settings']['vnext'][0]['address'],'vpn.example.invalid')
    def test_ipv6(self):
        _,c=self.one(profile(address='2001:db8::1'));self.assertEqual(c['settings']['vnext'][0]['address'],'[2001:db8::1]')
    def test_ipv6_mapped_tail(self):
        _,c=self.one(profile(address='::ffff:192.0.2.1'));self.assertEqual(c['settings']['vnext'][0]['address'],'[::ffff:192.0.2.1]')
    def test_base64_json_container(self):
        payload=base64.urlsafe_b64encode(json.dumps([profile('ws')],ensure_ascii=False).encode()).rstrip(b'=')
        _,c=self.one(payload);self.assertEqual(c['streamSettings']['network'],'ws')
    def test_aliases_array(self):
        payload=[profile('websocket'),profile('httpUpgrade'),profile('splithttp')]
        result,nodes=self.extract(payload);self.assertEqual(result['accepted'],3,result)
        self.assertEqual({n['network'] for n in nodes},{'ws','httpupgrade','xhttp'})
        for n in nodes: self.generate(n)
    def test_json_uri_equivalence_without_id_migration(self):
        p=profile('xhttp');n,c=self.one(p)
        uri=n['uri'];result,nodes=self.extract(uri.encode())
        self.assertEqual(result['rc'],0,result);self.assertEqual(len(nodes),1)
        # Repeat staging of the same endpoint must retain all model values.
        fields=['id','name','uri','protocol','address','port','uuid','encryption','flow','network','security','tls','reality','transport','xhttp']
        # Staging legitimately refreshes source observation timestamps.
        self.assertEqual({k:nodes[0][k] for k in fields},{k:n[k] for k in fields})
        self.assertEqual(nodes[0]['source']['subscriptionId'],n['source']['subscriptionId'])
        self.assertEqual(self.generate(nodes[0]),c)
    def test_foreign_files_untouched_by_failed_preparation(self):
        manual=self.app/'servers/manual-qa.json';other=self.app/'servers/other-subscription-qa.json'
        manual.write_bytes(b'{"qa":"manual"}');other.write_bytes(b'{"qa":"other"}')
        before={p.name:p.read_bytes() for p in (self.app/'servers').iterdir()}
        result,nodes=self.extract({'servers':[]});self.assertNotEqual(result['rc'],0)
        self.assertEqual(nodes,[]);self.assertEqual(before,{p.name:p.read_bytes() for p in (self.app/'servers').iterdir()})
    def test_partial_outbound_is_not_staged(self):
        p=profile();bad=copy.deepcopy(user(p));bad['encryption']='PRIVATE_CANARY'
        outbound(p)['settings']['vnext'][0]['users'].append(bad)
        result,nodes=self.extract(p);self.assertEqual(result['errorCode'],'NO_VALID_NODES')
        self.assertEqual(nodes,[]);self.assertEqual(result['accepted'],0)
    def test_existing_balancer_extraction_contract_is_not_execution(self):
        p=profile('ws');p['routing']={'balancers':[{'tag':'b','selector':['proxy']}]}
        p['dns']={'servers':['https://dns.example.invalid/dns-query']};p['inbounds']=[{'port':9999}]
        _,c=self.one(p);self.assertEqual(c['streamSettings']['network'],'ws')
        # Main generator guard above verifies top-level routing/DNS not copied.
    def test_generator_without_xhttp_host_is_unchanged(self):
        p=profile('xhttp');stream(p)['xhttpSettings'].pop('host')
        _,c=self.one(p);self.assertNotIn('host',c['streamSettings']['xhttpSettings'])

if __name__=='__main__':
    if os.name=='nt': raise SystemExit('Run in disposable Linux guest only')
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Pipeline))
    report={'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,
        'coreValidationRequested':bool(XRAY),'coreValidations':len(CORE_RESULTS),'coreResults':CORE_RESULTS,
        'routerAccessed':False,'providerAccessed':False,'networkRequested':False,
        'scope':'Actual extractor/stager/parser/generator, no HTTP transport or protected catalog commit'}
    print('STAGE05_PIPELINE_REPORT='+json.dumps(report),flush=True)
    raise SystemExit(0 if result.wasSuccessful() else 1)
