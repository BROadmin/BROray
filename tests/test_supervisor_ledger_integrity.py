import json,os,shutil,subprocess,tempfile,time,unittest
from pathlib import Path
APP=Path('/field/runtime/app')
class LedgerIntegrity(unittest.TestCase):
 def setUp(self):
  self.t=tempfile.TemporaryDirectory();self.addCleanup(self.t.cleanup);self.p=Path(self.t.name)
  self.app=self.p/'app';shutil.copytree(APP/'lib',self.app/'lib')
  self.env={**os.environ,'BRORAY_ROOT':str(self.app),'BRORAY_BASE':str(self.app),'AUDIT':str(self.p)}
 def shell(self,code):return subprocess.run(['/bin/ash','-c',code],env=self.env,capture_output=True,text=True,timeout=20)
 def test_supervisor_corrupt_ledger_is_overwritten(self):
  from test_supervisor import ticks
  ledger=self.p/'ledger';ready=self.p/'ready';release=self.p/'release';control=self.p/'control'
  control.write_text('printf "%s\\t%s\\t%s\\t%s\\n" "$TEST_OWNER_PID" "$TEST_OWNER_TICKS" "$TEST_BOOT" "$TEST_LEDGER"\n')
  env={**os.environ,'BRORAY_BACKGROUND_OPERATION_ID':'op-20261002120000-1234-012345abcdef','TEST_OWNER_PID':str(os.getpid()),'TEST_OWNER_TICKS':ticks(os.getpid()),'TEST_BOOT':Path('/proc/sys/kernel/random/boot_id').read_text().strip(),'TEST_LEDGER':str(ledger)}
  helper=f'echo ready >"{ready}"; while [ ! -e "{release}" ]; do :; done; /bin/true; exit 0'
  p=subprocess.Popen(['/.local/bin/linux-supervisor','/bin/ash',str(control),str(self.p/'cancel'),'15','0','1','--','/bin/ash','-c',helper],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  self.addCleanup(lambda: p.kill() if p.poll() is None else None)
  until=time.monotonic()+5
  while not ready.exists() and p.poll() is None and time.monotonic()<until:time.sleep(.01)
  self.assertTrue(ready.exists());ledger.write_bytes(b'{broken');release.touch()
  stdout,stderr=p.communicate(timeout=8)
  self.assertNotEqual(p.returncode,0,(stdout,stderr));self.assertEqual(ledger.read_bytes(),b'{broken')

from test_supervisor import Supervisor
class BoundaryRecords(Supervisor):
 @classmethod
 def setUpClass(cls):
  import ctypes
  assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
 def damaged(self,part,remove=False):
  ready=self.temp/'ready';release=self.temp/'release'
  p=self.launch(f'echo ready >"{ready}"; while [ ! -e "{release}" ]; do :; done; /bin/true')
  self.wait_ready(p,ready)
  file=Path(str(self.authority)+part)
  if remove:file.unlink()
  else:file.write_bytes(b'{broken')
  release.touch();out,err=p.communicate(timeout=8)
  self.assertNotEqual(p.returncode,0,(out,err))
  if remove:self.assertFalse(file.exists())
  else:self.assertEqual(file.read_bytes(),b'{broken')
 def test_anchor_corruption_preserved(self):self.damaged('')
 def test_missing_projection_not_recreated(self):self.damaged('.current',True)
 def test_missing_authority_not_recreated(self):self.damaged('',True)
 def test_detected_projection_corruption_preserved(self):self.damaged('.current')
 def test_foreign_terminal_never_replaced(self):self.damaged('.terminal')
 def test_bounded_files_and_terminal_authority(self):
  p=self.launch('i=0; while [ "$i" -lt 100 ]; do /bin/true; i=$((i+1)); done')
  out,err=p.communicate(timeout=15);self.assertEqual(p.returncode,0,(out,err))
  self.assertEqual(json.loads(self.authority.read_text())['revision'],1)
  self.assertEqual(self.ledger.read_bytes(),Path(str(self.authority)+'.terminal').read_bytes())
  self.assertEqual(len(list(self.temp.glob('children.json*'))),3)
 def test_canonical_evidence_is_write_once(self):
  ready=self.temp/'ready';release=self.temp/'release'
  p=self.launch(f'echo ready >"{ready}"; while [ ! -e "{release}" ]; do :; done; /bin/true')
  self.wait_ready(p,ready)
  authority=getattr(self,'authority',self.ledger);before=authority.read_bytes()
  release.touch();out,err=p.communicate(timeout=8);self.assertEqual(p.returncode,0,(out,err))
  self.assertEqual(authority.read_bytes(),before,'authoritative evidence was replaced')
 def test_duplicate_registration_preserves_all_records(self):
  p=self.launch('/bin/true');p.communicate(timeout=5);self.assertEqual(p.returncode,0)
  before={f.name:f.read_bytes() for f in self.temp.glob('children.json*')}
  p=self.launch('echo forbidden');out,err=p.communicate(timeout=5)
  self.assertNotEqual(p.returncode,0,(out,err));self.assertNotIn(b'forbidden',out)
  self.assertEqual({f.name:f.read_bytes() for f in self.temp.glob('children.json*')},before)
 def test_projection_revision_rollback_preserved(self):
  ready=self.temp/'ready';release=self.temp/'release'
  p=self.launch(f'echo ready >"{ready}"; while [ ! -e "{release}" ]; do :; done; /bin/true')
  self.wait_ready(p,ready);old=self.authority.read_bytes();self.ledger.write_bytes(old)
  release.touch();out,err=p.communicate(timeout=8)
  self.assertNotEqual(p.returncode,0,(out,err));self.assertEqual(self.ledger.read_bytes(),old)
