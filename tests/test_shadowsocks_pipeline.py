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
from test_shadowsocks_sip002 import uri, KEY16, KEY32

ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
XRAY=os.environ.get('BRORAY_TEST_XRAY','')
CORE_RESULTS=[]

class Pipeline(unittest.TestCase):
    def setUp(self):
        self.temp=Path(tempfile.mkdtemp(prefix='broray-ss-stage06-'))
        self.app=self.temp/'app'
        shutil.copytree(ROOT/'runtime/app/lib',self.app/'lib')
        for d in ['tmp','servers','config/system','logs']: (self.app/d).mkdir(parents=True,exist_ok=True)
        settings={'listenAddress':'127.0.0.1','socksPort':2080,'logLevel':'warning'}
        (self.app/'config/system/settings.json').write_text(json.dumps(settings),encoding='utf-8')
        self.env={**os.environ,'BRORAY_BASE':str(self.app),'BRORAY_ROOT':str(self.app),
            'BRORAY_SUB_BASE':str(self.app),'PATH':'/usr/bin:/bin:/usr/sbin:/sbin'}
    def tearDown(self):
        assert self.temp.parent.resolve()==Path(tempfile.gettempdir()).resolve()
        assert self.temp.name.startswith('broray-ss-stage06-')
        shutil.rmtree(self.temp)
    def shell(self,script,*args):
        return subprocess.run(['/bin/ash','-c',script,'stage06',*map(str,args)],env=self.env,
                              capture_output=True,timeout=45)
    def extract(self,payload):
        raw=payload if isinstance(payload,bytes) else payload.encode()
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
        self.assertEqual(config['outbounds'][0]['protocol'],'shadowsocks')
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
    def check_endpoint(self,password='test-password',method='aes-128-gcm',kind='plain',host='vpn.example.invalid',suffix=''):
        text=uri(password,method,kind=kind,host=host,suffix=suffix)
        node,config=self.one(text);endpoint=config['settings']['servers'][0]
        self.assertEqual(endpoint,{'address':host.strip('[]'),'port':443,'method':method,'password':password})
        self.assertEqual(node['uri'],text);self.assertEqual(node['name'],'Тест + # 東京')
        self.assertNotIn('streamSettings',config);return node
    def test_no_double_decode(self): self.check_endpoint('%40+\\x41:@/#東京',kind='b64url')
    def test_ipv6(self): self.check_endpoint(host='[2001:db8::1]')
    def test_trailing_slash(self): self.check_endpoint(suffix='/')
    def test_base64_subscription(self):
        text=uri(KEY16,'2022-blake3-aes-128-gcm');node,c=self.one(base64.b64encode(text.encode()))
        self.assertEqual(c['settings']['servers'][0]['password'],KEY16)
    def test_base64url_subscription(self):
        text=uri('🔐');node,c=self.one(base64.urlsafe_b64encode(text.encode()).rstrip(b'='))
        self.assertEqual(c['settings']['servers'][0]['password'],'🔐')
    def test_mixed_list_partial_result(self):
        result,nodes=self.extract(uri()+'\n'+uri(suffix='/?plugin=PRIVATE_CANARY'))
        self.assertEqual((result['accepted'],result['rejected']),(1,1));self.assertEqual(len(nodes),1)
        self.generate(nodes[0]);warnings=''.join(p.read_text() for p in (self.app/'tmp').glob('subscription-warnings.*'))
        self.assertNotIn('PRIVATE_CANARY',warnings)
    def test_failed_preparation_preserves_live_files(self):
        for name in ['manual-qa','other-subscription-qa']: (self.app/'servers'/name).write_bytes(b'unchanged')
        before={p.name:p.read_bytes() for p in (self.app/'servers').iterdir()}
        result,nodes=self.extract(uri('PRIVATE_CANARY','2022-blake3-aes-128-gcm'))
        self.assertNotEqual(result['rc'],0);self.assertEqual(nodes,[])
        self.assertEqual(before,{p.name:p.read_bytes() for p in (self.app/'servers').iterdir()})
    def test_repeat_id_and_parameters(self):
        text=uri();n,c=self.one(text);n2,c2=self.one(text)
        self.assertEqual(c,c2)
        for key in ['id','name','uri','protocol','address','port','method','password']: self.assertEqual(n[key],n2[key])
def endpoint_test(password,method='aes-128-gcm',kind='plain'):
    def test(self): self.check_endpoint(password,method,kind)
    return test
for kind in ['plain','b64','b64url','legacy']: setattr(Pipeline,'test_envelope_'+kind,endpoint_test('test-password',kind=kind))
for i,method in enumerate(['aes-128-gcm','aes-256-gcm','chacha20-poly1305','chacha20-ietf-poly1305','xchacha20-poly1305']): setattr(Pipeline,f'test_method_{i}',endpoint_test('test-password',method))
for i,(method,key) in enumerate([('2022-blake3-aes-128-gcm',KEY16),('2022-blake3-aes-256-gcm',KEY32),('2022-blake3-chacha20-poly1305',KEY32)]):
    setattr(Pipeline,f'test_aead2022_{i}',endpoint_test(key,method))
    if i<2: setattr(Pipeline,f'test_identity_chain_{i}',endpoint_test(key+':'+key,method))
if __name__=='__main__':
    suite=unittest.defaultTestLoader.loadTestsFromTestCase(Pipeline)
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(suite)
    print('STAGE06_PIPELINE_REPORT='+json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'coreValidationRequested':bool(XRAY),'coreValidations':len(CORE_RESULTS),'coreResults':CORE_RESULTS,'routerAccessed':False,'providerAccessed':False,'scope':'Actual extractor/stager/parser/generator; no HTTP or protected live catalog commit'}))
    raise SystemExit(not result.wasSuccessful())
