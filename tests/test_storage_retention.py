"""Bounded logs and committed route history, with real Linux guard exclusion."""
import gzip,json,os,shutil,subprocess,tempfile,unittest
from pathlib import Path

ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
GUARD=Path('/work/.local/bin/linux-guard')

class Retention(unittest.TestCase):
 def setUp(self):
  self.root=Path(tempfile.mkdtemp(prefix='retention-'))
  self.app=self.root/'app';self.state=self.root/'state';self.state.mkdir()
  shutil.copytree(ROOT/'runtime/app/lib',self.app/'lib')
  (self.app/'bin').mkdir();shutil.copy(ROOT/'runtime/app/bin/broray-log-maintenance',self.app/'bin/broray-log-maintenance')
  (self.app/'logs').mkdir();(self.app/'routes/locks').mkdir(parents=True)
  (self.app/'routes/operations').mkdir();(self.app/'routes/transactions').mkdir()
  self.env=os.environ|{'BRORAY_ROOT':str(self.app),'BRORAY_STATE_ROOT':str(self.state),
   'BRORAY_OPS_GUARD':str(GUARD),'BRORAY_OPS_ASH':'/bin/ash',
   'BRORAY_ROUTES_API_LOCK':str(self.root/'global.lock'),
   'BRORAY_LEGACY_GLOBAL_LOCK':str(self.root/'legacy.lock')}
 def tearDown(self):shutil.rmtree(self.root)
 def maintenance(self):
  return subprocess.run(['/bin/ash',str(self.app/'bin/broray-log-maintenance')],env=self.env,capture_output=True,timeout=25)
 def prune(self):
  lock=self.app/'routes/locks/operation.lock'
  return subprocess.run([str(GUARD),str(self.state/'operations.guard'),str(GUARD),str(lock.parent/'resource.control.guard'),
   '/bin/ash',str(self.app/'lib/routes-resource-recover.sh'),str(lock),'history','prune'],env=self.env,capture_output=True,timeout=25)
 def transaction(self,n,phase='committed',size=16):
  d=self.app/f'routes/transactions/20260101-{n:06d}-sync-test-123';d.mkdir()
  (d/'transaction.json').write_text(json.dumps(dict(schemaVersion=1,operation='sync',bundleId='test',phase=phase,updatedAt=f'2026-01-01T00:00:{n:02d}Z')))
  (d/'plan.json').write_bytes(b'x'*size)
  return d
 def test_home_log_rotates_with_bounded_recent_tail(self):
  f=self.app/'logs/home-snapshots.log';data=b'A'*300000+b'LATEST\n';f.write_bytes(data)
  self.assertEqual(self.maintenance().returncode,0)
  self.assertEqual(f.stat().st_size,0)
  self.assertEqual(gzip.decompress(Path(str(f)+'.1.gz').read_bytes()),data[-262144:])
 def test_rotation_count_and_link_preservation(self):
  f=self.app/'logs/home-snapshots.log'
  for n in range(4):f.write_bytes(bytes([65+n])*300000);self.assertEqual(self.maintenance().returncode,0)
  self.assertEqual(len(list(f.parent.glob(f.name+'.*.gz'))),2)
  foreign=self.root/'foreign';foreign.write_bytes(b'P'*300000)
  f.unlink();f.symlink_to(foreign);self.maintenance();self.assertEqual(foreign.read_bytes(),b'P'*300000)
  f.unlink();os.link(foreign,f);self.maintenance();self.assertEqual(foreign.read_bytes(),b'P'*300000)
 def test_committed_route_history_keeps_five_newest(self):
  dirs=[self.transaction(i) for i in range(8)]
  r=self.prune();self.assertEqual(r.returncode,0,r.stderr)
  self.assertEqual([d.exists() for d in dirs],[False]*3+[True]*5)
 def test_history_budget_includes_single_oversized_record(self):
  dirs=[self.transaction(i,size=2000000) for i in range(3)]
  self.assertEqual(self.prune().returncode,0)
  self.assertEqual([d.exists() for d in dirs],[False,True,True])
  oversized=self.transaction(8,size=5000000)
  self.assertEqual(self.prune().returncode,0);self.assertFalse(oversized.exists())
 def test_router_offset_dates_are_pruned_and_sorted_in_utc(self):
  dates=['2026-01-01T03:00:01+0300','2025-12-31T19:00:02-0500','2026-01-01T00:00:03Z',
         '2026-01-01T03:00:04+0300','2026-01-01T00:00:05Z','2025-12-31T19:00:06-0500',
         '2026-01-01T03:00:07+0300','2026-01-01T00:00:08Z']
  dirs=[self.transaction(i) for i in range(8)]
  for d,date in zip(dirs,dates):
   f=d/'transaction.json';row=json.loads(f.read_text());row['updatedAt']=date;f.write_text(json.dumps(row))
  self.assertEqual(self.prune().returncode,0)
  self.assertEqual([d.exists() for d in dirs],[False]*3+[True]*5)
 def test_invalid_dates_and_offsets_preserve_evidence(self):
  dates=['2026-02-30T00:00:00Z','2026-01-01T00:00:00+2400','2026-01-01T00:00:00+0360',
         '2026-01-01T00:00:00+ABCD','2026-01-01T00:00:00Zextra','2026-01-01X00:00:00Z']
  dirs=[self.transaction(i) for i in range(8)]
  protected=[]
  for i,date in enumerate(dates,10):
   d=self.transaction(i);f=d/'transaction.json';row=json.loads(f.read_text());row['updatedAt']=date;f.write_text(json.dumps(row));protected.append(d)
  self.assertEqual(self.prune().returncode,0)
  self.assertEqual([d.exists() for d in dirs],[False]*3+[True]*5)
  self.assertTrue(all(d.exists() for d in protected))
 def test_unknown_corrupt_paused_and_links_are_preserved(self):
  for i in range(8):self.transaction(i)
  protected=[self.transaction(10,'paused'),self.transaction(11,'rollback_failed'),self.transaction(12)]
  (protected[-1]/'transaction.json').write_text('{broken')
  unknown=self.transaction(13);(unknown/'unknown').write_text('preserve');protected.append(unknown)
  linked=self.transaction(14);foreign=self.root/'foreign';foreign.write_text('preserve');(linked/'plan.json').unlink();(linked/'plan.json').symlink_to(foreign);protected.append(linked)
  before={str(f):f.read_bytes() for d in protected for f in d.rglob('*') if f.is_file()}
  self.assertEqual(self.prune().returncode,0)
  self.assertTrue(all(d.exists() for d in protected));self.assertEqual(before,{str(f):f.read_bytes() for d in protected for f in d.rglob('*') if f.is_file()})
 def test_live_global_legacy_resource_and_resume_each_block_cleanup(self):
  dirs=[self.transaction(i) for i in range(8)]
  for fence in [self.root/'global.lock',self.root/'legacy.lock',self.app/'routes/locks/operation.lock']:
   fence.mkdir();r=self.prune();self.assertNotEqual(r.returncode,0);self.assertTrue(all(d.exists() for d in dirs));fence.rmdir()
  progress=self.app/'routes/operations/test.json'
  for row in [dict(running=True),dict(running=False,resumable=True),None]:
   progress.write_text(json.dumps(row) if row else '{broken');self.assertNotEqual(self.prune().returncode,0);self.assertTrue(all(d.exists() for d in dirs))
 def test_missing_guard_refuses_cleanup(self):
  dirs=[self.transaction(i) for i in range(8)]
  r=subprocess.run(['/bin/ash',str(self.app/'lib/routes-resource-recover.sh'),str(self.app/'routes/locks/operation.lock'),'history','prune'],env=self.env,capture_output=True)
  self.assertNotEqual(r.returncode,0);self.assertTrue(all(d.exists() for d in dirs))
 def test_maintenance_dispatches_serialized_route_cleanup(self):
  dirs=[self.transaction(i) for i in range(8)]
  r=self.maintenance();self.assertEqual(r.returncode,0,r.stderr)
  self.assertEqual([d.exists() for d in dirs],[False]*3+[True]*5)
 def test_export_delete_records_share_count_limit_and_wrong_names_stay(self):
  root=self.app/'routes/transactions';dirs=[self.transaction(i) for i in range(5)]
  for operation,sec in [('export',6),('delete',7)]:
   (root/f'{operation}-test-20260101-00000{sec}.json').write_text(json.dumps(dict(schemaVersion=1,operation=operation,bundleId='test',phase='committed',updatedAt=f'2026-01-01T00:00:0{sec}Z')))
  unknown=root/'foreign.json';unknown.write_text((root/'export-test-20260101-000006.json').read_text())
  self.assertEqual(self.prune().returncode,0);self.assertEqual([d.exists() for d in dirs],[False,False,True,True,True]);self.assertTrue(unknown.exists())
 def test_corrupt_runtime_json_and_hardlinks_never_deleted(self):
  for i in range(8):self.transaction(i)
  d=self.transaction(10);foreign=self.root/'foreign';foreign.write_text('keep');(d/'plan.json').unlink();os.link(foreign,d/'plan.json')
  self.assertEqual(self.prune().returncode,0);self.assertTrue(d.exists());self.assertEqual(foreign.read_text(),'keep')

if __name__=='__main__':unittest.main(verbosity=2)
