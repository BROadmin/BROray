"""Real Linux supervisor/cancel path for RAM steps; all processes are fixtures."""
import ctypes
import json
import os
import subprocess
import time
import unittest
from pathlib import Path
import test_subscription_jobs as subscriptions
import test_server_jobs as servers


class StepHelpers(unittest.TestCase):
    setUp = subscriptions.SubscriptionJobs.setUp
    reap_adopted_helpers = subscriptions.SubscriptionJobs.reap_adopted_helpers
    shell = servers.ServerJobs.shell
    collect = servers.ServerJobs.collect

    def test_drained_supervisor_is_proved_once_before_registry_retirement(self):
        coordinator=self.app/'lib/operation-coordinator.sh'
        text=coordinator.read_text()
        self.assertEqual(text.count('ops_supervisor_absent()\n'),1)
        text=text.replace('ops_supervisor_absent()\n','ops_supervisor_absent_original()\n',1)
        marker='verb="${1:-}"; [ "$#" -gt 0 ] && shift'
        self.assertEqual(text.count(marker),1)
        text=text.replace(marker,'''ops_supervisor_absent() {
    echo proof >>"$BRORAY_ROOT/tmp/absence-proofs"
    ops_supervisor_absent_original "$@"
}
'''+marker,1)
        coordinator.write_text(text)
        self.shell('''
. "$BRORAY_ROOT/lib/operation-job.sh"
request="$(broray_ops_queue_submit servers:quality batch SERVER_CHECK_AUTO aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 11111111111111111111111111111111)" || exit $?
request="$(printf '%s' "$request" | jq -er .requestId)" || exit $?
broray_job_claim_step "$request" || exit $?
broray_ops_run_helper 20 -- /bin/ash -c 'printf done' || exit $?
broray_job_finish completed
''')
        self.assertEqual((self.app/'tmp/absence-proofs').read_text().splitlines(),['proof'],
                         'Drain repeats the same departed-generation proof')
        records=list((self.temp/'ram/steps').glob('*/supervisors.json'))
        self.assertEqual(len(records),1)
        self.assertEqual(json.loads(records[0].read_bytes())['supervisors'],[])
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_cancel_reaches_exact_ram_step_helper_and_preserves_foreign_process(self):
        helper = self.temp/'helper.sh'
        helper.write_text('#!/bin/ash\necho $$ >"$TEST_CHILD_PID"\necho ready >"$TEST_READY"\nexec sleep 60\n')
        helper.chmod(0o700)
        self.env['TEST_CHILD_PID'] = str(self.temp/'child-pid')
        self.env['TEST_HELPER'] = str(helper)
        script = '''
. "$BRORAY_ROOT/lib/operation-job.sh"
request="$(broray_ops_queue_submit servers:quality batch SERVER_CHECK_AUTO aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 11111111111111111111111111111111)" || exit $?
request="$(printf '%s' "$request" | jq -r .requestId)"
owned="$(broray_ops_call queue-claim "$request" 22222222222222222222222222222222 "$$")" || exit $?
BRORAY_BACKGROUND_OPERATION_ID="$(printf '%s' "$owned" | jq -r .operationId)"
BRORAY_BACKGROUND_OPERATION_TOKEN="$(printf '%s' "$owned" | jq -r .token)"
export BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN
broray_ops_call ack "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" >/dev/null || exit $?
BRORAY_JOB_ACTIVE=true
rc=0; broray_ops_run_helper 20 -- /bin/ash "$TEST_HELPER" || rc=$?
broray_job_exit "$rc" || exit 75
exit "$rc"
'''
        foreign = subprocess.Popen(['sleep', '60'])
        before = Path(f'/proc/{foreign.pid}/stat').read_text().rsplit(') ', 1)[1].split()[19]
        worker = subprocess.Popen(['/bin/ash', '-c', script], env=self.env,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            deadline = time.monotonic()+40
            while not Path(self.env['TEST_READY']).exists():
                if worker.poll() is not None:
                    self.fail((worker.returncode, *worker.communicate()))
                self.assertLess(time.monotonic(), deadline, 'step helper did not start')
                time.sleep(.05)
            records = list((self.temp/'ram/steps').glob('*/state.json'))
            self.assertEqual(len(records), 1)
            operation = json.loads(records[0].read_bytes())
            self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call cancel '+operation['operationId'])
            out, err = self.collect(worker)
            self.assertEqual(worker.returncode, 130, (out, err))
            self.assertEqual(json.loads(records[0].read_bytes())['state'], 'aborted')
            self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
            child = Path(self.env['TEST_CHILD_PID']).read_text().strip()
            self.assertFalse(Path('/proc', child).exists(), 'owned helper not drained')
            self.assertIsNone(foreign.poll())
            self.assertEqual(Path(f'/proc/{foreign.pid}/stat').read_text().rsplit(') ', 1)[1].split()[19], before)
        finally:
            if worker.poll() is None:
                worker.kill(); self.collect(worker)
            if foreign.poll() is None:
                foreign.terminate()
            foreign.wait(timeout=5)


if __name__ == '__main__':
    assert ctypes.CDLL(None).prctl(36, 1, 0, 0, 0) == 0
    unittest.main(verbosity=2, failfast=True)
