import json
from test_global_admission_publication import GlobalAdmissionPublication, APP
from test_preflight_admission import Admission

class AdmissionRecovery(GlobalAdmissionPublication):
    def admission(self):
        code=(APP/'share/updater-platform/opt/libexec/broray-updater/broray-updater.sh').read_text()
        start=code.index('global_operation_lock_classify()\n')
        # Include the new helper when present; baseline remains executable.
        if 'global_operation_recover_admission()\n' in code:
            start=code.index('global_operation_recover_admission()\n')
        return ('APP_ROOT="$BRORAY_ROOT"\nOPERATION_ROOT="$BRORAY_STATE_ROOT/operations"\n'
            'STATE_ROOT="$TEST_ROOT/updater"\n'
            'LEGACY_GLOBAL_OPERATION_LOCK="$TEST_ROOT/legacy.lock"\n'
            'GLOBAL_OPERATION_LOCK="$BRORAY_ROUTES_API_LOCK"\n'
            +code[start:code.index('\nadmission_hook_call()\n')]
            +'\nroutes_resumable_pending() { return 1; }\nconflicting_operation_admission_clear\n')

    def test_stale_cooperative_admission_recovers(self):
        self.run_shell('. "$BRORAY_ROOT/lib/operation-client.sh"\nbroray_ops_begin system xray:install xray USER cooperative\n')
        self.run_shell(self.admission())
        self.assertFalse(self.lock.is_symlink())
        states=list((self.state/'operations').glob('op-*/state.json'))
        self.assertEqual(json.loads(states[0].read_bytes())['state'],'recovered')
        self.assertFalse((self.state/'background-automation.json').exists())

    def test_subshell_handoff_accept_and_finish(self):
        self.run_shell('''. "$BRORAY_ROOT/lib/operation-client.sh"
(
 broray_ops_begin system xray:install xray USER cooperative || exit 91
 broray_ops_tick working || exit 92
 (
   IFS=' ' read -r worker rest </proc/self/stat
   printf '%s' "$worker" >"$TEST_ROOT/worker"
   while [ ! -f "$TEST_ROOT/go" ]; do sleep 1; done
   broray_ops_accept_handoff aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa || exit 95
   broray_ops_finish completed || exit 96
 ) & child=$!
 trap 'kill "$child" 2>/dev/null; wait "$child" 2>/dev/null || true' EXIT
 while [ ! -s "$TEST_ROOT/worker" ]; do sleep 1; done
 worker=$(cat "$TEST_ROOT/worker")
 broray_ops_handoff_to "$worker" aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa || exit 93
 touch "$TEST_ROOT/go"
 wait "$child" || exit 94
 trap - EXIT
)
''')
        self.assertFalse(self.lock.is_symlink())

    def test_live_owner_preserved(self):
        self.run_shell('. "$BRORAY_ROOT/lib/operation-client.sh"\nbroray_ops_begin system xray:install xray USER cooperative || exit 91\n'
            +'before=$(sha256sum "$BRORAY_ROUTES_API_LOCK/owner.json")\n'
            +self.admission()+'''rc=$?
test "$rc" != 0 || exit 92
test "$before" = "$(sha256sum "$BRORAY_ROUTES_API_LOCK/owner.json")" || exit 93
broray_ops_finish completed
''')

    def test_stale_protected_preserved(self):
        self.run_shell('. "$BRORAY_ROOT/lib/operation-client.sh"\nbroray_ops_begin system keenetic:web-access-enable keenetic USER protected\n')
        before={str(p):p.read_bytes() for p in (self.state/'operations').rglob('*') if p.is_file()}
        self.run_shell(self.admission(),1)
        self.assertTrue(self.lock.is_symlink())
        self.assertEqual(before,{str(p):p.read_bytes() for p in (self.state/'operations').rglob('*') if p.is_file()})

    def test_corrupt_owner_preserved(self):
        self.run_shell('. "$BRORAY_ROOT/lib/operation-client.sh"\nbroray_ops_begin system xray:install xray USER cooperative\n')
        (self.lock/'owner.json').write_bytes(b'{broken')
        self.run_shell(self.admission(),1)
        self.assertEqual((self.lock/'owner.json').read_bytes(),b'{broken')

    def test_legacy_partial_preserved(self):
        self.lock.mkdir();(self.lock/'action').touch();(self.lock/'bundle').touch()
        # Classifier uses these helpers only for legacy directory evidence.
        script='proc_identity_status() { return 1; }\n'+self.admission()
        self.run_shell(script,1)
        self.assertEqual(sorted(p.name for p in self.lock.iterdir()),['action','bundle'])

    def test_foreign_symlink_preserved(self):
        target=self.root/'foreign';target.mkdir();(target/'sentinel').write_bytes(b'unchanged')
        self.lock.symlink_to(target)
        self.run_shell(self.admission(),1)
        self.assertEqual(self.lock.readlink(),target)
        self.assertEqual((target/'sentinel').read_bytes(),b'unchanged')

    def test_new_owner_after_recovery_is_not_admitted(self):
        target=self.root/'foreign';target.mkdir();self.lock.symlink_to(target)
        script=self.admission()
        # Simulates a new owner winning after the coordinator releases its
        # guard. Successful recovery alone must not grant admission.
        script=script.rsplit('conflicting_operation_admission_clear\n',1)[0]
        script+='global_operation_recover_admission() { return 0; }\nconflicting_operation_admission_clear\n'
        self.run_shell(script,1)
        self.assertTrue(self.lock.is_symlink())

class PreflightShell(Admission):
    def test_subshell_preflight_owner_and_ack(self):
        self.ok('''(
broray_ops_preflight_admit "$TEST_SHA" || exit 91
IFS=' ' read -r actual rest </proc/self/stat
jq -e --argjson pid "$actual" '.owner.pid==$pid' "$BRORAY_ROUTES_API_LOCK/owner.json" || exit 92
broray_ops_finish completed || exit 93
)
''')
        self.assertFalse(self.lock.is_symlink())
