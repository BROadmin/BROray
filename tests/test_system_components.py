"""Uninstall component dispatch with actual self identity; isolated business calls.

The Linux tests substitute the OPKG transition fence, whose real implementation
is separately exercised on ARM. They do not assert native OPKG safety.
"""
import json,os,shutil,subprocess,unittest
from pathlib import Path
from test_route_entry import ROOT,RouteEntry

SETUP='''
set -e
. "$BRORAY_ROOT/lib/broray-page.sh"
. "$BRORAY_ROOT/lib/component-lifecycle.sh"
operation_id=uninstall-fixture
mkdir -p "$BRORAY_GLOBAL_LOCK"
printf 'system\\n' >"$BRORAY_GLOBAL_LOCK/scope"
printf 'uninstall\\n' >"$BRORAY_GLOBAL_LOCK/action"
: >"$BRORAY_GLOBAL_LOCK/bundle"
printf '2026-09-16T00:00:00Z\\n' >"$BRORAY_GLOBAL_LOCK/startedAt"
printf '%s\\n' "$operation_id" >"$BRORAY_GLOBAL_LOCK/operation-id"
broray_tx_control_owner_identity_capture "$$" "$BRORAY_GLOBAL_LOCK/owner-identity.tsv"
broray_system_global_control_validate uninstall "$operation_id"
broray_tx_control_owner_assert_self "$BRORAY_GLOBAL_LOCK/owner-identity.tsv"
# Only the native OPKG fence is replaced in this Linux wiring test.
broray_tx_control_transition_begin() { FIXTURE_FENCE=held; }
broray_tx_control_transition_assert() { [ "${FIXTURE_FENCE:-}" = held ]; }
broray_tx_control_transition_end() { FIXTURE_FENCE=; }
'''
class SystemComponents(unittest.TestCase):
    setUpBase=RouteEntry.setUp
    tearDown=RouteEntry.tearDown
    def setUp(self):
        self.setUpBase()
        self.env|={'BRORAY_BASE':str(self.app),'BRORAY_GLOBAL_LOCK':str(self.temp/'legacy'),
          'BRORAY_TMP_ROOT':str(self.temp),'BRORAY_TX_ASH':'/bin/ash','BRORAY_TX_APP_ROOT':str(self.app)}
        for f in ['broray-servers','broray-routes-dot']:
            shutil.copy2(ROOT/'implementation/runtime/app/bin'/f,self.app/'bin'/f)
        (self.app/'routes/bundles.json').write_text('{"bundles":["fixture"]}')
        (self.app/'config').mkdir();(self.app/'config/active-server').write_text('fixture\n')
        (self.app/'lib/routes-summary.sh').write_text('broray_routes_summary() { echo \'{"installed":true}\'; }\n')
        (self.app/'lib/routes-router-delete.sh').write_text('''
broray_routes_delete_cleanup() { :; }
broray_routes_router_delete_run() { echo DELETE >>"$BRORAY_ROOT/changed"; }
''')
        (self.app/'lib/server-service.sh').write_text('''
. "$BRORAY_ROOT/lib/operation-job.sh"
broray_server_deactivate() { echo DEACTIVATE >>"$BRORAY_ROOT/changed"; }
broray_server_deactivate_commit() { echo DEACTIVATE >>"$BRORAY_ROOT/changed"; }
''')
        # Independent DoT protocol is outside this regression's scope.
        (self.app/'bin/broray-routes-dot').write_text('#!/bin/ash\nexit 0\n')
        for p in (self.app/'bin').iterdir():p.chmod(0o700)
    def shell(self,cmd):
        return subprocess.run(['/bin/ash','-c',SETUP+cmd],env=self.env,capture_output=True,timeout=40)
    def test_uninstall_deletes_owned_bundle_under_its_existing_fence(self):
        p=self.shell('broray_lifecycle_routes_remove_all\n')
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertEqual((self.app/'changed').read_text(),'DELETE\n')
    def test_uninstall_deactivates_under_its_existing_fence(self):
        p=self.shell('broray_lifecycle_servers_deactivate\n')
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertEqual((self.app/'changed').read_text(),'DEACTIVATE\n')
    def test_rollback_exports_in_order_without_second_admission(self):
        (self.app/'lib/routes-export-build.sh').write_text('broray_routes_export_build_run() { echo BUILD >>"$BRORAY_ROOT/changed"; }\n')
        (self.app/'lib/routes-router-sync.sh').write_text('broray_routes_sync_apply() { echo APPLY >>"$BRORAY_ROOT/changed"; }\n')
        p=self.shell('broray_lifecycle_component route-export fixture\n')
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr));self.assertEqual((self.app/'changed').read_text(),'BUILD\nAPPLY\n')
    def test_failed_control_fence_prevents_mutation(self):
        p=self.shell('broray_tx_control_transition_begin() { return 1; }; broray_lifecycle_routes_remove_all\n')
        self.assertNotEqual(p.returncode,0);self.assertFalse((self.app/'changed').exists())
    def test_foreign_owner_and_wrong_action_remain_blocked(self):
        for mutation in ['printf "update\\n" >"$BRORAY_GLOBAL_LOCK/action"',
                         'printf "BAD\\n" >"$BRORAY_GLOBAL_LOCK/owner-identity.tsv"']:
            p=self.shell(mutation+'\nbroray_lifecycle_routes_remove_all\n')
            self.assertNotEqual(p.returncode,0,(p.stdout,p.stderr));self.assertFalse((self.app/'changed').exists())

if __name__=='__main__':
    Path('/opt/bin').mkdir(parents=True,exist_ok=True)
    subprocess.run(['/bin/busybox','--install','-s','/opt/bin'],check=True)
    if not Path('/opt/bin/jq').exists():Path('/opt/bin/jq').symlink_to('/usr/bin/jq')
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(SystemComponents))
    (ROOT/'docs/evidence/system-components-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'nativeOpkgFixture':True,'routerAccessed':False})+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
