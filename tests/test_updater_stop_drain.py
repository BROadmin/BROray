"""Real Linux owner identities: cooperative observation never signals a process."""
import hashlib,json,os,shutil,subprocess,tempfile,time,unittest
from pathlib import Path
from test_updater_cooperative_stop import ROOT,SOURCE

class StopDrain(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.home=Path(self.tmp.name)
  self.app=self.home/'opt/broray';(self.app/'lib').mkdir(parents=True);(self.app/'run').mkdir()
  for name in ['service-lifecycle.sh','operation-owner.sh']:shutil.copyfile(ROOT/'runtime/app/lib'/name,self.app/'lib'/name)
  self.svc=self.home/'opt/var/lib/broray/services/home-snapshot';self.svc.mkdir(parents=True)
  worker=self.home/'worker.sh';worker.write_text('''touch "$DRAIN_HOME/ready"
while [ ! -e "$DRAIN_HOME/release" ]; do sleep 1; done
jq '.state="stopped"' "$DRAIN_HOME/opt/var/lib/broray/services/home-snapshot/identity.json" >"$DRAIN_HOME/next"
mv "$DRAIN_HOME/next" "$DRAIN_HOME/opt/var/lib/broray/services/home-snapshot/identity.json"
''')
  self.child=subprocess.Popen(['/bin/ash',str(worker)],env=os.environ|{'DRAIN_HOME':str(self.home)},stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  self.addCleanup(self.cleanup_child)
  end=time.monotonic()+5
  while not (self.home/'ready').exists() and time.monotonic()<end:time.sleep(.02)
  self.assertTrue((self.home/'ready').exists())
  p=Path('/proc')/str(self.child.pid)
  owner=dict(pid=self.child.pid,startTicks=(p/'stat').read_text().rsplit(') ',1)[1].split()[19],bootId=Path('/proc/sys/kernel/random/boot_id').read_text().strip(),executable=os.readlink(p/'exe'),commandDigest=hashlib.sha256((p/'cmdline').read_bytes()).hexdigest())
  self.record=dict(schemaVersion=1,service='home-snapshot',generation='a'*32,owner=owner,state='running')
  self.identity=self.svc/'identity.json';self.identity.write_text(json.dumps(self.record));self.identity.chmod(0o600)
  self.stop=self.svc/'stop.json';self.stop.write_text(json.dumps(dict(schemaVersion=1,generation='a'*32)));self.stop.chmod(0o600)
  (self.app/'run/home-snapshotd.pid').write_text(str(self.child.pid)+'\n');(self.app/'run/home-snapshotd.pid').chmod(0o600)
  text=SOURCE.read_text();a=text.index('\nservice_wait_cooperative_stop()\n');b=text.index('\nservice_stop_continue_intent()\n',a)
  self.code=text[a:b]
  self.env=os.environ|{'APP_ROOT':str(self.app),'DRAIN_HOME':str(self.home)}
  self.controllers=[]
 def cleanup_child(self):
  for p in self.controllers:
   if p.poll() is None:p.kill()
   p.wait(timeout=3)
  if self.child.poll() is None:self.child.kill()
  self.child.wait(timeout=3)
 def start(self):
  # Controlled clock only accelerates the timeout negative; positive uses an
  # explicit collector-release event after real owner verification.
  pre='''set -u
root_path(){ printf '%s%s\n' "$DRAIN_HOME" "$1"; }
epoch(){ if [ -e "$DRAIN_HOME/deadline" ]; then printf '200\n'; else printf '100\n'; fi; }
'''
  self.log=self.home/'observer.log';self.err=self.home/'observer.err'
  with self.log.open('wb') as out,self.err.open('wb') as err:
   p=subprocess.Popen(['/bin/ash','-c',pre+self.code+'\nservice_wait_cooperative_stop S24broray\n'],env=self.env,stdout=out,stderr=err)
  self.controllers.append(p);return p
 def begun(self,p):
  end=time.monotonic()+8
  while time.monotonic()<end:
   if 'COOPERATIVE_DRAIN_BEGIN' in self.log.read_text():return
   if p.poll() is not None:break
   time.sleep(.03)
  self.fail('observer never admitted exact live generation: '+self.err.read_text())
 def failed(self,p):
  self.assertNotEqual(p.wait(timeout=10),0,self.log.read_text());self.assertIsNone(self.child.poll(),'observer signalled unrelated/live process')
 def test_real_delayed_generation_drains(self):
  p=self.start();self.begun(p);(self.home/'release').touch();self.child.wait(timeout=8)
  self.assertEqual(p.wait(timeout=10),0,self.err.read_text());self.assertIn('COOPERATIVE_DRAIN_COMPLETE',self.log.read_text())
 def test_changed_generation_refused(self):
  p=self.start();self.begun(p);self.record['generation']='b'*32;self.identity.write_text(json.dumps(self.record));self.failed(p)
 def test_pid_reuse_identity_refused(self):
  p=self.start();self.begun(p);self.record['owner']['startTicks']='1';self.identity.write_text(json.dumps(self.record));self.failed(p)
 def test_corrupt_ledger_preserved(self):
  p=self.start();self.begun(p);self.identity.write_text('{broken');self.failed(p);self.assertEqual(self.identity.read_text(),'{broken')
 def test_missing_ledger_not_recreated(self):
  p=self.start();self.begun(p);self.identity.unlink();self.failed(p);self.assertFalse(self.identity.exists())
 def test_changed_stop_request_preserved(self):
  p=self.start();self.begun(p);self.stop.write_text('{broken');self.failed(p);self.assertEqual(self.stop.read_text(),'{broken')
 def test_missing_stop_request_not_recreated(self):
  p=self.start();self.begun(p);self.stop.unlink();self.failed(p);self.assertFalse(self.stop.exists())
 def test_timeout_never_means_stopped(self):
  p=self.start();self.begun(p);(self.home/'deadline').touch();self.failed(p);self.assertEqual(json.loads(self.identity.read_text())['state'],'running')
 def test_dead_but_unretired_owner_refused(self):
  self.child.kill();self.child.wait(timeout=3);p=self.start();self.assertNotEqual(p.wait(timeout=10),0)
  self.assertEqual(json.loads(self.identity.read_text())['state'],'running')
 def test_absent_stop_intent_never_authorizes_continuation(self):
  self.stop.unlink();p=self.start();self.failed(p)

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
