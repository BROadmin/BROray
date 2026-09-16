"""Drop real coordinator responses after durable mutation, retaining exit zero."""
import json,subprocess,unittest
from test_supervisor_integration import Integration,ROOT

SHIM='''
broray_ops_call() {
  local rc
  if [ "$1" = "$TEST_DROP_METHOD" ] && { [ "$TEST_DROP_KIND" = always ] || [ ! -e "$TEST_WORK/dropped-$1" ]; }; then
    rc=0
    "$BRORAY_OPS_GUARD" "$BRORAY_STATE_ROOT/operations.guard" /bin/ash "$BRORAY_ROOT/lib/operation-coordinator.sh" "$@" >"$TEST_WORK/reply-$1" || rc=$?
    [ "$rc" = 0 ] || { cat "$TEST_WORK/reply-$1"; return "$rc"; }
    : >"$TEST_WORK/dropped-$1"
    case "$TEST_DROP_KIND" in
      malformed) printf '{"ok":' ;;
      wrong_token) printf '{"ok":true,"operationId":"op-fake","token":"bad"}' ;;
      nonzero) return 74 ;;
    esac
    return 0
  fi
  "$BRORAY_OPS_GUARD" "$BRORAY_STATE_ROOT/operations.guard" /bin/ash "$BRORAY_ROOT/lib/operation-coordinator.sh" "$@"
}
'''

class Responses(unittest.TestCase):
    setUpFixture=Integration.setUpFixture
    tearDown=Integration.tearDown
    def setUp(self):
        Integration.setUp(self)
        for key in ['BRORAY_BACKGROUND_OPERATION_ID','BRORAY_BACKGROUND_OPERATION_TOKEN','BRORAY_BACKGROUND_LAUNCH_NONCE']:
            self.env.pop(key,None)
    def run_script(self,body,method='begin',kind='empty'):
        (self.temp/'shim.sh').write_text(SHIM)
        script=self.temp/'parent.sh';script.write_text('set -eu\n. "$BRORAY_ROOT/lib/operation-client.sh"\n. "$TEST_WORK/shim.sh"\n'+body)
        p=subprocess.run(['/bin/ash',str(script)],env={**self.env,'TEST_WORK':str(self.temp),'TEST_DROP_METHOD':method,'TEST_DROP_KIND':kind},capture_output=True,timeout=60)
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        return p
    def begin_response(self,kind):
        self.run_script('''broray_ops_begin system subscriptions:scheduler subscriptions USER cooperative
broray_ops_finish completed
''',kind=kind)
        ops=list((self.state/'operations').glob('op-*'))
        self.assertEqual(len(ops),1)
        self.assertEqual(json.loads((ops[0]/'state.json').read_text())['state'],'completed')
        self.assertFalse((self.temp/'global.lock').is_symlink())
    def handoff_response(self,method,kind):
        (self.temp/'worker.sh').write_text('''set -eu
. "$BRORAY_ROOT/lib/operation-client.sh"
. "$TEST_WORK/shim.sh"
broray_ops_accept_handoff "$TEST_NONCE"
echo yes >"$TEST_WORK/accepted"
broray_ops_finish completed
''')
        self.run_script('''broray_ops_begin system xray:update xray USER protected
TEST_NONCE=11111111111111111111111111111111; export TEST_NONCE
/bin/ash "$TEST_WORK/worker.sh" & worker=$!
broray_ops_handoff_to "$worker" "$TEST_NONCE"
[ -z "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ]
wait "$worker"
''',method,kind)
        self.assertTrue((self.temp/'accepted').exists())
        self.assertFalse((self.temp/'global.lock').is_symlink())
    def test_all_responses_lost_never_opens_work_gate(self):
        self.run_script('''rc=0
broray_ops_begin system subscriptions:scheduler subscriptions USER cooperative || rc=$?
[ "$rc" = 1 ]
[ -z "${BRORAY_BACKGROUND_OPERATION_ID:-}" ]
[ -z "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ]
''',kind='always')
        ops=list((self.state/'operations').glob('op-*'));self.assertEqual(len(ops),1)
        state=json.loads((ops[0]/'state.json').read_text())
        self.assertEqual(state['state'],'starting');self.assertFalse(state['acknowledged'])
    def test_structured_rejection_with_exit_zero_is_failure(self):
        self.run_script('''broray_ops_call() { printf '{"ok":false,"errorCode":"OPERATION_BUSY"}'; }
rc=0
broray_ops_begin system subscriptions:scheduler subscriptions USER cooperative || rc=$?
[ "$rc" = 2 ]
[ -z "${BRORAY_BACKGROUND_OPERATION_ID:-}" ]
''')
        self.assertFalse((self.state/'operations').exists())

for kind in ['empty','malformed','wrong_token','nonzero']:
    def test(self,kind=kind):self.begin_response(kind)
    setattr(Responses,'test_begin_'+kind,test)
for method in ['handoff','accept-handoff']:
    for kind in ['empty','malformed']:
        def test(self,method=method,kind=kind):self.handoff_response(method,kind)
        setattr(Responses,'test_'+method.replace('-','_')+'_'+kind,test)

if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Responses))
    (ROOT/'docs/evidence/client-responses-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'actual Linux shell owners and durable coordinator; deliberate transport response loss','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
