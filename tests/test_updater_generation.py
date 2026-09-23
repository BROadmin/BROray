"""Lifetime primitive: actual Linux processes, no router/network/host mounts.

Integration readiness and legacy migration are separate gates. These tests
never infer platform readiness from this primitive's RUNNING state.
"""
from pathlib import Path
import hashlib,json,os,signal,subprocess,tempfile,time,unittest

GEN='/work/.local/bin/linux-generation'
class Generation(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='gen-');self.home=Path(self.tmp.name)
  self.domain=self.home/'domain';self.domain.mkdir(mode=0o700)
  self.gid='generation-one';self.sha='a'*64;self.processes=[];self.logs=[]
 def tearDown(self):
  print('GENERATION_FIXTURE_EVIDENCE '+json.dumps({'test':self.id(),'state':(self.domain/'state.json').read_text() if (self.domain/'state.json').exists() else None,'logs':{p.name:p.read_text() for p in self.home.glob('*.log')}}),flush=True)
  for p in reversed(self.processes):
   if p.poll() is None:p.kill()
   try:p.wait(timeout=3)
   except subprocess.TimeoutExpired:pass
  for f in self.logs:f.close()
  self.tmp.cleanup()
 def wait(self,fn,seconds=5):
  end=time.monotonic()+seconds
  while time.monotonic()<end:
   try:
    r=fn()
    if r:return r
   except (FileNotFoundError,json.JSONDecodeError,ProcessLookupError):pass
   time.sleep(.02)
  self.fail('condition deadline; logs='+''.join(p.read_text() for p in self.home.glob('*.log')))
 def latest_record(self):
  # Fixture inspection only; readiness must use the authenticated live control.
  records=sorted(self.domain.glob('revision-*.json'))
  return records[-1] if records else self.domain/'state.json'
 def state(self):return json.loads(self.latest_record().read_text())
 def start(self,body='while :; do sleep 2; done\n',env=None):
  script=self.home/'daemon.sh';script.write_text('#!/bin/ash\n'+body)
  log=open(self.home/'native.log','wb');self.logs.append(log)
  p=subprocess.Popen([GEN,'run',str(self.domain),self.gid,self.sha,'--','/bin/ash',str(script)],env={**os.environ,'TEST_HOME':str(self.home),**(env or {})},stdout=log,stderr=log)
  self.processes.append(p);self.wait(lambda:(self.domain/'control').exists())
  return p
 def call(self,verb='STATUS',gid=None,nonce='nonce-one',op='operation-one',sha=None):
  return subprocess.run([GEN,'control',str(self.domain),verb,gid or self.gid,sha or self.sha,op,nonce],capture_output=True,text=True,timeout=4)
 def running(self):return self.wait(lambda:self.state() if self.state()['state']=='RUNNING' else None)
 def stopped(self):return self.wait(lambda:self.state() if self.state()['state']=='STOPPED' else None)
 def live(self,pid):
  try:return Path(f'/proc/{pid}/stat').read_text().rsplit(') ',1)[1].split()[0] not in ['Z','X']
  except FileNotFoundError:return False
 def test_sha256_vectors(self):
  for n in [0,1,55,56,63,64,65,127,128,4095,4096,1048576]:
   b=bytes(i%251 for i in range(n));p=subprocess.run([GEN,'--sha256'],input=b,capture_output=True,timeout=5)
   self.assertEqual(p.returncode,0);self.assertEqual(p.stdout.decode().strip(),hashlib.sha256(b).hexdigest())
 def test_supervised_birth_exact_identity(self):
  p=self.start();s=self.running();self.assertEqual(s['supervisor']['pid'],p.pid)
  for id in [s['supervisor'],s['updater']]:
   pid=id['pid'];self.assertEqual(id['commandDigest'],hashlib.sha256(Path(f'/proc/{pid}/cmdline').read_bytes()).hexdigest());self.assertEqual(id['executable'],os.readlink(f'/proc/{pid}/exe'))
   self.assertEqual(id['startTicks'],Path(f'/proc/{pid}/stat').read_text().rsplit(') ',1)[1].split()[19])
  self.assertIn(f'TracerPid:\t{p.pid}',Path(f"/proc/{s['updater']['pid']}/status").read_text())
  self.assertTrue(s['supervisedFromBirth']);self.assertEqual(s['platformManifestSha256'],self.sha)
 def writer_body(self,depth=2):
  writer=self.home/'writer.sh';writer.write_text('''#!/bin/ash
echo $$ >"$TEST_HOME/writer.pid.tmp"
mv "$TEST_HOME/writer.pid.tmp" "$TEST_HOME/writer.pid"
while [ ! -e "$TEST_HOME/release" ]; do sleep .02; done
echo CORRUPT >>"$TEST_HOME/platform"
''')
  command='/bin/ash "$TEST_HOME/writer.sh" </dev/null >/dev/null 2>&1 &\n'
  for i in range(depth):
   file=self.home/f'fork-{i}.sh';file.write_text('#!/bin/ash\n'+command)
   command=f'/bin/ash "$TEST_HOME/fork-{i}.sh"\n'
  (self.home/'platform').write_text('original\n')
  return command+'while :; do sleep 2; done\n'
 def writer_pid(self):return self.wait(lambda:int((self.home/'writer.pid').read_text()))
 def test_preexisting_double_fork_writer_stop(self):
  self.start(self.writer_body());pid=self.writer_pid();s=self.wait(lambda:self.state() if any(x['pid']==pid for x in self.state()['children']) else None)
  self.assertTrue(self.live(pid));r=self.call('STOP');self.assertEqual(r.returncode,0,r.stderr+r.stdout)
  end=self.stopped();self.assertEqual(end['children'],[]);self.assertFalse(self.live(pid))
  (self.home/'release').touch();time.sleep(.1);self.assertEqual((self.home/'platform').read_text(),'original\n')
  print('GENERATION_DETACHED_WRITER '+json.dumps({'beforeRevision':s['revision'],'writerPid':pid,'after':end,'writerCannotWrite':True}),flush=True)
 def test_child_grandchild_detach(self):
  self.start(self.writer_body(depth=4));pid=self.writer_pid();self.assertEqual(self.call('STOP').returncode,0);self.stopped();self.assertFalse(self.live(pid))
  (self.home/'release').touch();time.sleep(.1);self.assertEqual((self.home/'platform').read_text(),'original\n')
 def test_supervisor_sigkill_kills_detached_writer(self):
  p=self.start(self.writer_body());pid=self.writer_pid();self.wait(lambda:any(x['pid']==pid for x in self.state()['children']))
  p.kill();p.wait(timeout=3);self.wait(lambda:not self.live(pid));(self.home/'release').touch();time.sleep(.1)
  self.assertEqual((self.home/'platform').read_text(),'original\n');self.assertNotEqual(self.state()['state'],'STOPPED')
 def test_root_exit_drains_live_writer(self):
  self.start(self.writer_body().replace('while :; do sleep 2; done','sleep .3; exit 0'));pid=self.writer_pid();self.stopped();self.assertFalse(self.live(pid))
 def test_foreign_same_executable_and_xray_untouched(self):
  x=subprocess.Popen(['/bin/ash','-c','sleep 60']);self.processes.append(x)
  self.start();self.running();self.assertEqual(self.call('STOP').returncode,0);self.stopped();self.assertIsNone(x.poll())
 def test_duplicate_generation_preserves_exact_ledger(self):
  self.start('while :; do :; done\n');self.running();before=(self.domain/'state.json').read_bytes()
  p=subprocess.run([GEN,'run',str(self.domain),'generation-two',self.sha,'--','/bin/ash','-c','exit 99'],capture_output=True,timeout=5)
  self.assertNotEqual(p.returncode,0);self.assertIn(b'GENERATION_ALREADY_OWNED',p.stderr);self.assertEqual((self.domain/'state.json').read_bytes(),before)
 def test_ledger_corruption_fails_closed(self):
  p=self.start(self.writer_body());pid=self.writer_pid();(self.domain/'state.json').write_text('{broken')
  self.assertNotEqual(p.wait(timeout=3),0);self.wait(lambda:not self.live(pid));self.assertEqual((self.domain/'state.json').read_text(),'{broken')
 def test_ledger_missing_fails_closed(self):
  p=self.start(self.writer_body());pid=self.writer_pid();(self.domain/'state.json').unlink();self.assertNotEqual(p.wait(timeout=3),0);self.wait(lambda:not self.live(pid));self.assertFalse((self.domain/'state.json').exists())
 def test_ledger_revision_rollback_fails_closed(self):
  p=self.start(self.writer_body());early=self.latest_record().read_bytes();pid=self.writer_pid();self.wait(lambda:self.latest_record().read_bytes()!=early)
  current=self.latest_record();current.write_bytes(early);self.assertNotEqual(p.wait(timeout=3),0);self.wait(lambda:not self.live(pid));self.assertEqual(current.read_bytes(),early)
 def test_wrong_generation_manifest_nonce_no_mutation(self):
  self.start('while :; do :; done\n');self.running();before=(self.domain/'state.json').read_bytes()
  self.assertNotEqual(self.call('STOP',gid='other-generation').returncode,0);self.assertNotEqual(self.call('STOP',sha='b'*64).returncode,0);self.assertEqual((self.domain/'state.json').read_bytes(),before)
  self.assertEqual(self.call('STOP').returncode,0);self.stopped();stopped=(self.domain/'state.json').read_bytes()
  self.assertNotEqual(self.call('STOP',nonce='wrong-nonce').returncode,0);self.assertEqual((self.domain/'state.json').read_bytes(),stopped)
 def test_lost_reply_idempotent_stop_no_second_term(self):
  # RUNNING proves exec, not application readiness. Synchronize the fixture's
  # handler before testing exactly-once TERM; keep all original assertions.
  self.start('trap \'echo TERM >>"$TEST_HOME/events"; exit 0\' TERM\necho ready >"$TEST_HOME/trap.ready"\nwhile :; do sleep .1; done\n');self.running();self.wait(lambda:(self.home/'trap.ready').exists());self.assertEqual(self.call('STOP').returncode,0);self.stopped()
  before=(self.domain/'state.json').read_bytes();self.assertEqual(self.call('STOP').returncode,0);self.assertEqual((self.domain/'state.json').read_bytes(),before);self.assertEqual((self.home/'events').read_text(),'TERM\n')
 def test_crashed_generation_cannot_restart_from_absent_pid(self):
  p=self.start();self.running();p.kill();p.wait(timeout=3);before=(self.domain/'state.json').read_bytes()
  r=subprocess.run([GEN,'run',str(self.domain),'new-generation',self.sha,'--','/bin/ash','-c','echo UNSAFE'],capture_output=True,timeout=5)
  self.assertNotEqual(r.returncode,0);self.assertIn(b'GENERATION_REQUIRES_EXPLICIT_RECOVERY',r.stderr);self.assertEqual((self.domain/'state.json').read_bytes(),before)

 def assert_drained_generation(self,end,root):
  self.assertEqual(end['state'],'STOPPED');self.assertTrue(end['termSent'])
  self.assertEqual(end['stopOperationId'],'operation-one');self.assertEqual(end['stopNonce'],'nonce-one')
  for key in ['children','awaitingBirth','exitedUnreaped']:self.assertEqual(end[key],[])
  self.assertFalse(self.live(root))
  records=[json.loads(p.read_bytes()) for p in sorted(self.domain.glob('revision-*.json'))]
  self.assertIn('DRAINING',[r['state'] for r in records])
  exact={p.name:p.read_bytes() for p in self.domain.glob('*.json')}
  self.assertEqual(self.call('STOP').returncode,0)
  self.assertEqual({p.name:p.read_bytes() for p in self.domain.glob('*.json')},exact)
  return records
 def test_supervised_ignored_term_drains_owned_tree(self):
  foreign=subprocess.Popen(['/bin/ash','-c','sleep 60']);self.processes.append(foreign)
  self.start('trap \'echo TERM >>"$TEST_HOME/events"\' TERM\necho ready >"$TEST_HOME/trap.ready"\nwhile :; do :; done\n')
  self.wait(lambda:(self.home/'trap.ready').exists());root=self.running()['updater']['pid']
  self.assertEqual(self.call('STOP').returncode,0)
  end=self.stopped();self.assert_drained_generation(end,root)
  self.assertEqual((self.home/'events').read_text(),'TERM\n');self.assertIsNone(foreign.poll())
 def test_cleanup_writer_forked_on_term_is_accounted_and_drained(self):
  self.writer_body()  # Creates the private writer/release fixture; starts nothing.
  body='trap \'echo TERM >>"$TEST_HOME/events"; /bin/ash "$TEST_HOME/writer.sh" &\' TERM\necho ready >"$TEST_HOME/trap.ready"\nwhile :; do :; done\n'
  self.start(body);self.wait(lambda:(self.home/'trap.ready').exists());initial=self.running();root=initial['updater']['pid']
  self.assertFalse((self.home/'writer.pid').exists())
  self.assertEqual(self.call('STOP').returncode,0);writer=self.writer_pid()
  end=self.stopped();records=self.assert_drained_generation(end,root)
  self.assertTrue(any(r['revision']>initial['revision'] and any(c['pid']==writer for c in r['children']) for r in records))
  self.assertFalse(self.live(writer));self.assertEqual((self.home/'events').read_text(),'TERM\n')
  (self.home/'release').touch();time.sleep(.1)
  self.assertEqual((self.home/'platform').read_text(),'original\n')

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
