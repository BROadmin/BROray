"""Actual Linux workers/guard/supervisor; only domain handlers are fixtures."""
import ctypes
import json
import os
import subprocess
import time
import unittest
import uuid
from pathlib import Path
import test_subscription_jobs as support
import test_service_lifecycle as services
from test_client_responses import SHIM


class Dispatch(unittest.TestCase):
    def setUp(self):
        support.SubscriptionJobs.setUp(self)
        self.addCleanup(self.release_and_reap)
        self.children = []
        for directory in ['logs','run']:
            (self.app/directory).mkdir(exist_ok=True)
        handler = '''
broray_quality_step() {
    broray_job_checkpoint checking || return $?
    printf '%s\\n' "$BRORAY_BACKGROUND_OPERATION_ID" >>"$BRORAY_ROOT/tmp/quality-starts"
    broray_ops_run_helper 60 -- /bin/ash "$BRORAY_ROOT/tmp/hold.sh"
}
broray_auto_health_step() {
    broray_job_checkpoint checking || return $?
    echo done >"$BRORAY_ROOT/tmp/health-done"
}
broray_subscription_step() {
    broray_job_checkpoint fetching || return $?
    echo done >"$BRORAY_ROOT/tmp/subscription-done"
}
'''
        for file in ['server-check-job.sh','active-proxy-health.sh','subscription-job.sh']:
            (self.app/'lib'/file).write_text(handler)
        (self.app/'tmp/hold.sh').write_text('''#!/bin/ash
echo ready >"$BRORAY_ROOT/tmp/prepare-ready"
while [ ! -f "$BRORAY_ROOT/tmp/release" ]; do sleep .05; done
echo done >"$BRORAY_ROOT/tmp/prepare-done"
''')
        self.prefix = '. "$BRORAY_ROOT/lib/service-lifecycle.sh"; . "$BRORAY_ROOT/lib/operation-job.sh"; '

    def release_and_reap(self):
        (self.app/'tmp/release').touch()
        for child in self.children:
            if child.poll() is None:
                child.communicate(timeout=60)
        deadline = time.monotonic()+30
        while True:
            try:
                pid, _ = os.waitpid(-1, os.WNOHANG)
            except ChildProcessError:
                break
            if not pid:
                self.assertLess(time.monotonic(), deadline, 'worker still live; preserve fixture')
                time.sleep(.05)

    def shell(self, script, timeout=30):
        p = subprocess.run(['/bin/ash','-c',self.prefix+script], env=self.env,
                           capture_output=True, timeout=timeout)
        self.assertEqual(p.returncode, 0, (p.stdout,p.stderr))
        return p

    def submit(self, action='servers:quality', source='SERVER_CHECK_AUTO', target='batch'):
        nonce = uuid.uuid4().hex
        request = json.loads(self.shell('broray_ops_queue_submit '+action+' '+target+' '+source+' '+'a'*64+' '+nonce).stdout)
        return request, nonce

    def dispatch(self):
        self.shell('broray_job_dispatch_step')

    def wait_completed(self, nonce):
        end = time.monotonic()+30
        while True:
            result = json.loads(self.shell('broray_ops_queue_lookup '+nonce).stdout)
            if result['state']=='completed': return
            self.assertIn(result['state'], ['queued','running'])
            self.assertLess(time.monotonic(),end,('request did not finish',result))
            time.sleep(.1)

    def wait_file(self, name):
        end = time.monotonic()+40
        file = self.app/'tmp'/name
        while not file.exists():
            self.assertLess(time.monotonic(), end, name+' was not produced')
            time.sleep(.05)
        return file

    def test_slow_prepare_does_not_block_active_observer(self):
        _, prepare_nonce = self.submit()
        self.dispatch()
        self.wait_file('prepare-ready')
        _, health_nonce = self.submit('servers:active-health','AUTO_SWITCH','active')
        self.dispatch()
        self.wait_file('health-done')
        self.assertFalse((self.app/'tmp/release').exists())
        self.assertFalse((self.app/'tmp/prepare-done').exists())
        self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())
        self.assertEqual(len((self.app/'tmp/quality-starts').read_text().splitlines()), 1)
        (self.app/'tmp/release').touch()
        self.wait_file('prepare-done')
        self.wait_completed(health_nonce)
        self.wait_completed(prepare_nonce)
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
        self.assertFalse((self.temp/'ram/resources/active-observer').is_symlink())

    def test_sleeping_subscription_scheduler_does_not_stall_head(self):
        _, nonce = self.submit('subscriptions:refresh','SUBSCRIPTION_AUTO','sub-one')
        self.shell('broray_service_setup auto-switch || exit $?; broray_job_dispatch_step')
        self.wait_file('subscription-done')
        self.wait_completed(nonce)

    def test_completion_wakes_next_request_without_another_service_tick(self):
        handler='''
broray_quality_step() {
    broray_job_checkpoint checking || return $?
    printf '%s\\n' "$1" >>"$BRORAY_ROOT/tmp/completed-requests"
}
'''
        for file in ['server-check-job.sh','active-proxy-health.sh']:
            (self.app/'lib'/file).write_text(handler)
        first,first_nonce=self.submit(target='first')
        second,second_nonce=self.submit(target='second')
        self.shell('broray_job_dispatch_step continue')
        self.wait_completed(first_nonce)
        # No daemon tick or second explicit dispatch here. A confirmed terminal
        # worker may ask the same coordinator to select the next finite stage.
        self.wait_completed(second_nonce)
        self.assertEqual((self.app/'tmp/completed-requests').read_text().splitlines(),
                         [first['requestId'],second['requestId']])
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_claim_wakes_other_free_resource_before_current_stage_finishes(self):
        # One dispatch must fill both independent resources. The observer is
        # deliberately held before completion, so completion-only wakeup
        # cannot hide an idle prepare slot until the next service tick.
        release = self.app/'tmp/observer-release'
        os.mkfifo(release, 0o600)
        fd = os.open(release, os.O_RDWR | os.O_NONBLOCK)
        def release_observer():
            os.write(fd, b'release\n')
            os.close(fd)
        self.addCleanup(release_observer)
        (self.app/'lib/active-proxy-health.sh').write_text('''
broray_auto_health_step() {
    broray_job_checkpoint checking || return $?
    echo ready >"$BRORAY_ROOT/tmp/observer-ready"
    IFS= read -r release <"$BRORAY_ROOT/tmp/observer-release"
}
''')
        (self.app/'lib/server-check-job.sh').write_text('''
broray_quality_step() {
    broray_job_checkpoint checking || return $?
    echo done >"$BRORAY_ROOT/tmp/quality-done"
}
''')
        _, health_nonce = self.submit('servers:active-health','AUTO_SWITCH','active')
        _, quality_nonce = self.submit()
        self.shell('broray_job_dispatch_step continue')
        self.wait_file('observer-ready')
        self.wait_file('quality-done')
        self.assertTrue((self.temp/'ram/resources/active-observer').is_symlink())
        self.wait_completed(quality_nonce)
        os.write(fd, b'release\n')
        self.wait_completed(health_nonce)

    def test_concurrent_dispatch_claims_once_without_waiting_workers(self):
        for n in range(4):
            self.submit(target='batch-'+str(n))
        for _ in range(4):
            child = subprocess.Popen(['/bin/ash','-c',self.prefix+'broray_job_dispatch_step'],
                                     env=self.env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            self.children.append(child)
        for child in self.children:
            out, err = child.communicate(timeout=40)
            self.assertEqual(child.returncode, 0, (out,err))
        self.wait_file('prepare-ready')
        self.assertEqual(len((self.app/'tmp/quality-starts').read_text().splitlines()), 1)
        boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
        queue = json.loads((self.temp/'ram/queue'/boot/'state.json').read_bytes())
        self.assertEqual(sum(r['state']=='running' for r in queue['requests']), 1)
        self.assertEqual(sum(r['state']=='queued' for r in queue['requests']), 3)
        end = time.monotonic()+10
        while True:
            workers = []
            for entry in Path('/proc').iterdir():
                if not entry.name.isdigit(): continue
                try: argv=(entry/'cmdline').read_bytes().split(b'\0')
                except (FileNotFoundError,ProcessLookupError): continue
                if str(self.app/'lib/operation-worker.sh').encode() in argv:
                    workers.append(entry.name)
            if len(workers)==1: break
            self.assertLess(time.monotonic(),end,('waiting workers',workers))
            time.sleep(.05)
        (self.app/'tmp/release').touch()
        self.wait_file('prepare-done')

    def test_daemon_stop_preserves_protected_step(self):
        request, nonce = self.submit('subscriptions:refresh','SUBSCRIPTION_AUTO','protected')
        self.env['TEST_REQUEST']=request['requestId']
        for phase,next_stage in [('fetching','parse'),('parsing','apply')]:
            self.env.update(TEST_PHASE=phase,TEST_NEXT_STAGE=next_stage)
            self.shell('''
broray_job_claim_step "$TEST_REQUEST" || exit $?
broray_job_checkpoint "$TEST_PHASE" || exit $?
printf '{"schemaVersion":1}' >"$BRORAY_OPS_RAM_ROOT/requests/$TEST_REQUEST/result.json"
chmod 600 "$BRORAY_OPS_RAM_ROOT/requests/$TEST_REQUEST/result.json"
digest="$(sha256sum "$BRORAY_OPS_RAM_ROOT/requests/$TEST_REQUEST/result.json" | cut -d ' ' -f 1)"
broray_job_yield "$TEST_NEXT_STAGE" "$digest"
''',timeout=60)
        (self.app/'lib/subscription-job.sh').write_text('''
broray_subscription_step() {
    [ "$2" = apply ] || return 1
    broray_job_checkpoint committing || return $?
    echo ready >"$BRORAY_ROOT/tmp/protected-ready"
    while [ ! -f "$BRORAY_ROOT/tmp/release" ]; do sleep .05; done
}
''')
        (self.app/'bin/broray-subscription-scheduler').write_text('''
. "$BRORAY_ROOT/lib/service-lifecycle.sh"
broray_service_daemon_enter subscriptions || exit $?
broray_service_spawn_step "$TEST_REQUEST" || exit $?
while ! broray_service_stop_requested; do sleep .05; done
broray_service_daemon_exit
''')
        daemon=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-subscription-scheduler')],
            env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.children.append(daemon)
        self.wait_file('protected-ready')
        target=(self.temp/'global.lock').resolve()
        before=json.loads((target/'owner.json').read_bytes())
        stop=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-service'),'subscriptions','stop'],
            env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.children.append(stop)
        deadline=time.monotonic()+30
        while True:
            try:
                stop_out,stop_err=stop.communicate(timeout=.1)
                break
            except subprocess.TimeoutExpired:
                # Like Services.call: this test is the daemon's parent/subreaper.
                # It must reap its exited child while stop checks /proc identity.
                daemon.poll()
                self.assertLess(time.monotonic(),deadline,'service stop exceeded 30 seconds')
        self.assertEqual(stop.returncode,0,(stop_out,stop_err))
        out,err=daemon.communicate(timeout=10)
        self.assertEqual(daemon.returncode,0,(out,err))
        self.assertTrue((self.temp/'global.lock').is_symlink())
        self.assertEqual(json.loads((target/'owner.json').read_bytes()),before)
        state=json.loads((target.parent/'state.json').read_bytes())
        self.assertEqual(state['phase'],'committing')
        self.assertEqual(state['cancelability'],'protected')
        self.assertTrue(state['running'])
        self.assertFalse((target.parent/'cancel.json').exists())
        (self.app/'tmp/release').touch()
        self.wait_completed(nonce)
        self.assertFalse((self.temp/'global.lock').is_symlink())


class DispatchLease(unittest.TestCase):
    setUp = services.Services.setUp
    tearDown = services.Services.tearDown
    call = services.Services.call
    record = services.Services.record
    reap_daemon = services.Services.reap_daemon
    direct = services.Services.direct

    def test_worker_does_not_inherit_service_lease(self):
        # Isolated executable fixture; production spawn path is unchanged.
        (self.app/'lib/operation-worker.sh').write_text('''#!/bin/ash
for fd in /proc/$$/fd/*; do readlink "$fd" || :; done >"$BRORAY_ROOT/tmp/worker-fds"
echo ready >"$BRORAY_ROOT/tmp/worker-ready"
while [ ! -f "$BRORAY_ROOT/tmp/worker-release" ]; do sleep .05; done
''')
        (self.app/'bin/broray-subscription-scheduler').write_text('''#!/bin/ash
. "$BRORAY_ROOT/lib/service-lifecycle.sh"
broray_service_daemon_enter subscriptions || exit $?
broray_service_spawn_step q-11111111111111111111111111111111 || exit $?
broray_service_daemon_exit
''')
        parent = self.direct()
        try:
            out, err = parent.communicate(timeout=20)
            self.assertEqual(parent.returncode, 0, (out,err))
            deadline = time.monotonic()+10
            while not (self.app/'tmp/worker-ready').exists():
                self.assertLess(time.monotonic(),deadline,'worker not launched')
                time.sleep(.05)
            self.assertNotIn(str(self.svc/'lifetime.guard'), (self.app/'tmp/worker-fds').read_text())
            lease = subprocess.run([self.env['BRORAY_OPS_GUARD'],str(self.svc/'lifetime.guard'),'/bin/true'],
                                   env=self.env,capture_output=True,timeout=8)
            self.assertEqual(lease.returncode, 0, (lease.stdout,lease.stderr))
        finally:
            (self.app/'tmp/worker-release').touch()


class DispatchResponses(Dispatch):
    # Reuse the isolated app, not the dispatch cases (loaded explicitly below).
    def test_lost_claim_ack_and_yield_replies_are_idempotent(self):
        (self.temp/'shim.sh').write_text(SHIM)
        self.env['TEST_WORK'] = str(self.temp)
        for method in ['queue-claim','ack','queue-yield']:
            with self.subTest(method=method):
                request, nonce = self.submit('subscriptions:refresh','SUBSCRIPTION_AUTO',method)
                self.env.update(TEST_DROP_METHOD=method,TEST_DROP_KIND='empty',TEST_REQUEST=request['requestId'])
                self.shell('''
. "$TEST_WORK/shim.sh"
broray_job_claim_step "$TEST_REQUEST" || exit $?
broray_job_checkpoint fetching || exit $?
mkdir -p "$BRORAY_OPS_RAM_ROOT/requests/$TEST_REQUEST" || exit $?
chmod 700 "$BRORAY_OPS_RAM_ROOT/requests/$TEST_REQUEST"
printf '{"schemaVersion":1}' >"$BRORAY_OPS_RAM_ROOT/requests/$TEST_REQUEST/result.json"
chmod 600 "$BRORAY_OPS_RAM_ROOT/requests/$TEST_REQUEST/result.json"
digest="$(sha256sum "$BRORAY_OPS_RAM_ROOT/requests/$TEST_REQUEST/result.json" | cut -d ' ' -f 1)"
broray_job_yield parse "$digest" || exit $?
[ "$BRORAY_JOB_ACTIVE" = false ] || exit 1
[ -z "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ] || exit 1
''', timeout=60)
                result=json.loads(self.shell('broray_ops_queue_lookup '+nonce).stdout)
                self.assertEqual(result['state'],'queued')
                boot=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
                state=json.loads((self.temp/'ram/queue'/boot/'state.json').read_bytes())
                entry=next(r for r in state['requests'] if r['requestId']==request['requestId'])
                self.assertEqual(entry['stage'],'parse')
                self.assertNotIn('operationId',entry)
                self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
                self.shell('broray_ops_queue_cancel '+request['requestId'])
        self.assertEqual(len(list((self.temp/'ram/steps').glob('*/owner.json'))),3)


def load_tests(loader, tests, pattern):
    return unittest.TestSuite([
        loader.loadTestsFromTestCase(Dispatch),
        loader.loadTestsFromTestCase(DispatchLease),
        DispatchResponses('test_lost_claim_ack_and_yield_replies_are_idempotent'),
    ])


if __name__ == '__main__':
    assert ctypes.CDLL(None).prctl(36, 1, 0, 0, 0) == 0
    unittest.main(verbosity=2, failfast=True)
