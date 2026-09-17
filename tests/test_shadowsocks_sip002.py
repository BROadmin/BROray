"""Offline real shell parser tests; synthetic secrets only, no networking."""
import base64, hashlib, json, os, shutil, subprocess, tempfile, unittest
from pathlib import Path
from urllib.parse import quote
ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
KEY16=base64.b64encode(bytes(range(16))).decode()
KEY32=base64.b64encode(bytes([251,255])*16).decode()
def uri(password='test-password',method='aes-128-gcm',host='vpn.example.invalid',port='443',kind='plain',suffix='',name='Тест + # 東京'):
    credentials=method+':'+password
    if kind=='plain': authority=quote(method,safe='')+':'+quote(password,safe='')+'@'+host+':'+port
    elif kind in ('b64','b64url'):
        encode=base64.b64encode if kind=='b64' else base64.urlsafe_b64encode
        authority=encode(credentials.encode()).decode().rstrip('=')+'@'+host+':'+port
    elif kind=='legacy': authority=base64.b64encode((credentials+'@'+host+':'+port).encode()).decode()
    else: raise ValueError(kind)
    return 'ss://'+authority+suffix+'#'+quote(name,safe='')
class Parser(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(prefix='broray-ss-test-');self.addCleanup(self.temp.cleanup)
        self.app=Path(self.temp.name)/'app';(self.app/'lib').mkdir(parents=True);(self.app/'tmp').mkdir()
        for name in ['util.sh','parser-shadowsocks.sh']: shutil.copyfile(ROOT/'runtime/app/lib'/name,self.app/'lib'/name)
    def parse(self,value,ok=True):
        (self.app/'input').write_bytes(value if isinstance(value,bytes) else value.encode())
        script='''. "$BRORAY_ROOT/lib/parser-shadowsocks.sh"
uri="$(cat "$BRORAY_ROOT/input"; printf x)"; uri="${uri%x}"
broray_parse_shadowsocks "$uri"
jq -nc --arg method "$BRORAY_METHOD" --arg password "$BRORAY_PASSWORD" --arg address "$BRORAY_ADDRESS" --arg port "$BRORAY_PORT" --arg name "$BRORAY_NAME" '{method:$method,password:$password,address:$address,port:$port,name:$name}'
'''
        p=subprocess.run([os.environ.get('BRORAY_TEST_SHELL','/bin/ash'),'-c',script],env={**os.environ,'BRORAY_ROOT':str(self.app),'BRORAY_BASE':str(self.app),'BRORAY_TMP':str(self.app/'tmp')},capture_output=True,timeout=15)
        if ok:
            self.assertEqual(p.returncode,0,p.stderr.decode(errors='replace'));result=json.loads(p.stdout)
        else:
            self.assertNotEqual(p.returncode,0,p.stdout);self.assertEqual(p.stdout,b'');result=None
            self.assertNotIn(b'PRIVATE_CANARY',p.stderr)
        if not os.environ.get('STAGE06_REPRO'): self.assertEqual(list((self.app/'tmp').iterdir()),[])
        return result
    def test_regression_plain(self): self.assertEqual(self.parse(uri())['password'],'test-password')
    def test_regression_aead2022(self): self.assertEqual(self.parse(uri(KEY32,'2022-blake3-aes-256-gcm'))['password'],KEY32)
    def test_regression_slash(self): self.assertEqual(self.parse(uri(kind='b64url',suffix='/'))['port'],'443')
    def test_regression_literal_plus(self): self.assertEqual(self.parse(uri(kind='b64url',name='').split('#')[0]+'#A+B')['name'],'A+B')
    def test_regression_literal_backslash(self): self.assertEqual(self.parse(uri(kind='b64url',name='').split('#')[0]+r'#A\x42')['name'],r'A\x42')
    def test_regression_encoded_plugin(self): self.parse(uri(kind='b64url',suffix='?%70lugin=PRIVATE_CANARY'),ok=False)
    def test_regression_bad_2022_key(self): self.parse(uri('PRIVATE_CANARY','2022-blake3-aes-128-gcm',kind='b64url'),ok=False)
    def test_regression_decoded_nul(self): self.parse(uri('a\x00PRIVATE_CANARY',kind='b64url'),ok=False)
    def test_regression_ipv6_invalid(self): self.parse(uri(kind='b64url',host='[2001:::1]'),ok=False)
    def test_standard_base64_padded(self):
        raw=base64.b64encode(b'aes-128-gcm:x').decode();self.assertEqual(self.parse('ss://'+raw+'@vpn.example.invalid:443')['password'],'x')
    def test_no_double_decode(self):
        for kind in ['plain','b64','b64url','legacy']:
            self.assertEqual(self.parse(uri('%40+\\x41: @ /?#',kind=kind))['password'],'%40+\\x41: @ /?#')
    def test_legacy_password_at_signs(self): self.assertEqual(self.parse(uri('a@b@c',kind='legacy'))['password'],'a@b@c')
    def test_percent_encoded_method(self): self.assertEqual(self.parse('ss://%61es-128-gcm:p@localhost:443')['method'],'aes-128-gcm')
    def test_unknown_query_ignored(self): self.assertEqual(self.parse(uri(suffix='/?remark=x&unused=true'))['password'],'test-password')
    def test_empty_plugin_compatible(self): self.parse(uri(suffix='/?plugin='))
    def test_duplicate_nonempty_plugin_rejected(self): self.parse(uri(suffix='/?plugin=&plugin=PRIVATE_CANARY'),ok=False)
    def test_default_name(self): self.assertEqual(self.parse(uri(name=''))['name'],'vpn.example.invalid:443')
    def test_decimal_leading_zero_port(self): self.assertEqual(self.parse(uri(port='00443'))['port'],'443')
    def test_parent_trap_and_unrelated_old_temp_preserved(self):
        script='. "$BRORAY_ROOT/lib/parser-shadowsocks.sh"\n' + "trap 'printf preserved > \"$BRORAY_ROOT/parent-trap\"' EXIT\n" + 'old="$BRORAY_TMP/shadowsocks-decoded.$$.txt"; printf preserve > "$old"\n' + 'broray_parse_shadowsocks "$1"; [ "$(cat "$old")" = preserve ]'
        p=subprocess.run(['/bin/ash','-c',script,'fixture',uri()],env={**os.environ,'BRORAY_ROOT':str(self.app),'BRORAY_BASE':str(self.app),'BRORAY_TMP':str(self.app/'tmp')},capture_output=True,timeout=15)
        self.assertEqual(p.returncode,0,p.stderr);self.assertEqual((self.app/'parent-trap').read_text(),'preserved')
        self.assertEqual(len(list((self.app/'tmp').iterdir())),1)
    def test_key_padding_normalized_without_changing_bytes(self):
        self.assertEqual(self.parse(uri(KEY16.rstrip('='),'2022-blake3-aes-128-gcm'))['password'],KEY16)
    def test_urlsafe_key_normalized_without_changing_bytes(self):
        value=KEY32.replace('+','-').replace('/','_').rstrip('=')
        self.assertEqual(self.parse(uri(value,'2022-blake3-aes-256-gcm'))['password'],KEY32)
    def test_invalid_name_percent_encoding(self): self.parse(uri(kind='b64url',name='').split('#')[0]+'#%GG',ok=False)
    def test_input_length_limit(self): self.parse(uri('p'*16384),ok=False)
    def test_decoded_text_length_limit(self): self.parse(uri('p'*8200),ok=False)
    def test_raw_control_not_removed_by_awk(self): self.parse(uri(kind='b64url')+'\nPRIVATE_CANARY',ok=False)
def positive_test(password,method='aes-128-gcm',kind='plain',host='vpn.example.invalid'):
    def test(self):
        result=self.parse(uri(password,method,kind=kind,host=host));self.assertEqual(result['password'],password);self.assertEqual(result['address'],host.strip('[]'));self.assertEqual(result['name'],'Тест + # 東京')
    return test
for kind in ['plain','b64','b64url','legacy']:
    for i,password in enumerate(['abc',':@/?#+%= &','p\\q\\x41','пароль 東京 🔐','%25%40%2B','$(touch /tmp/PRIVATE_CANARY);`false`']):
        setattr(Parser,f'test_roundtrip_{kind}_{i}',positive_test(password,kind=kind))
for method,key in [('2022-blake3-aes-128-gcm',KEY16),('2022-blake3-aes-256-gcm',KEY32),('2022-blake3-chacha20-poly1305',KEY32)]:
    for kind in ['plain','b64url','legacy']: setattr(Parser,'test_'+method+'_'+kind,positive_test(key,method,kind))
for i,method in enumerate(['aes-128-gcm','aes-256-gcm','chacha20-poly1305','chacha20-ietf-poly1305','xchacha20-poly1305']): setattr(Parser,f'test_method_{i}',positive_test('password',method))
for i,host in enumerate(['localhost','192.0.2.1','vpn.example.invalid.','[::1]','[2001:db8::1]','[2001:db8:1:2:3:4:5:6]','[::ffff:192.0.2.1]']): setattr(Parser,f'test_host_{i}',positive_test('password',host=host))
for i,method in enumerate(['2022-blake3-aes-128-gcm','2022-blake3-aes-256-gcm']):
    key=KEY16 if i==0 else KEY32;setattr(Parser,f'test_identity_chain_{i}',positive_test(key+':'+key,method))
def negative_test(value):
    def test(self): self.parse(value,ok=False)
    return test
bad=['ss://','https://example.invalid','ss://aes-128-gcm@a:443','ss://aes-128-gcm:@a:443','ss://aes-128-gcm:PRIVATE_CANARY@@a:443','ss://aes-128-gcm:p@a:443/path','ss://aes-128-gcm:p@a:443//']
for port in ['0','65536','-1','abc','443:1','999999999999999999999','']: bad.append(uri(port=port))
for host in ['','[::gg]','[:::]','[1:2:3]','[::ffff:999.0.0.1]','[::1%25eth0]','2001:db8::1','a/b','a b','a\\b','[::1','::1]']: bad.append(uri(host=host))
for value in ['bad%','bad%2','bad%GG','bad%00PRIVATE_CANARY','bad%0APRIVATE_CANARY','bad%7FPRIVATE_CANARY','bad%FF','bad%C0%AF']: bad.append('ss://aes-128-gcm:'+value+'@a:443')
for value in ['a','!!!!','YWVz!','YQ===','YQ=','YQ==junk','Y=Q=','YR==']: bad.append('ss://'+value+'@a:443')
for value in [b'aes-128-gcm:p\x00PRIVATE_CANARY',b'aes-128-gcm:p\nPRIVATE_CANARY',b'aes-128-gcm:\xff']:
    bad.append('ss://'+base64.b64encode(value).decode()+'@a:443')
for key in ['x','PRIVATE_CANARY',KEY32,KEY16+':',':'+KEY16,KEY16+'::'+KEY16]: bad.append(uri(key,'2022-blake3-aes-128-gcm'))
bad += [uri('p','PRIVATE_CANARY'),uri(KEY32+':'+KEY32,'2022-blake3-chacha20-poly1305'),uri(suffix='/?plugin'),uri(suffix='/?plugin=PRIVATE_CANARY'),uri(suffix='/?%70lugin=PRIVATE_CANARY')]
for i,value in enumerate(bad): setattr(Parser,f'test_reject_{i:03}',negative_test(value))
if __name__=='__main__':
    suite=unittest.defaultTestLoader.loadTestsFromTestCase(Parser)
    if os.environ.get('STAGE06_REPRO'): suite=unittest.TestSuite(t for t in suite if 'test_regression_' in t.id())
    result=unittest.TextTestRunner(verbosity=2,failfast=not bool(os.environ.get('STAGE06_REPRO'))).run(suite)
    print('STAGE06_PARSER_REPORT='+json.dumps({'testsRun':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),'skipped':len(result.skipped),'routerAccessed':False}));raise SystemExit(not result.wasSuccessful())
