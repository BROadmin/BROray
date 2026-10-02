"""Regression: failed rollback preserves evidence and cannot report success."""
import json,os,shutil,subprocess,tempfile,time,unittest
from pathlib import Path
APP=Path('/field/runtime/app')
class RouteRollback(unittest.TestCase):
 def setUp(self):
  self.t=tempfile.TemporaryDirectory();self.addCleanup(self.t.cleanup);self.p=Path(self.t.name)
  self.app=self.p/'app';shutil.copytree(APP/'lib',self.app/'lib')
  self.env={**os.environ,'BRORAY_ROOT':str(self.app),'BRORAY_BASE':str(self.app),'AUDIT':str(self.p)}
 def shell(self,code):return subprocess.run(['/bin/ash','-c',code],env=self.env,capture_output=True,text=True,timeout=20)
 def test_route_sync_false_rollback_success_and_backup_loss(self):
  routes=self.app/'routes';backup=self.p/'work/backup'
  backup.mkdir(parents=True)
  paths={'routes.json':routes/'installed/routes.json','bundle.json':routes/'installed/bundles/test.json','state.json':routes/'state/test.json','export-plan.json':routes/'catalog/test/export-plan.json'}
  for name,dest in paths.items():
   dest.parent.mkdir(parents=True,exist_ok=True);dest.write_text('NEW-'+name);(backup/name).write_text('OLD-'+name)
  r=self.shell('''. "$BRORAY_ROOT/lib/routes-router-sync.sh"
BRORAY_SYNC_BUNDLE=test
BRORAY_SYNC_WORK="$AUDIT/work"
BRORAY_SYNC_LOCAL_BACKUP="$AUDIT/work/backup"
BRORAY_SYNC_TRANSACTION="$AUDIT/transaction"
cp() { echo COPY_FAILURE >&2; return 1; }
# Real router restoration is outside this isolated local-state test.
broray_routes_sync_rollback_router() { return 0; }
broray_routes_sync_lock_release() { return 0; }
broray_routes_sync_abort "Injected local commit failure"
''')
  self.assertEqual(r.returncode,1,r.stderr)
  state=json.loads((self.p/'transaction/transaction.json').read_text())
  self.assertEqual(state['phase'],'rollback_failed')
  self.assertEqual(paths['routes.json'].read_text(),'NEW-routes.json')
  self.assertTrue(backup.exists()); self.assertEqual((backup/'routes.json').read_text(),'OLD-routes.json')
  self.assertTrue((routes/'rollback-required.json').exists())
 def test_export_delete_restore_suppress_io_failure(self):
  backup=self.p/'backup';backup.mkdir()
  for name in ['routes.json','bundle.json','state.json','export-plan.json']:(backup/name).write_text('OLD')
  for library,fn,args in [('routes-router-export.sh','broray_routes_router_export_restore_local','"$AUDIT/a" "$AUDIT/b" "$AUDIT/c" "$AUDIT/d" "$AUDIT/e" "$AUDIT/backup"'),('routes-router-delete.sh','broray_routes_delete_restore_local','"$AUDIT/backup" "$AUDIT/a" "$AUDIT/b" "$AUDIT/c" "$AUDIT/d" "$AUDIT/e"')]:
   with self.subTest(library=library):
    r=self.shell('. "$BRORAY_ROOT/lib/'+library+'"\ncp() { return 1; }\n'+fn+' '+args)
    self.assertNotEqual(r.returncode,0,r.stderr);self.assertFalse((self.p/'a').exists())
 def test_restore_success_preserves_original_bytes_and_mode(self):
  src=self.p/'original';dst=self.p/'live';src.write_bytes(b'original\x00bytes');src.chmod(0o640);dst.write_bytes(b'NEW')
  r=self.shell('. "$BRORAY_ROOT/lib/routes-resource-lock.sh"; broray_route_restore_file "$AUDIT/original" "$AUDIT/live"')
  self.assertEqual(r.returncode,0,r.stderr);self.assertEqual(dst.read_bytes(),src.read_bytes());self.assertEqual(dst.stat().st_mode & 0o777,0o640)
 def test_partial_restore_copy_does_not_truncate_live_or_backup(self):
  src=self.p/'original';dst=self.p/'live';src.write_bytes(b'original');dst.write_bytes(b'NEW')
  r=self.shell('''. "$BRORAY_ROOT/lib/routes-resource-lock.sh"
cp() { printf partial >"$3"; return 1; }
broray_route_restore_file "$AUDIT/original" "$AUDIT/live"
''')
  self.assertNotEqual(r.returncode,0);self.assertEqual(dst.read_bytes(),b'NEW');self.assertEqual(src.read_bytes(),b'original');self.assertEqual(list(self.p.glob('live.restore.*')),[])
 def test_delete_failed_cleanup_preserves_work_and_reports_failure(self):
  work=self.p/'work';work.mkdir();(work/'backup').mkdir();(work/'backup/routes.json').write_bytes(b'original')
  r=self.shell('''. "$BRORAY_ROOT/lib/routes-router-delete.sh"
BRORAY_ROUTES_DELETE_PAUSED=false
BRORAY_ROUTES_DELETE_COMMITTED=false
BRORAY_ROUTES_DELETE_ROLLBACK_NEEDED=true
BRORAY_ROUTES_DELETE_WORK="$AUDIT/work"
BRORAY_ROUTES_DELETE_ORIGINAL="$AUDIT/work/backup"
BRORAY_ROUTES_DELETE_ROUTES="$BRORAY_ROOT/routes"
BRORAY_ROUTES_DELETE_BUNDLE_ID=test
mkdir -p "$BRORAY_ROUTES_DELETE_ROUTES"
broray_routes_delete_kill_active() { :; }
broray_routes_delete_rollback_routes() { return 0; }
broray_routes_delete_restore_local() { return 1; }
broray_routes_delete_progress_fail() { printf '%s' "$2" >"$AUDIT/rolled-back"; }
broray_routes_delete_lock_release() { touch "$AUDIT/released"; }
broray_routes_delete_cleanup
''')
  self.assertNotEqual(r.returncode,0);self.assertEqual((work/'backup/routes.json').read_bytes(),b'original')
  self.assertEqual((self.p/'rolled-back').read_text(),'false');self.assertTrue((self.app/'routes/rollback-required.json').exists());self.assertTrue((self.p/'released').exists())
 def test_export_failed_cleanup_preserves_work_and_blocks_retry(self):
  work=self.p/'work';work.mkdir();(work/'backup').write_bytes(b'original')
  r=self.shell('''. "$BRORAY_ROOT/lib/routes-router-export.sh"
BRORAY_ROUTES_ROUTER_EXPORT_ROUTER_SAVED=false
BRORAY_ROUTES_ROUTER_EXPORT_LOCAL_COMMITTED=true
BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_WORK="$AUDIT/work"
BRORAY_ROUTES_ROOT="$BRORAY_ROOT/routes"
mkdir -p "$BRORAY_ROUTES_ROOT"
broray_routes_router_export_kill_active() { :; }
broray_routes_router_export_restore_local() { return 1; }
broray_routes_router_export_rollback_created() { return 0; }
broray_routes_router_export_lock_release() { touch "$AUDIT/released"; }
broray_routes_router_export_cleanup
''')
  self.assertNotEqual(r.returncode,0);self.assertEqual((work/'backup').read_bytes(),b'original')
  self.assertTrue((self.app/'routes/rollback-required.json').exists());self.assertTrue((self.p/'released').exists())
