"""Updater field regressions: real shell/jq with isolated network fixtures."""
import hashlib,json,os,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
APP=ROOT/'implementation/runtime/app'
class FieldRegressions(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.base=Path(self.tmp.name);self.app=self.base/'opt/broray'
        (self.app/'config').mkdir(parents=True);(self.app/'routes').mkdir()
        self.bin=self.base/'bin';self.bin.mkdir()
        self.ip=self.base/'addresses';self.ip.write_text('    inet 192.168.2.1/24 scope global br0\n')
        fake=self.bin/'ip';fake.write_text('#!/bin/ash\n[ "$*" = "-4 addr show" ] || exit 1\ncat "$TEST_ADDRESSES"\n');fake.chmod(0o755)
        self.env=dict(os.environ,BRORAY_UPDATER_ROOT_PREFIX=str(self.base),BRORAY_UPDATER_PATH=str(self.bin)+':/usr/bin:/bin',TEST_ADDRESSES=str(self.ip))
        self.owner=self.app/'config/interface.json';self.config=self.app/'routes/config.json'
        self.library=self.base/'updater.sh'
        source=(APP/'share/updater-platform/opt/libexec/broray-updater/broray-updater.sh').read_text()
        assert source.endswith('main "$@"\n');self.library.write_text(source.removesuffix('main "$@"\n'))
        self.set_endpoint('192.168.2.1')
    def set_endpoint(self,host,port=2080,schema=1):
        owner=dict(schemaVersion=schema,owner='BROray',interfaceName='Proxy0',protocol='socks5',upstream=dict(host=host,port=port),selectionMode='auto',updatedAt='2026-09-16T00:00:00Z')
        if schema==2:owner.update(contract='r14c38-proxy-owned-interface/1',description='BROray - Test',writeProtocolSha256='1'*64,runningBlockSha256='2'*64,startupBlockSha256='2'*64)
        self.owner.write_text(json.dumps(owner));self.config.write_text(json.dumps(dict(managedInterface='Proxy0',managedMetric=1200)))
        self.xray=self.app/'config/config.json';self.xray.write_text(json.dumps(dict(inbounds=[dict(protocol='socks',listen=host,port=port)],outbounds=[])))
    def owner_result(self):
        return subprocess.run(['/bin/ash','-c','. "$1"; routes_owner_interface "$2" "$3"','test',str(self.library),str(self.owner),str(self.config)],env=self.env,capture_output=True,timeout=15)
    def assert_owner(self,valid):
        p=self.owner_result();self.assertEqual(p.returncode==0,valid,(p.returncode,p.stdout,p.stderr))
        if valid:self.assertEqual(p.stdout,b'Proxy0\n')
    def test_owner_accepts_actual_lan_addresses(self):
        for schema in (1,2):
            for host in ('192.168.1.1','192.168.2.1','192.168.10.1','10.20.30.1','172.20.1.1'):
                with self.subTest(schema=schema,host=host):
                    self.set_endpoint(host,schema=schema);self.ip.write_text('    inet '+host+'/24 scope global br0\n');self.assert_owner(True)
    def test_owner_accepts_matching_configured_port(self):
        self.set_endpoint('192.168.2.1',2081);self.assert_owner(True)
    def test_owner_rejects_upstream_router_address(self):
        self.set_endpoint('192.168.1.1');self.assert_owner(False)
    def test_owner_rejects_listener_mismatch(self):
        self.set_endpoint('192.168.1.1');self.ip.write_text('    inet 192.168.1.1/24 scope global br0\n')
        self.xray.write_text('{"inbounds":[{"protocol":"socks","listen":"192.168.2.1","port":2080}]}');self.assert_owner(False)
    def test_owner_rejects_ambiguous_listener_and_local_address(self):
        self.ip.write_text('    inet 192.168.2.1/24\n    inet 192.168.2.1/24\n');self.assert_owner(False)
        self.ip.write_text('    inet 192.168.2.1/24\n');d=json.loads(self.xray.read_text());d['inbounds']*=2;self.xray.write_text(json.dumps(d));self.assert_owner(False)
    def test_owner_rejects_bad_endpoint_or_wrong_ownership(self):
        for host,port in [('router.local',2080),('192.168.2.999',2080),('192.168.2.1',0),('192.168.2.1',65536),('192.168.2.1','2080')]:
            with self.subTest(host=host,port=port):self.set_endpoint(host,port);self.assert_owner(False)
        self.set_endpoint('192.168.2.1');d=json.loads(self.owner.read_text());d['owner']='foreign';self.owner.write_text(json.dumps(d));self.assert_owner(False)
    def test_owner_rejects_missing_or_symlink_xray_config(self):
        self.xray.unlink();self.assert_owner(False);target=self.base/'foreign.json';target.write_text('{"inbounds":[]}');self.xray.symlink_to(target);self.assert_owner(False)
    def route_fixture(self,extra='',count=868):
        self.operation=self.base/'operation';self.operation.mkdir()
        self.running=self.base/'running';self.startup=self.base/'startup';self.commands=self.base/'commands'
        self.routes=[f'ip route 10.{n//256}.{n%256}.0 255.255.255.0 Proxy0 1200' for n in range(count)]
        self.foreign='ip route 203.0.113.1 Proxy1 1200\nip route 198.51.100.1 ISP 10\n'
        self.running.write_text('\n'.join(self.routes)+'\n'+self.foreign+extra);self.startup.write_bytes(self.running.read_bytes())
        self.env.update(TEST_RUNNING=str(self.running),TEST_STARTUP=str(self.startup),TEST_COMMANDS=str(self.commands))
        ndmc=self.bin/'ndmc';ndmc.write_text('''#!/bin/ash
[ "$1" = -c ] && [ "$#" = 2 ] || exit 2
case "$2" in
  'show running-config') cat "$TEST_RUNNING" ;;
  'more startup-config') cat "$TEST_STARTUP" ;;
  'system configuration save') printf '%s\\n' "$2" >>"$TEST_COMMANDS"; cp "$TEST_RUNNING" "$TEST_STARTUP" ;;
  'ip route '*) printf '%s\\n' "$2" >>"$TEST_COMMANDS"; printf '%s\\n' "$2" >>"$TEST_RUNNING" ;;
  *) exit 3 ;;
esac
''');ndmc.chmod(0o755)
        # The real updater consumes the authenticated staged target dispatcher,
        # never a PATH ndmc from the old application. Rebase only its endpoint.
        slot_id='3.2.0-route-fixture--update-test';slot=self.app/'releases'/slot_id
        (slot/'app/bin').mkdir(parents=True)
        helper=slot/'app/bin/broray-system-ndmc'
        helper.write_text((APP/'bin/broray-system-ndmc').read_text().replace('/bin/ndmc',str(ndmc)));helper.chmod(0o755)
        (slot/'SHA256SUMS').write_text(hashlib.sha256(helper.read_bytes()).hexdigest()+'  app/bin/broray-system-ndmc\n')
        (slot/'.broray-slot').write_text(slot_id+'\n')
        for name in ['target','target-slot']:(self.operation/name).write_text(slot_id+'\n')
    def routes_call(self,call):
        script='. "$1"; CURRENT_OPERATION_DIR="$2"; CURRENT_OPERATION_LOG="$2/log"; '+call
        return subprocess.run(['/bin/ash','-c',script,'test',str(self.library),str(self.operation)],env=self.env,capture_output=True,timeout=300)
    def test_capture_and_verify_868_routes_on_nondefault_lan(self):
        self.route_fixture();p=self.routes_call('routes_capture && routes_restore_captured && routes_verify_captured')
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertEqual(set((self.operation/'managed-routes.before').read_text().splitlines()),set(self.routes))
        self.assertFalse(self.commands.exists(),'Unchanged routes should not be rewritten')
        self.assertIn('count=868 changed=false',(self.operation/'log').read_text())
    def check_small_route_count(self,count):
        self.route_fixture(count=count);before=self.running.read_bytes()
        p=self.routes_call('routes_capture && routes_restore_captured && routes_verify_captured')
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertEqual(set((self.operation/'managed-routes.before').read_text().splitlines()),set(self.routes))
        self.assertEqual(self.running.read_bytes(),before);self.assertEqual(self.startup.read_bytes(),before)
        self.assertFalse(self.commands.exists())
    def test_capture_and_verify_1_route(self):self.check_small_route_count(1)
    def test_capture_and_verify_42_routes(self):self.check_small_route_count(42)
    def test_restore_only_missing_captured_route_preserves_foreign(self):
        self.route_fixture(count=3);p=self.routes_call('routes_capture');self.assertEqual(p.returncode,0,p.stderr)
        self.running.write_text('\n'.join(self.routes[1:])+'\n'+self.foreign)
        p=self.routes_call('routes_restore_captured && routes_verify_captured');self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual(self.commands.read_text().splitlines(),[self.routes[0],'system configuration save'])
        self.assertIn(self.foreign,self.running.read_text());self.assertEqual(self.running.read_bytes(),self.startup.read_bytes())
    def test_capture_refuses_ambiguous_managed_route_without_writes(self):
        self.route_fixture('ip route 192.0.2.1 Proxy0 50\n',count=3);before=self.running.read_bytes()
        p=self.routes_call('routes_capture');self.assertNotEqual(p.returncode,0)
        self.assertEqual(self.running.read_bytes(),before);self.assertFalse(self.commands.exists())
        self.assertFalse((self.operation/'managed-routes.before').exists())
if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(FieldRegressions))
    out=ROOT/'docs/evidence/field-regressions-tests.json';out.write_text(json.dumps(dict(status='PASS' if result.wasSuccessful() else 'FAIL',testsRun=result.testsRun,routerAccessed=False,environment='real ash/jq, isolated files and network fixtures'),indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
