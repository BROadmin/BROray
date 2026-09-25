"""Hysteria2 URI authority/path regression and subscription import pipeline."""
import base64,ctypes,json,os,subprocess,unittest
from test_subscription_xray_json import SubscriptionXrayJson
from test_subscription_jobs import ROOT,SubscriptionJobs
from test_subscription_vless_pipeline import Pipeline, CORE_RESULTS
from urllib.parse import quote, unquote

class Hysteria2Pipeline(unittest.TestCase):
    setUp=Pipeline.setUp
    tearDown=Pipeline.tearDown
    shell=Pipeline.shell
    extract=Pipeline.extract
    generate=Pipeline.generate
    one=Pipeline.one
    reject_uri=Pipeline.reject_uri
    def uri(self,query='',auth='password',host='vpn.example.invalid:443'):
        return f'hysteria2://{auth}@{host}/?{query}'.encode()
    def test_auth_components(self):
        for value in ['a+b','a%2Bb','a%20b','a%40b','a%3Ab','a%2Fb','a%3Fb','a%23b','a%252Bb']:
            with self.subTest(value=value):
                n,c=self.one(self.uri(auth=value))
                self.assertEqual(n['auth'],unquote(value))
                self.assertEqual(c['streamSettings']['hysteriaSettings']['auth'],unquote(value))
        self.reject_uri(self.uri(auth='a%GG'))
    def test_default_and_explicit_ports(self):
        for host in ['vpn.example.invalid:443','vpn.example.invalid','[2001:db8::5]:443','[2001:db8::5]']:
            for scheme in ['hysteria2','hy2']:
                with self.subTest(host=host,scheme=scheme):
                    n,c=self.one(self.uri(host=host).replace(b'hysteria2:',scheme.encode()+b':'))
                    self.assertEqual(n['port'],443)
                    self.assertEqual(c['settings']['port'],443)
        for port in ['0','65536','abc','443x']:
            self.reject_uri(self.uri(host='vpn.example.invalid:'+port))
    def test_pin_and_insecure_contract(self):
        pin='ab'*32
        for insecure in ['', '&insecure=1','&insecure=true']:
            n,c=self.one(self.uri('pinSHA256='+':'.join(['AB']*32)+insecure))
            self.assertEqual(n['tls']['pinnedPeerCertSha256'],pin)
            self.assertFalse(n['tls']['allowInsecure'])
            self.assertEqual(c['streamSettings']['tlsSettings']['pinnedPeerCertSha256'],pin)
            self.assertNotIn('allowInsecure',c['streamSettings']['tlsSettings'])
        for query in ['insecure=1','insecure=true','pinSHA256=abc','pinSHA256='+'g'*64]:
            self.reject_uri(self.uri(query))
        for value in ['', '0','false']:
            n,c=self.one(self.uri('insecure='+value))
            self.assertFalse(n['tls']['allowInsecure'])
            self.assertNotIn('pinnedPeerCertSha256',c['streamSettings']['tlsSettings'])
    def test_hysteria_model_rejects_unsafe_tls(self):
        n,c=self.one(self.uri())
        for field,value in [('allowInsecure',True),('pinnedPeerCertSha256',''),('pinnedPeerCertSha256','bad'),('pinnedPeerCertSha256',42),('pinnedPeerCertSha256',None)]:
            with self.subTest(field=field,value=value):
                modified=json.loads(json.dumps(n));modified['tls'][field]=value
                (self.app/'model.json').write_text(json.dumps(modified))
                r=self.shell('. "$BRORAY_ROOT/lib/server.sh"; broray_server_validate "$BRORAY_ROOT/model.json"')
                self.assertNotEqual(r.returncode,0)
    def test_obfs_contract(self):
        n,c=self.one(self.uri('obfs=salamander&obfs-password=mask%2Bpassword'))
        self.assertEqual(n['hysteria']['obfsPassword'],'mask+password')
        self.assertEqual(c['streamSettings']['finalmask']['udp'][0]['settings']['password'],'mask+password')
        # Gecko is now an authorized native Xray mask; unknown obfuscators still fail.
        n,c=self.one(self.uri('obfs=gecko&obfs-password=password'))
        self.assertEqual(c['streamSettings']['finalmask']['udp'][0],{'type':'salamander','settings':{'password':'password','packetSize':'512-1200'}})
        for query in ['obfs=salamander','obfs-password=password','obfs=other','obfs=unknown&obfs-password=password']:
            self.reject_uri(self.uri(query))
    def test_finalmask_contract(self):
        fm={'udp':[{'type':'salamander','settings':{'password':'mask'}}]}
        n,c=self.one(self.uri('fm='+quote(json.dumps(fm))))
        self.assertEqual(n['hysteria']['finalMask'],fm)
        self.assertEqual(c['streamSettings']['finalmask'],fm)
        for value in ['', '{}']:
            n,c=self.one(self.uri('fm='+quote(value)))
            self.assertEqual(n['hysteria']['finalMask'],{})
        for value in ['{broken','[]','42','null','"text"']:
            self.reject_uri(self.uri('fm='+quote(value)))
    def test_unsupported_bandwidth_rejected(self):
        for query in ['upmbps=100','downmbps=100','upmbps=abc','downmbps=0']:
            self.reject_uri(self.uri(query))
        n,c=self.one(self.uri('upmbps=&downmbps='))
        self.assertEqual(n['hysteria']['upMbps'],'')
        self.assertEqual(n['hysteria']['downMbps'],'')

