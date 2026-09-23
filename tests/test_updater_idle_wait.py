"""Idle pipe failures must abort, never become an EOF busy loop."""
from pathlib import Path
import os,subprocess,tempfile,time,unittest

ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
UPDATER=ROOT/'runtime/app/share/updater-platform/opt/libexec/broray-updater/broray-updater.sh'

class IdleWait(unittest.TestCase):
 def setUp(self):
  data=UPDATER.read_text();self.body=data[data.index('\nupdater_idle_wait()\n')+1:data.index('\ndaemon_run()\n')]
  self.fds=[]
 def tearDown(self):
  for fd in self.fds:
   try:os.close(fd)
   except OSError:pass
 def pipe(self):
  r,w=os.pipe();self.fds.extend([r,w]);return r,w
 def call(self,r=None,w=None,keep=None):
  env={**os.environ,'BRORAY_UPDATER_GENERATION':'fixture'}
  for name,value in [('READ',r),('WRITE',w)]:
   env.pop('BRORAY_UPDATER_IDLE_'+name+'_FD',None)
   if value is not None:env['BRORAY_UPDATER_IDLE_'+name+'_FD']=str(value)
  return subprocess.run(['/bin/ash','-c','set -u\n'+self.body+'\nupdater_idle_wait\n'],env=env,pass_fds=tuple(self.fds if keep is None else keep),capture_output=True,text=True,timeout=4)
 def test_native_pipe_waits_without_eof(self):
  r,w=self.pipe();start=time.monotonic();p=self.call(r,w)
  self.assertEqual(p.returncode,0,p.stderr);self.assertGreaterEqual(time.monotonic()-start,1.8)
 def test_missing_pipe_fails_closed(self):self.assertEqual(self.call().returncode,75)
 def test_invalid_descriptor_fails_closed(self):self.assertEqual(self.call('bad',9).returncode,75)
 def test_reversed_pipe_modes_fail_closed(self):
  r,w=self.pipe();self.assertEqual(self.call(w,r).returncode,75)
 def test_closed_write_end_fails_closed(self):
  r,w=self.pipe();self.assertEqual(self.call(r,w,keep=[r]).returncode,75)
 def test_same_descriptor_fails_closed(self):
  r,w=self.pipe();self.assertEqual(self.call(r,r).returncode,75)
 def test_two_read_descriptors_fail_closed(self):
  r,w=self.pipe();second=os.dup(r);self.fds.append(second)
  self.assertEqual(self.call(r,second).returncode,75)
 def test_different_pipes_fail_closed(self):
  r,w=self.pipe();r2,w2=self.pipe();self.assertEqual(self.call(r,w2).returncode,75)
 def test_regular_file_is_not_a_wait_pipe(self):
  with tempfile.TemporaryFile() as f:
   self.assertEqual(self.call(f.fileno(),f.fileno(),keep=[f.fileno()]).returncode,75)
 def test_unexpected_pipe_data_fails_closed(self):
  r,w=self.pipe();os.write(w,b'UNEXPECTED\n');self.assertEqual(self.call(r,w).returncode,75)

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
