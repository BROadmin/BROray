"""Init-ordering research only; no migration/candidate acceptance inferred.

The saved exact rc.unslung and actual S22 bytes are hashed. Only /opt paths are
rendered into a private fixture prefix. S21 is a proposed-behavior fixture, not
production code. These cases expose why PID occupancy is not a lasting barrier.
"""
from pathlib import Path
import hashlib,json,os,shutil,subprocess,tempfile,time,unittest
from test_updater_generation import GEN

ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
# Historical research inputs must stay frozen when the current platform evolves.
PLATFORM=Path(__file__).parent/'fixtures/legacy_init_ordering_cp77'

class InitOrdering(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='init-order-');self.home=Path(self.tmp.name);self.root=self.home/'router';self.opt=self.root/'opt'
  self.init=self.opt/'etc/init.d';self.init.mkdir(parents=True);self.bin=self.opt/'bin';self.bin.mkdir(parents=True)
  for name in ['ash','find']:
   target=shutil.which(name);self.assertIsNotNone(target,'required fixture tool: '+name);(self.bin/name).symlink_to(target)
  (self.bin/'curl').write_text('#!/bin/ash\nexit 98\n');(self.bin/'curl').chmod(0o755)
  self.assertEqual(hashlib.sha256((PLATFORM/'SHA256SUMS').read_bytes()).hexdigest(),'407436f1d5a0b9ec490e2bae260d3ec9b9e42096251b1783c8eee5d5b236c3ad')
  self.assertEqual(hashlib.sha256((PLATFORM/'opt/libexec/broray-updater/broray-updater.sh').read_bytes()).hexdigest(),'3c368f07392a89b0c089a398bc85a04b1b9f9ce6bdc49c1da3d3145f67c37650')
  rc=(Path(__file__).parent/'fixtures/legacy_rc.unslung').read_bytes();self.assertEqual(hashlib.sha256(rc).hexdigest(),'d5b830f14cc4120da89495200802d44869c9fb73eaf30a7a49699d7b551a93de')
  s22=(PLATFORM/'opt/etc/init.d/S22broray-updater').read_bytes();self.assertEqual(hashlib.sha256(s22).hexdigest(),'9eed286cfdb0eff76b8b8bf6e4a2d2882353f4c90055f1356ba602c646587e58')
  self.rc=self.home/'rc.unslung';self.rc.write_bytes(rc.replace(b'/opt',str(self.opt).encode()))
  (self.init/'S22broray-updater').write_bytes(s22.replace(b'/opt',str(self.opt).encode()));(self.init/'S22broray-updater').chmod(0o755)
  self.state=self.opt/'var/lib/broray-updater';self.state.mkdir(parents=True)
  legacy=self.opt/'libexec/broray-updater/broray-updater.sh';legacy.parent.mkdir(parents=True)
  legacy.write_text('''#!/bin/ash
printf '%s\\n' "$$" >"$TEST_STATE/daemon.pid"
printf '%s\\n' "$$" >"$TEST_HOME/legacy-started"
n=0
while [ ! -e "$TEST_HOME/release-legacy" ] && [ "$n" -lt 200 ]; do n=$((n+1)); sleep .05; done
''');legacy.chmod(0o755)
  stage=self.home/'staged';stage.mkdir();self.updater=stage/'broray-updater.sh';self.updater.write_bytes((PLATFORM/'opt/libexec/broray-updater/broray-updater.sh').read_bytes());self.updater.chmod(0o755)
  self.domain=self.home/'generations/boot-generation';self.domain.mkdir(parents=True,mode=0o700);self.domain.parent.chmod(0o700)
  self.sha=hashlib.sha256((PLATFORM/'SHA256SUMS').read_bytes()).hexdigest()
  self.env={**os.environ,'TEST_HOME':str(self.home),'TEST_STATE':str(self.state),'TEST_NATIVE':GEN,'TEST_DOMAIN':str(self.domain),'TEST_SHA':self.sha,'TEST_STAGED':str(self.updater),'BRORAY_UPDATER_ROOT_PREFIX':str(self.root),'BRORAY_UPDATER_PATH':str(self.bin)+':/usr/bin:/bin:/usr/sbin:/sbin','BRORAY_UPDATER_ASH':'/bin/ash'}
  print('BOOT_SOURCE_MAPPING '+json.dumps({'rcOriginalSha256':hashlib.sha256(rc).hexdigest(),'s22OriginalSha256':hashlib.sha256(s22).hexdigest(),'renderedRcSha256':hashlib.sha256(self.rc.read_bytes()).hexdigest(),'renderedS22Sha256':hashlib.sha256((self.init/'S22broray-updater').read_bytes()).hexdigest(),'onlyTransformation':'literal /opt -> private fixture /opt','scope':'INIT_ORDERING_RESEARCH_ONLY'}),flush=True)
 def control(self,verb):
  return subprocess.run([GEN,'control',str(self.domain),verb,'boot-generation',self.sha,'boot-research','research-nonce'],capture_output=True,text=True,timeout=4)
 def tearDown(self):
  (self.home/'release-legacy').touch()
  if (self.domain/'control').exists():
   self.control('STOP')
   for _ in range(40):
    r=self.control('STATUS')
    if r.returncode!=0:break
    if json.loads(r.stdout)['state']=='STOPPED':self.control('RETIRE');break
    time.sleep(.05)
  time.sleep(.1);self.tmp.cleanup()
 def boot(self,body):
  s21=self.init/'S21broray-migration';s21.write_text('#!/bin/ash\n'+body);s21.chmod(0o755)
  r=subprocess.run(['/bin/ash',str(self.rc),'start'],env=self.env,capture_output=True,text=True,timeout=25)
  print('BOOT_SEQUENCE_RESULT '+json.dumps({'stdout':r.stdout,'stderr':r.stderr,'exit':r.returncode,'legacyStarted':(self.home/'legacy-started').exists()}),flush=True)
  self.assertEqual(r.returncode,0,r.stderr)
 def supervised_prefix(self):
  return '''"$TEST_NATIVE" run "$TEST_DOMAIN" boot-generation "$TEST_SHA" -- /bin/ash "$TEST_STAGED" daemon >"$TEST_HOME/generation.log" 2>&1 </dev/null &
n=0
while [ ! -s "$TEST_STATE/daemon.ready" ] && [ "$n" -lt 200 ]; do n=$((n+1)); sleep .05; done
[ -s "$TEST_STATE/daemon.ready" ] || exit 91
"$TEST_NATIVE" control "$TEST_DOMAIN" STATUS boot-generation "$TEST_SHA" boot-research research-nonce >"$TEST_HOME/status-before-s22" || exit 92
'''
 def test_failed_s21_does_not_block_old_s22(self):
  self.boot('exit 75\n');self.assertTrue((self.home/'legacy-started').exists())
 def test_live_supervised_staged_daemon_prevents_one_legacy_start(self):
  self.boot(self.supervised_prefix()+'exit 0\n');self.assertFalse((self.home/'legacy-started').exists())
  r=self.control('STATUS');self.assertEqual(r.returncode,0,r.stderr);s=json.loads(r.stdout)
  self.assertEqual(s['state'],'RUNNING');self.assertTrue(s['supervisedFromBirth']);self.assertEqual(s['updater']['pid'],int((self.state/'daemon.pid').read_text()))
 def test_stopping_staged_daemon_reopens_old_s22_start_window(self):
  body=self.supervised_prefix()+'''"$TEST_NATIVE" control "$TEST_DOMAIN" STOP boot-generation "$TEST_SHA" boot-research research-nonce >/dev/null || exit 93
n=0
while [ "$n" -lt 100 ]; do
 n=$((n+1))
 "$TEST_NATIVE" control "$TEST_DOMAIN" STATUS boot-generation "$TEST_SHA" boot-research research-nonce >"$TEST_HOME/stopped-status" || exit 94
 jq -e '.state=="STOPPED"' "$TEST_HOME/stopped-status" >/dev/null && break
 sleep .05
done
jq -e '.state=="STOPPED"' "$TEST_HOME/stopped-status" >/dev/null || exit 95
exit 75
'''
  self.boot(body);self.assertTrue((self.home/'legacy-started').exists())
  s=json.loads((self.home/'stopped-status').read_text());self.assertEqual(s['state'],'STOPPED');self.assertNotEqual(int((self.home/'legacy-started').read_text()),s['updater']['pid'])

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