class Hysteria2Uri(SubscriptionXrayJson):
    def parse(self,uri):
        return subprocess.run(['/bin/ash','-c',
            '. "$BRORAY_ROOT/lib/parser-hysteria2.sh"; broray_parse_hysteria2 "$1"; '
            'jq -nc --arg address "$BRORAY_ADDRESS" --arg port "$BRORAY_PORT" '
            '--arg auth "$BRORAY_AUTH" --arg sni "$BRORAY_SNI" --arg name "$BRORAY_NAME" '
            '--arg network "$BRORAY_NETWORK" --arg security "$BRORAY_SECURITY" '
            '\'{address:$address,port:$port,auth:$auth,sni:$sni,name:$name,network:$network,security:$security}\'',
            'test',uri],env=self.env,capture_output=True,timeout=15)
    def test_optional_root_path_equivalent_to_no_path(self):
        for scheme in ['hysteria2','hy2']:
            for host in ['vpn.example.invalid','192.0.2.5','[2001:db8::5]']:
                for suffix in ['', '?sni=tls.example.invalid', '#Fixture', '?sni=tls.example.invalid#Fixture']:
                    with self.subTest(scheme=scheme,host=host,suffix=suffix):
                        plain=self.parse(f'{scheme}://fixture@{host}:443'+suffix)
                        slash=self.parse(f'{scheme}://fixture@{host}:443/'+suffix)
                        self.assertEqual(plain.returncode,0,plain.stderr)
                        self.assertEqual(slash.returncode,0,slash.stderr)
                        self.assertEqual(json.loads(plain.stdout),json.loads(slash.stdout))
                        self.assertEqual(json.loads(slash.stdout)['port'],'443')
    def test_encoded_delimiters_preserved_after_component_split(self):
        p=self.parse('hy2://user%3Apass%2Fword%40host%3F%23@vpn.example.invalid:443/?sni=tls.example.invalid#Name%2F%3F%23')
        self.assertEqual(p.returncode,0,p.stderr);d=json.loads(p.stdout)
        self.assertEqual(d['auth'],'user:pass/word@host?#')
        self.assertEqual(d['name'],'Name/?#');self.assertEqual(d['sni'],'tls.example.invalid')
    def test_invalid_ports_and_nonroot_paths_are_rejected(self):
        for suffix in [':0/',':65536/',':abc/',':443x/',':/',':443/path',':443//']:
            with self.subTest(suffix=suffix):
                p=self.parse('hysteria2://fixture@vpn.example.invalid'+suffix+'?sni=tls.example.invalid')
                self.assertNotEqual(p.returncode,0)
        for uri in ['hy2://@vpn.example.invalid:443/','hy2://vpn.example.invalid:443/','hy2://fixture@:443/']:
            self.assertNotEqual(self.parse(uri).returncode,0)
    def test_plain_and_base64_mixed_feed_accepts_hysteria2_and_filters_duplicate(self):
        lines=[f'vless://11111111-2222-4333-8444-555555555555@node{i}.example.invalid:443?security=tls&type=tcp#Node{i}' for i in range(8)]
        lines+=['hysteria2://fixture-one@hy-one.example.invalid:443/?sni=tls.example.invalid#HY1',
                'hy2://fixture-two@[2001:db8::5]:443/?sni=tls.example.invalid#HY2',lines[0]]
        raw=('\n'.join(lines)+'\n').encode()
        for payload in [raw,base64.b64encode(raw)]:
            result,servers,warnings=self.extract(payload)
            self.assertEqual(result['rc'],0,result)
            self.assertEqual((result['received'],result['accepted'],result['rejected']),(11,10,1))
            hy=[s for s in servers if s['protocol']=='hysteria2'];self.assertEqual(len(hy),2)
            for s in hy:
                self.assertEqual(s['port'],443);self.assertEqual(s['network'],'hysteria')
                self.assertEqual(s['security'],'tls');self.assertEqual(s['source']['subscriptionId'],'fixture')
            self.assertNotIn('порт Hysteria2',warnings)

