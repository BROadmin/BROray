import json
from test_updater_generation import Generation

class CheckpointRetention(Generation):
 def test_checkpoint_budget_fails_closed_without_discarding_evidence(self):
  # Repeated root exec creates legitimate lifecycle changes. Child churn
  # does not: it is separately checked below. Overflow must not erase history.
  p=self.start('n=0; if [ -f "$TEST_HOME/count" ]; then read -r n <"$TEST_HOME/count"; fi\nn=$((n+1)); printf "%s\\n" "$n" >"$TEST_HOME/count"\nexec /bin/ash "$TEST_HOME/daemon.sh"\n')
  self.assertNotEqual(p.wait(timeout=15),0)
  files={f.name:f.read_bytes() for f in self.domain.glob('*.json')}
  self.assertEqual(len(files),32)
  self.assertLessEqual(sum(map(len,files.values())),1024*1024)
  self.assertNotEqual(self.state()['state'],'STOPPED')
  self.assertFalse(self.live(self.state()['updater']['pid']))
  self.assertNotEqual(self.call('RETIRE').returncode,0)
  self.assertEqual({f.name:f.read_bytes() for f in self.domain.glob('*.json')},files)

 def test_child_churn_does_not_persist_process_history(self):
  self.start('while [ ! -e "$TEST_HOME/go" ]; do read -t 1 -u "$BRORAY_UPDATER_IDLE_READ_FD" line || :; done\ni=0; while [ "$i" -lt 80 ]; do /bin/true || exit 12; i=$((i+1)); done\nprintf ready >"$TEST_HOME/workload.ready"\nwhile :; do :; done\n')
  self.running();before={p.name:p.read_bytes() for p in self.domain.glob('*.json')}
  (self.home/'go').touch();self.wait(lambda:(self.home/'workload.ready').exists(),seconds=30)
  after={p.name:p.read_bytes() for p in self.domain.glob('*.json')}
  self.assertEqual(set(after),set(before),'PROCESS_CHURN_PERSISTED_NEW_REVISIONS')
  self.assertEqual(after,before)
  self.assertEqual(self.call('STOP').returncode,0);self.stopped()

 def test_live_child_accounting_is_authenticated_and_does_not_write(self):
  self.start(self.writer_body());pid=self.writer_pid()
  before={p.name:p.read_bytes() for p in self.domain.glob('*.json')}
  reply=self.call('LIVE');self.assertEqual(reply.returncode,0,reply.stdout+reply.stderr)
  row=json.loads(reply.stdout);self.assertEqual(row['snapshotKind'],'volatile-live')
  self.assertTrue(any(child['pid']==pid for child in row['children']))
  self.assertEqual({p.name:p.read_bytes() for p in self.domain.glob('*.json')},before)
  self.assertEqual(self.call('STOP').returncode,0);self.stopped()
  self.assertFalse(self.live(pid));(self.home/'release').touch()
  self.assertEqual((self.home/'platform').read_text(),'original\n')

 def test_terminal_receipt_stays_small_and_successor_starts(self):
  from test_generation_installation import Installation
  self.start('i=0; while [ "$i" -lt 80 ]; do /bin/true || exit 12; i=$((i+1)); done\nprintf ready >"$TEST_HOME/workload.ready"\nwhile :; do :; done\n')
  self.wait(lambda:(self.home/'workload.ready').exists(),seconds=30)
  self.assertEqual(self.call('STOP').returncode,0);self.stopped()
  self.assertEqual(self.call('RETIRE').returncode,0)
  self.assertEqual(self.processes[0].wait(timeout=2),0)
  files=[p for p in self.domain.iterdir() if p.is_file()]
  total=sum(p.stat().st_size for p in files)
  print('CHECKPOINT_RETENTION '+json.dumps(dict(files=len(files),bytes=total)),flush=True)
  self.assertLessEqual(total,65536)
  proc,other=Installation.launch_next(self)
  def started():
   records=sorted(other.glob('revision-*.json'))
   return bool(records) and json.loads(records[-1].read_bytes())['state']=='RUNNING'
  self.wait(started);self.assertIsNone(proc.poll())
