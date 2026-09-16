"""All legacy route resource consumers preserve ambiguous/foreign generations."""
import json,os,shutil,subprocess,tempfile,time,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
SPECS=[('routes-source-check.sh','broray_routes_lock'),('routes-download.sh','broray_routes_download_lock'),('routes-export-build.sh','broray_routes_export_lock'),('routes-router-preflight.sh','broray_routes_preflight_lock'),('routes-router-export.sh','broray_routes_router_export_lock'),('routes-router-sync.sh','broray_routes_sync_lock'),('routes-router-delete.sh','broray_routes_delete_lock'),('routes-user-import.sh','broray_user_routes_lock')]
class RouteLocks(unittest.TestCase):
    def setUp(self):
        self.temp=Path(tempfile.mkdtemp(prefix='route-lock-',dir=ROOT/'.local'))
        self.app=self.temp/'app';(self.app/'lib').mkdir(parents=True)
        for p in (ROOT/'implementation/runtime/app/lib').iterdir():
            if p.is_file():shutil.copy2(p,self.app/'lib'/p.name)
        self.lock=self.app/'routes/locks/operation.lock';self.lock.parent.mkdir(parents=True)
        self.env=os.environ.copy();self.env.update({'BRORAY_ROOT':str(self.app),'BRORAY_ROUTES_ROOT':str(self.app/'routes'),'BRORAY_OPS_GUARD':str(ROOT/'.local/bin/linux-guard'),'BRORAY_OPS_ASH':'/bin/ash','BRORAY_ROUTES_API_LOCK':str(self.temp/'global.lock'),'BRORAY_UPDATER_REQUEST_LOCK':str(self.temp/'updater/request.lock'),'BRORAY_LEGACY_GLOBAL_LOCK':str(self.temp/'legacy.lock'),'BRORAY_UPDATER_OPERATION_POINTER':str(self.temp/'updater/last-operation'),'BRORAY_ROUTES_API_STALE_LOCK_ROOT':str(self.temp/'archive')})
    def tearDown(self):
        assert self.temp.resolve().parent==(ROOT/'.local').resolve()
        shutil.rmtree(self.temp)
    def shell(self,body,expected=0):
        p=subprocess.run(['/bin/ash','-c','set -eu\n'+body],env=self.env,capture_output=True,timeout=15)
        self.assertEqual(p.returncode,expected,(body,p.stdout,p.stderr));return p
    def before(self):return {p.name:p.read_bytes() for p in self.lock.iterdir() if p.is_file()}
    def test_all_eight_consumers_acquire_and_release_own_generation(self):
        for module,prefix in SPECS:
            with self.subTest(module=module):
                bundle='' if prefix in ['broray_routes_sync_lock','broray_user_routes_lock'] else 'fixture'
                extra='[ "$(cat "$BRORAY_ROOT/routes/locks/operation.lock/operation")" = delete ]; [ -s "$BRORAY_ROOT/routes/locks/operation.lock/startedAt" ];' if prefix=='broray_routes_delete_lock' else ''
                self.shell(f'. "$BRORAY_ROOT/lib/{module}"\nBRORAY_ROUTES_ACTIVE_BUNDLE=fixture; BRORAY_ROUTES_EXPORT_BUNDLE=fixture; BRORAY_ROUTES_PREFLIGHT_BUNDLE=fixture; BRORAY_ROUTES_ROUTER_EXPORT_BUNDLE=fixture\n{prefix}_acquire fixture\njq -e --arg bundle "{bundle}" \' .kind=="route-resource-lock" and .owner.pid>1 and .bundle==$bundle \' "$BRORAY_ROOT/routes/locks/operation.lock/owner.json" >/dev/null\n{extra}\n{prefix}_release\n')
                self.assertFalse(self.lock.exists())
        self.assertTrue((self.lock.parent/'resource.control.guard').is_file())
    def test_all_eight_preserve_unknown_existing_lock(self):
        self.lock.mkdir();(self.lock/'foreign').write_text('KEEP')
        before=self.before()
        for module,prefix in SPECS:
            with self.subTest(module=module):
                self.shell(f'. "$BRORAY_ROOT/lib/{module}"\nrc=0; {prefix}_acquire fixture || rc=$?\n[ "$rc" = 2 ]\n{prefix}_release\n')
                self.assertEqual(self.before(),before)
    def test_actual_check_download_and_export_build_with_private_http_fixture(self):
        routes=self.app/'routes';(routes/'manifests').mkdir();(routes/'tmp').mkdir()
        seed=ROOT/'implementation/runtime/state-seed/routes'
        shutil.copy2(seed/'config.json',routes/'config.json')
        manifest=json.loads((seed/'manifests/telegram.json').read_bytes());manifest['source'].pop('discovery')
        (routes/'manifests/telegram.json').write_text(json.dumps(manifest))
        (routes/'bundles.json').write_text('{"schemaVersion":1,"bundles":["telegram"]}')
        fixture=self.temp/'http-fixture';fixture.mkdir();self.env['BRORAY_ROUTES_HTTP_FIXTURE_DIR']=str(fixture)
        (fixture/'commit.json').write_text(json.dumps([{'sha':'a'*40,'commit':{'committer':{'date':'2026-09-16T00:00:00Z'}}}]))
        (fixture/'telegram.bat').write_text('route ADD 203.0.113.0 MASK 255.255.255.0 0.0.0.0\nroute ADD 198.51.100.0 MASK 255.255.255.0 0.0.0.0\n')
        self.shell('. "$BRORAY_ROOT/lib/routes-source-check.sh"\nbroray_routes_check_run telegram\n')
        self.assertFalse(self.lock.exists())
        state=routes/'state/telegram.json';self.assertEqual(json.loads(state.read_bytes())['routeCount'],2)
        self.shell('. "$BRORAY_ROOT/lib/routes-download.sh"\nbroray_routes_download_run telegram\n')
        self.assertFalse(self.lock.exists());self.assertEqual(json.loads(state.read_bytes())['status'],'downloaded')
        self.shell('. "$BRORAY_ROOT/lib/routes-export-build.sh"\nbroray_routes_export_build_run telegram\n')
        self.assertFalse(self.lock.exists());self.assertEqual(json.loads(state.read_bytes())['routeCount'],2)
    def test_dead_pid_and_old_boot_evidence_are_not_reclaimed(self):
        self.shell('. "$BRORAY_ROOT/lib/routes-download.sh"\nbroray_routes_download_lock_acquire fixture\n')
        before=self.before()
        self.shell('. "$BRORAY_ROOT/lib/routes-download.sh"\nrc=0; broray_routes_download_lock_acquire fixture || rc=$?\n[ "$rc" = 2 ]\n')
        self.assertEqual(self.before(),before)
        record=json.loads((self.lock/'owner.json').read_bytes());record['owner']['bootId']='old-boot';(self.lock/'owner.json').write_text(json.dumps(record))
        before=self.before();self.shell('. "$BRORAY_ROOT/lib/routes-source-check.sh"\nrc=0; broray_routes_lock_acquire fixture || rc=$?\n[ "$rc" = 2 ]\n');self.assertEqual(self.before(),before)
    def test_copied_token_cannot_authorize_a_child_release(self):
        self.shell('''. "$BRORAY_ROOT/lib/routes-download.sh"
broray_routes_download_lock_acquire fixture
export TEST_TOKEN="$BRORAY_ROUTES_DOWNLOAD_LOCK_TOKEN"
/bin/ash -c '. "$BRORAY_ROOT/lib/routes-resource-lock.sh"; rc=0; broray_route_resource_release "$BRORAY_ROOT/routes/locks/operation.lock" "$TEST_TOKEN" || rc=$?; [ "$rc" = 2 ]'
[ -f "$BRORAY_ROOT/routes/locks/operation.lock/owner.json" ]
broray_routes_download_lock_release
''');self.assertFalse(self.lock.exists())
    def test_unknown_added_evidence_is_preserved_on_release(self):
        self.shell('''. "$BRORAY_ROOT/lib/routes-download.sh"
broray_routes_download_lock_acquire fixture
echo KEEP >"$BRORAY_ROUTES_DOWNLOAD_LOCK/foreign"
rc=0; broray_routes_download_lock_release || rc=$?
[ "$rc" = 2 ]
[ "$(cat "$BRORAY_ROUTES_DOWNLOAD_LOCK/foreign")" = KEEP ]
[ -f "$BRORAY_ROUTES_DOWNLOAD_LOCK/owner.json" ]
rm "$BRORAY_ROUTES_DOWNLOAD_LOCK/foreign"
broray_routes_download_lock_release
''');self.assertFalse(self.lock.exists())
    def test_changed_generation_is_never_removed(self):
        self.shell('''. "$BRORAY_ROOT/lib/routes-download.sh"
broray_routes_download_lock_acquire fixture
jq '.token="00000000000000000000000000000000"' "$BRORAY_ROUTES_DOWNLOAD_LOCK/owner.json" >"$BRORAY_ROOT/new-owner"
cat "$BRORAY_ROOT/new-owner" >"$BRORAY_ROUTES_DOWNLOAD_LOCK/owner.json"
rc=0; broray_routes_download_lock_release || rc=$?
[ "$rc" = 2 ]
cmp "$BRORAY_ROOT/new-owner" "$BRORAY_ROUTES_DOWNLOAD_LOCK/owner.json"
''')
    def test_symlink_and_hidden_evidence_remain_untouched(self):
        foreign=self.temp/'foreign';foreign.mkdir();(foreign/'keep').write_text('KEEP');self.lock.symlink_to(foreign)
        self.shell('. "$BRORAY_ROOT/lib/routes-download.sh"\nrc=0; broray_routes_download_lock_acquire fixture || rc=$?\n[ "$rc" = 2 ]\nbroray_routes_download_lock_release\n')
        self.assertTrue(self.lock.is_symlink());self.assertEqual((foreign/'keep').read_text(),'KEEP')
    def test_five_file_global_lock_cannot_be_archived_by_pid_absence(self):
        lock=self.temp/'global.lock';lock.mkdir()
        for name,value in {'pid':'99999999','scope':'routes','action':'download','bundle':'fixture','startedAt':'2000'}.items():(lock/name).write_text(value+'\n')
        before={p.name:p.read_bytes() for p in lock.iterdir()}
        self.shell('''. "$BRORAY_ROOT/lib/routes-api-operation.sh"
rc=0; broray_routes_api_lock_reclaim_stale || rc=$?; [ "$rc" = 1 ]
rc=0; broray_routes_api_lock_acquire download fixture || rc=$?; [ "$rc" = 2 ]
broray_routes_api_lock_release
''')
        self.assertEqual({p.name:p.read_bytes() for p in lock.iterdir()},before);self.assertFalse((self.temp/'archive').exists())
    def test_kernel_guard_conflict_is_bounded_and_next_owner_can_acquire(self):
        marker=self.temp/'ready'
        child=subprocess.Popen([self.env['BRORAY_OPS_GUARD'],str(self.lock.parent/'resource.control.guard'),'/bin/ash','-c','echo ready >"$1"; n=0; while [ ! -f "$1.stop" ] && [ "$n" -lt 20 ]; do sleep 1; n=$((n+1)); done','holder',str(marker)],env=self.env)
        try:
            deadline=time.monotonic()+3
            while not marker.exists() and time.monotonic()<deadline:time.sleep(.01)
            self.assertTrue(marker.exists());started=time.monotonic()
            self.shell('. "$BRORAY_ROOT/lib/routes-download.sh"\nrc=0; broray_routes_download_lock_acquire fixture || rc=$?\n[ "$rc" = 2 ]\n')
            # Native guard waits up to two seconds; include process launch and
            # software-emulated guest overhead in the overall bound.
            self.assertLess(time.monotonic()-started,6)
        finally:Path(str(marker)+'.stop').touch();child.wait(timeout=5)
        self.shell('. "$BRORAY_ROOT/lib/routes-download.sh"\nbroray_routes_download_lock_acquire fixture\nbroray_routes_download_lock_release\n')
if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(RouteLocks))
    (ROOT/'docs/evidence/route-resource-locks-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'Linux native guard, real owner identities, private route files','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
