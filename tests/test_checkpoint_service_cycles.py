"""Unchanged public lifecycle assertions with the real RAM workspace contract.

The old canonical export predates RAM-only updater work and contains no /tmp.
Supply an actual tmpfs inside the isolated VM; never bypass its production check.
"""
import json,subprocess,unittest
from test_installed_service_cycles import ServiceCycles

class CheckpointServiceCycles(ServiceCycles):
 def test_compact_platform_one(self):
  self.installed();current=self.success('start');self.success('stop')
  domain=self.updater/'generations'/current['generationId'];rows=(domain/'retirement.receipt').read_text().splitlines()
  result=subprocess.run([str(self.native),'compact-retired',str(domain),rows[1],rows[2],rows[3],rows[4]],capture_output=True,text=True,timeout=30)
  print('PLATFORM_COMPACTION_DIAG '+json.dumps({'rc':result.returncode,'stdout':result.stdout,'stderr':result.stderr,
   'stages':{str(p.relative_to(self.updater)):sorted(str(f.relative_to(p)) for f in p.rglob('*')) for p in self.updater.glob('.broray-retention-*')},
   'starts':sorted(str(p.relative_to(self.updater/'starts')) for p in (self.updater/'starts').rglob('*'))}),flush=True)
  self.assertEqual(result.returncode,0,result.stdout+result.stderr)
  self.assertTrue((domain/'retention.record').exists());self.assertTrue((self.updater/'starts'/rows[1]/'retention.record').exists())
 def setUp(self):
  super().setUp()
  self.ram=self.root/'router/tmp'
  self.assertFalse(self.ram.exists() or self.ram.is_symlink())
  self.ram.mkdir(mode=0o700)
  subprocess.run(['mount','-t','tmpfs','-o','mode=1777,size=16m','tmpfs',str(self.ram)],check=True,timeout=5)
  # Clean up after the inherited exact-generation stop and fixture restore.
  self._cleanups.insert(0,(self.release_ram,(),{}))
 def test_multiple_cycles_idempotent_start_and_public_restart(self):
  super().test_multiple_cycles_idempotent_start_and_public_restart()
  domains=sorted((self.updater/'generations').glob('*/retention.record'))
  starts=sorted((self.updater/'starts').glob('*/retention.record'))
  self.assertEqual(len(domains),3,'RETIRED_GENERATIONS_NOT_COMPACTED')
  self.assertEqual({p.parent.name for p in domains},{p.parent.name for p in starts},'WITNESSES_NOT_COMPACTED')
  self.assertFalse(list(self.updater.glob('.broray-retention-*/replacement')),'ORIGINAL_JOURNAL_NOT_RECLAIMED')
  sizes={p.parent.name:sum(f.stat().st_size for root in [p.parent,self.updater/'starts'/p.parent.name] for f in root.rglob('*') if f.is_file()) for p in domains}
  self.assertTrue(all(size<=65536 for size in sizes.values()),sizes)
  print('PUBLIC_TERMINAL_RETENTION '+json.dumps({'generations':len(domains),'logicalBytes':sizes,'witnessesCompacted':True,'originalTreesReclaimed':True}),flush=True)
 def test_compact_replay_public(self):
  self.installed();platform=self.bytes_now();original=None;seen=set();retained=None
  sentinel=subprocess.Popen(['/bin/ash','-c','while :; do sleep 1; done','xray-foreign-fixture'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  try:
   for index in range(3):
    current=self.success('start');self.assertNotIn(current['generationId'],seen);seen.add(current['generationId']);self.one_live(current['generationId'])
    if original is None:original=self.files(self.op)
    self.assertEqual(self.files(self.op),original);self.assertEqual(self.bytes_now(),platform);self.assertIsNone(sentinel.poll())
    records=sorted((self.updater/'generations').glob('*/retention.record'))
    self.assertEqual(len(records),index)
    self.assertEqual({p.parent.name for p in records},{p.parent.name for p in (self.updater/'starts').glob('*/retention.record')})
    self.assertFalse(list(self.updater.glob('.broray-retention-*/replacement')))
    if retained is not None:self.assertEqual(self.files(retained[0]),retained[1])
    if records:retained=(records[0].parent,self.files(records[0].parent))
    self.success('stop');self.assertIsNone(sentinel.poll())
   self.assertEqual(len(seen),3)
   print('PUBLIC_TERMINAL_REPLAY '+json.dumps({'generations':3,'compacted':2,'repeatPreserved':True,'platformUnchanged':True,'migrationUnchanged':True,'foreignXrayPreserved':True}),flush=True)
  finally:
   if sentinel.poll() is None:sentinel.terminate()
   sentinel.wait(timeout=5)
 def release_ram(self):
  records=list((self.updater/'generations').glob('*/*.json'))
  witnesses=list((self.updater/'starts').glob('*/ledger-witnesses/*.json'))
  print('PUBLIC_CHECKPOINT_FOOTPRINT '+json.dumps(dict(records=len(records),witnesses=len(witnesses),bytes=sum(p.stat().st_size for p in records+witnesses))),flush=True)
  subprocess.run(['umount',str(self.ram)],check=True,timeout=5)
  self.ram.rmdir()

if __name__=='__main__':
 # Targeted journal-change gate. The inherited adversarial cases remain
 # individually selectable; do not repeat unrelated higher-level cases here.
 names=['test_multiple_cycles_idempotent_start_and_public_restart']
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(CheckpointServiceCycles(name) for name in names))
 raise SystemExit(not result.wasSuccessful())
