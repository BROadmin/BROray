"""Saved F03 probe against current exact source; stronger preservation cases."""
from pathlib import Path
import ast,hashlib,json,unittest
from test_updater_preflight import UpdaterPreflight

saved=Path(__file__).parent/'fixtures/baseline_f01_f05.py.txt'
tree=ast.parse(saved.read_text());definitions=ast.Module(body=[n for n in tree.body if isinstance(n,(ast.Import,ast.ImportFrom,ast.FunctionDef))],type_ignores=[])
probe={};exec(compile(definitions,str(saved),'exec'),probe)

class DaemonExclusion(UpdaterPreflight):
 def test_original_f03_duplicate_daemon(self):
  result=probe['duplicate'](self);print('F03_CURRENT_SOURCE '+json.dumps(result),flush=True)
  self.assertFalse(result['bugReproduced']);self.assertTrue(result['ownerAlive']);self.assertNotEqual(result['secondClaimReturncode'],0);self.assertTrue(result['readyExistsAfterSecondClaim'])
 def test_unknown_owner_preserves_all_daemon_evidence(self):
  source,env=probe['library'](self);self.assertEqual(probe['call_lib'](env,'ensure_layout').returncode,0)
  root=self.root/'opt/var/lib/broray-updater';lock=root/'daemon.lock';lock.mkdir();(lock/'unconfirmed').write_text('KEEP\n');(root/'daemon.ready').write_bytes(b'UNKNOWN_READY');(root/'daemon.pid').write_text('2147483646\n')
  before={p.relative_to(root).as_posix():p.read_bytes() for p in root.rglob('*') if p.is_file()}
  result=probe['call_lib'](env,'daemon_lock_claim');self.assertNotEqual(result.returncode,0)
  self.assertEqual({p.relative_to(root).as_posix():p.read_bytes() for p in root.rglob('*') if p.is_file()},before)
 def test_empty_daemon_fence_is_not_reclaimed(self):
  source,env=probe['library'](self);self.assertEqual(probe['call_lib'](env,'ensure_layout').returncode,0)
  root=self.root/'opt/var/lib/broray-updater';lock=root/'daemon.lock';lock.mkdir()
  result=probe['call_lib'](env,'daemon_lock_claim');self.assertNotEqual(result.returncode,0);self.assertEqual(list(lock.iterdir()),[]);self.assertFalse((root/'daemon.pid').exists())

if __name__=='__main__':
 names=[n for n in DaemonExclusion.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(DaemonExclusion(n) for n in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)
