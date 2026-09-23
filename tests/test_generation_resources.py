"""Required lifetime-resource check at the physically observed target limit.

Only the offline Linux VM is permitted. No router or host kernel mutation.
"""
from pathlib import Path
import json,os,time,unittest
from test_updater_generation import Generation

class Resources(Generation):
 def test_persistent_generation_survives_target_watch_budget(self):
  self.assertEqual(os.environ.get('BRORAY_TEST_ROOT'),'/work/implementation')
  knob=Path('/proc/sys/fs/inotify/max_user_watches');before=knob.read_bytes()
  try:
   knob.write_text('8192\n') # Same value captured from the test Keenetic.
   p=self.start('i=0\nwhile [ "$i" -lt 3000 ]; do /bin/true || exit 12; i=$((i+1)); echo "$i" >"$TEST_HOME/progress"; done\necho ready >"$TEST_HOME/workload.ready"\nwhile :; do :; done\n')
   self.wait(lambda:p.poll() is not None or (self.home/'workload.ready').exists(),seconds=180)
   records=[f for f in self.domain.iterdir() if f.is_file()]
   print('GENERATION_RESOURCE_EVIDENCE '+json.dumps({'targetMaxUserWatches':8192,'childrenRequested':3000,'childrenCompleted':(self.home/'progress').read_text() if (self.home/'progress').exists() else None,'ledgerFiles':len(records),'ledgerBytes':sum(f.stat().st_size for f in records),'supervisorExit':p.poll()}),flush=True)
   self.assertIsNone(p.poll(),'PERSISTENT_GENERATION_EXHAUSTED_TARGET_WATCH_BUDGET')
   self.assertTrue((self.home/'workload.ready').exists())
   watches=sum(sum(line.startswith('inotify wd:') for line in f.read_text().splitlines()) for f in Path(f'/proc/{p.pid}/fdinfo').iterdir())
   print('GENERATION_WATCH_COUNT '+json.dumps({'watches':watches,'records':len(records)}),flush=True)
   self.assertLessEqual(watches,4,'Watch use must not grow with generation age')
   self.assertEqual(self.call('STOP').returncode,0);self.stopped()
  finally:knob.write_bytes(before)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([Resources('test_persistent_generation_survives_target_watch_budget')]))
 raise SystemExit(0 if r.wasSuccessful() else 1)