class Hysteria2SubscriptionUpdate(unittest.TestCase):
    setUp=SubscriptionJobs.setUp
    clean_fixture=SubscriptionJobs.clean_fixture
    record=SubscriptionJobs.record
    states=SubscriptionJobs.states
    shell=SubscriptionJobs.shell
    job_script=SubscriptionJobs.job_script
    def test_base64_update_keeps_url_schedule_and_stable_server_ids(self):
        record=self.record(updateIntervalMinutes=360);before=json.loads(record.read_text())
        one='hysteria2://fixture-one@one.example.invalid:443/?sni=tls.example.invalid#One'
        two='hy2://fixture-two@two.example.invalid:443/?sni=tls.example.invalid#Two'
        self.payload.write_bytes(base64.b64encode((one+'\n'+two+'\n'+one+'\n').encode()))
        ids=None
        for _ in range(2):
            self.shell(self.job_script('broray_subscription_update test manual'),timeout=180)
            SubscriptionJobs.assert_generated_configs(self)
            data=json.loads(record.read_text());result=data['lastUpdateResult']
            self.assertEqual(data['url'],before['url']);self.assertTrue(data['autoUpdateEnabled'])
            self.assertEqual(data['updateIntervalMinutes'],360)
            self.assertEqual((result['received'],result['parsed'],result['accepted'],result['rejected']),(3,3,2,1))
            servers=[json.loads(f.read_text()) for f in (self.app/'servers').glob('*.json')]
            self.assertEqual(len(servers),2);new_ids={s['id'] for s in servers}
            if ids is not None:self.assertEqual(new_ids,ids)
            ids=new_ids
            self.assertTrue(all(s['source']['subscriptionId']=='test' for s in servers))
        self.assertTrue(all(s['state']=='completed' for s in self.states()))

if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    suite=unittest.TestSuite(Hysteria2Uri(n) for n in sorted(Hysteria2Uri.__dict__) if n.startswith('test_'))
    suite.addTests(unittest.defaultTestLoader.loadTestsFromTestCase(Hysteria2Pipeline))
    if os.environ.get('HY2_BASELINE')!='1':suite.addTests(unittest.defaultTestLoader.loadTestsFromTestCase(Hysteria2SubscriptionUpdate))
    result=unittest.TextTestRunner(verbosity=2).run(suite)
    (ROOT/'docs/evidence/hysteria2-uri-tests.json').write_text(json.dumps(dict(status='PASS' if result.wasSuccessful() else 'FAIL',testsRun=result.testsRun,
        routerAccessed=False,webuiTested=False,environment='Production parser/import/update with isolated files; HTTP transport fixture'),indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
