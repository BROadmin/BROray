"""Actual persistent service entrypoints must enqueue finite workers."""
import ctypes
import hashlib
import json
import os
import subprocess
import time
import unittest
from pathlib import Path
from test_subscription_jobs import SubscriptionJobs
from test_server_jobs import ServerJobs
from test_auto_switch_jobs import AutoSwitchJobs
from test_auto_switch_queue import AutoSwitchQueue


class Loops(unittest.TestCase):
    setUp=SubscriptionJobs.setUp
    shell=ServerJobs.shell
    collect=ServerJobs.collect
    reap_adopted_helpers=ServerJobs.reap_adopted_helpers
    record=SubscriptionJobs.record

    def check_loop(self, service, executable, expected_action, marker):
        boot=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
        queue=self.temp/'ram/queue'/boot/'state.json'
        process=subprocess.Popen(['/bin/ash',str(self.app/'bin'/executable)],
            env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            deadline=time.monotonic()+40
            while not queue.exists() and not marker.exists():
                self.assertIsNone(process.poll(),process.communicate() if process.poll() is not None else None)
                self.assertLess(time.monotonic(),deadline,'service did not reach admission')
                time.sleep(.05)
            self.assertTrue(queue.exists(),'Persistent service still invokes the old monolithic job')
            rows=json.loads(queue.read_bytes())['requests']
            self.assertEqual([r['action'] for r in rows],[expected_action])
            self.assertIn(rows[0]['state'],['queued','running'])
            self.assertFalse(marker.exists(),'Legacy handler was invoked by the persistent service')
        finally:
            # Cancel/pause by the real coordinator. Service stop is bound to
            # its generation; no PID-only or name-based signal is used here.
            self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call stop-background')
            stopper=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-service'),service,'stop'],
                env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
            deadline=time.monotonic()+30
            while True:
                process.poll()
                identity=self.state/'services'/service/'identity.json'
                if identity.exists():
                    pid=json.loads(identity.read_bytes()).get('owner',{}).get('pid')
                    if pid and pid!=process.pid:
                        try: os.waitpid(pid,os.WNOHANG)
                        except ChildProcessError: pass
                self.reap_adopted_helpers()
                try: out,err=stopper.communicate(timeout=.1);break
                except subprocess.TimeoutExpired:
                    self.assertLess(time.monotonic(),deadline,'generation stop did not complete')
            self.assertEqual(stopper.returncode,0,(out,err))
            out,err=process.communicate(timeout=10)
            self.assertEqual(process.returncode,0,(out,err))
            deadline=time.monotonic()+40
            while True:
                self.reap_adopted_helpers()
                try:
                    pid,_=os.waitpid(-1,os.WNOHANG)
                except ChildProcessError: break
                if not pid:
                    self.assertLess(time.monotonic(),deadline,'finite worker did not drain')
                    time.sleep(.05)

    def test_subscription_daemon_uses_queue(self):
        self.record()
        self.env['TEST_MODE']='wait'
        marker=self.app/'tmp/legacy-subscription-called'
        # The old long-lived domain handler is a marker, not the new queue
        # producer/helper. This isolates which path the real daemon selects.
        with (self.app/'lib/subscription-service.sh').open('a') as f:
            f.write('\nbroray_subscription_scheduler_once() { echo old >"$BRORAY_ROOT/tmp/legacy-subscription-called"; }\n')
        self.check_loop('subscriptions','broray-subscription-scheduler','subscriptions:refresh',marker)


class AutoLoop(unittest.TestCase):
    setUp=AutoSwitchJobs.setUp
    set_config=AutoSwitchJobs.set_config
    shell=ServerJobs.shell
    collect=ServerJobs.collect
    reap_adopted_helpers=ServerJobs.reap_adopted_helpers
    check_loop=Loops.check_loop

    def test_auto_switch_daemon_uses_queue(self):
        self.set_config(qualityRefreshEnabled=True)
        self.env['TEST_MODE']='wait'
        marker=self.app/'tmp/legacy-auto-called'
        # Old run_cycle calls the foreground job launcher; a marker makes
        # this wiring regression deterministic without running the old scan.
        with (self.app/'lib/service-lifecycle.sh').open('a') as f:
            f.write('\nbroray_service_run_job() { echo old >"$BRORAY_ROOT/tmp/legacy-auto-called"; }\n')
        self.check_loop('auto-switch','broray-server-auto-switch','servers:quality',marker)


class SnapshotCost(unittest.TestCase):
    setUp=AutoSwitchJobs.setUp
    shell=ServerJobs.shell
    collect=ServerJobs.collect
    reap_adopted_helpers=ServerJobs.reap_adopted_helpers

    def test_snapshot_batches_hashes_and_preserves_exact_rows(self):
        template=json.loads((self.app/'servers'/f'{self.server}.json').read_bytes())
        for index in range(30):
            node=dict(template,id=f'subscription-batch-{index:04d}',name=f'Batch {index}')
            path=self.app/'servers'/f'{node["id"]}.json'
            path.write_text(json.dumps(node));path.chmod(0o600)
        expected=sorted([dict(id=p.stem,sha256=hashlib.sha256(p.read_bytes()).hexdigest())
                         for p in (self.app/'servers').glob('*.json')],key=lambda x:x['id'])
        sha=self.app/'bin/sha256sum';native=sha.readlink();sha.unlink()
        sha.write_text('#!/bin/ash\necho call >>"$BRORAY_ROOT/tmp/hash-calls"\nexec "'+str(native)+'" "$@"\n')
        sha.chmod(0o700)
        started=time.monotonic()
        result=self.shell('. "$BRORAY_ROOT/lib/server-service.sh"; broray_quality_snapshot all')
        calls=(self.app/'tmp/hash-calls').read_text().splitlines()
        print('SNAPSHOT_COST',json.dumps(dict(nodes=31,hashProcesses=len(calls),seconds=time.monotonic()-started)),flush=True)
        self.assertEqual(json.loads(result.stdout),expected)
        self.assertEqual(len(calls),1,'One hash process per node delays every queue stage')


class AdmissionStops(unittest.TestCase):
    setUp=SubscriptionJobs.setUp
    shell=ServerJobs.shell
    collect=ServerJobs.collect
    reap_adopted_helpers=ServerJobs.reap_adopted_helpers

    def guard_fixture(self,first_result):
        guard=self.temp/'admission-guard'
        guard.write_text('''#!/bin/ash
echo call >>"$BRORAY_ROOT/tmp/guard-calls"
if [ ! -f "$BRORAY_ROOT/tmp/stopping" ]; then
  touch "$BRORAY_ROOT/tmp/stopping"
  '''+first_result+'''
fi
printf '{"ok":true,"requestId":"accepted"}\n'
''')
        guard.chmod(0o700)
        self.env['BRORAY_OPS_GUARD']=str(guard)

    def call(self,expected=0,bound=True):
        self.env['BRORAY_SERVICE_GENERATION']='a'*32 if bound else ''
        return self.shell('''. "$BRORAY_ROOT/lib/operation-client.sh"
broray_service_stop_requested() { [ -f "$BRORAY_ROOT/tmp/stopping" ]; }
broray_ops_call queue-next
''',expected=expected)

    def test_bound_stop_ends_empty_guard_wait_without_second_admission(self):
        self.guard_fixture('exit 75')
        reply=self.call(expected=2)
        self.assertEqual(json.loads(reply.stdout)['errorCode'],'SERVICE_STOP_REQUESTED')
        self.assertEqual((self.app/'tmp/guard-calls').read_text(),'call\n')

    def test_stop_does_not_discard_already_executed_response(self):
        self.guard_fixture('printf \'{"ok":true,"requestId":"first"}\\n\'; exit 0')
        self.assertEqual(json.loads(self.call().stdout)['requestId'],'first')
        self.assertEqual((self.app/'tmp/guard-calls').read_text(),'call\n')

    def test_client_without_service_generation_keeps_bounded_retry(self):
        self.guard_fixture('exit 75')
        self.assertEqual(json.loads(self.call(bound=False).stdout)['requestId'],'accepted')
        self.assertEqual((self.app/'tmp/guard-calls').read_text(),'call\ncall\n')

    def test_busy_maintenance_yields_guard_without_retrying_admission(self):
        self.guard_fixture('exit 75')
        self.env['BRORAY_SERVICE_GENERATION']='a'*32
        reply=self.shell('''. "$BRORAY_ROOT/lib/operation-client.sh"
broray_service_stop_requested() { return 1; }
broray_ops_call queue-next
''',expected=77)
        self.assertEqual(json.loads(reply.stdout)['errorCode'],'QUEUE_ADMISSION_DEFERRED')
        self.assertEqual((self.app/'tmp/guard-calls').read_text(),'call\n')

    def test_busy_urgent_health_admission_keeps_bounded_retry(self):
        self.guard_fixture('exit 75')
        self.env['BRORAY_SERVICE_GENERATION']='a'*32
        reply=self.shell('''. "$BRORAY_ROOT/lib/operation-client.sh"
broray_service_stop_requested() { return 1; }
broray_ops_call queue-submit servers:active-health active AUTO_SWITCH context nonce
''')
        self.assertEqual(json.loads(reply.stdout)['requestId'],'accepted')
        self.assertEqual((self.app/'tmp/guard-calls').read_text(),'call\ncall\n')


class ConcurrentLoops(unittest.TestCase):
    setUp=AutoSwitchJobs.setUp
    set_config=AutoSwitchJobs.set_config
    active_runtime_fixture=AutoSwitchJobs.active_runtime_fixture
    context=AutoSwitchQueue.context
    shell=ServerJobs.shell
    collect=ServerJobs.collect
    reap_adopted_helpers=ServerJobs.reap_adopted_helpers
    record=SubscriptionJobs.record

    def evidence(self, stage):
        # Read-only failure evidence from this isolated fixture, before cleanup.
        print('QUEUE_LOOP_EVIDENCE',stage,flush=True)
        for root in [self.temp/'ram',self.state/'services',self.app/'logs',self.app/'tmp']:
            for path in sorted(root.rglob('*')):
                if path.is_file() and not path.is_symlink() and path.stat().st_size<32768:
                    print(str(path.relative_to(self.temp)),path.read_text(errors='replace'),flush=True)
        print(subprocess.run(['ps','-ef'],capture_output=True,text=True).stdout,flush=True)

    def stop_daemon(self,service,process):
        stopper=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-service'),service,'stop'],
            env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        deadline=time.monotonic()+30
        while True:
            process.poll()
            identity=self.state/'services'/service/'identity.json'
            if identity.exists():
                pid=json.loads(identity.read_bytes()).get('owner',{}).get('pid')
                if pid and pid!=process.pid:
                    try: os.waitpid(pid,os.WNOHANG)
                    except ChildProcessError: pass
            self.reap_adopted_helpers()
            try: out,err=stopper.communicate(timeout=.1);break
            except subprocess.TimeoutExpired:
                self.assertLess(time.monotonic(),deadline,'generation stop did not complete')
        self.assertEqual(stopper.returncode,0,(out,err))
        out,err=process.communicate(timeout=10)
        self.assertEqual(process.returncode,0,(out,err))

    def test_real_services_observe_active_proxy_during_subscription_fetch(self):
        self.set_config(enabled=True,qualityRefreshEnabled=True)
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.env.update(TEST_ACTIVE_ID=self.server,TEST_ACTIVE_MODE='working')
        self.active_runtime_fixture()
        path=self.record();before=path.read_bytes();context=self.context()
        # Model a stalled socket with a blocking read, without synthesizing
        # thousands of fork/exec events in the supervised transport fixture.
        release=self.temp/'transport-release'
        os.mkfifo(release,0o600)
        release_fd=os.open(release,os.O_RDWR|os.O_NONBLOCK)
        self.addCleanup(os.close,release_fd)
        self.env['TEST_RELEASE']=str(release)
        curl=self.app/'bin/curl';original=curl.read_text()
        curl.write_text('''#!/bin/ash
case "$*" in
 *--dump-header*)
  echo ready >"$TEST_READY"
  IFS= read -r release <"$TEST_RELEASE"
  exit 28 ;;
esac
'''+original.removeprefix('#!/bin/ash\n'))
        children=[]
        try:
            sub=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-subscription-scheduler')],
                env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
            children.append(('subscriptions',sub))
            deadline=time.monotonic()+40
            while not (self.temp/'transport-ready').exists():
                self.assertIsNone(sub.poll())
                self.assertLess(time.monotonic(),deadline,'subscription fetch did not start')
                time.sleep(.05)
            auto=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-server-auto-switch')],
                env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
            children.append(('auto-switch',auto))
            cache=self.app/'run/server-auto-switch-state.json'
            deadline=time.monotonic()+60
            while True:
                self.assertIsNone(auto.poll())
                state=json.loads(cache.read_bytes()) if cache.exists() else {}
                if (state.get('activeHealth') or {}).get('status')=='healthy': break
                self.assertLess(time.monotonic(),deadline,('real health did not complete',state))
                time.sleep(.05)
            self.assertEqual(state['activeHealth']['context'],context)
            self.assertFalse((self.app/'tmp/release-fetch').exists())
            owner=(self.temp/'ram/resources/background-prepare').resolve().parent
            self.assertEqual(json.loads((owner/'state.json').read_bytes())['operation'],'subscriptions:refresh')
            self.assertEqual(path.read_bytes(),before)
            self.assertEqual(self.context(),context)
        except BaseException:
            self.evidence('before-stop')
            raise
        finally:
            self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call stop-background')
            try:
                for service,process in reversed(children): self.stop_daemon(service,process)
            except BaseException:
                self.evidence('stop-unconfirmed')
                raise
            deadline=time.monotonic()+60
            while any((self.temp/'ram/resources'/name).is_symlink() for name in ['background-prepare','active-observer']):
                self.reap_adopted_helpers()
                self.assertLess(time.monotonic(),deadline,'owned queue helpers did not drain')
                time.sleep(.05)
            (self.app/'tmp/release-fetch').touch()
        self.assertEqual(path.read_bytes(),before)
        self.assertEqual(self.context(),context,'Background cancellation changed Xray identity')


class ConcurrentFailover(unittest.TestCase):
    setUp=ConcurrentLoops.setUp
    set_config=ConcurrentLoops.set_config
    active_runtime_fixture=ConcurrentLoops.active_runtime_fixture
    context=ConcurrentLoops.context
    shell=ConcurrentLoops.shell
    collect=ConcurrentLoops.collect
    reap_adopted_helpers=ConcurrentLoops.reap_adopted_helpers
    stop_daemon=ConcurrentLoops.stop_daemon
    evidence=ConcurrentLoops.evidence
    failover_init_fixture=AutoSwitchQueue.failover_init_fixture

    def test_due_quality_batch_cannot_starve_live_service_failover(self):
        # Keep worker diagnostics in this isolated fixture instead of the
        # daemon's normal /dev/null sink. No production logging policy change.
        lifecycle=self.app/'lib/service-lifecycle.sh'
        text=lifecycle.read_text()
        sink='} </dev/null >/dev/null 2>&1 &'
        self.assertEqual(text.count(sink),1)
        lifecycle.write_text(text.replace(sink,
            '} </dev/null >>"$BRORAY_ROOT/tmp/worker-$request.log" 2>&1 &',1))
        worker=self.app/'lib/operation-worker.sh'
        worker.write_text(worker.read_text().replace('WORK_RC=$?\n',
            'WORK_RC=$?\nprintf "WORKER_RESULT %s %s %s\\n" "$WORK_ACTION" "$WORK_STAGE" "$WORK_RC" >&2\n',1))
        # Profile the actual coordinator client without changing its response,
        # owner variables, timeout or retry policy. Only this fixture copy.
        client=self.app/'lib/operation-client.sh'
        client.write_text(client.read_text().replace('broray_ops_call()\n',
            'broray_ops_call_profiled()\n',1)+'''
broray_ops_call() {
    local profile_start profile_end profile_rest profile_rc
    IFS=' ' read -r profile_start profile_rest </proc/uptime
    profile_rc=0
    broray_ops_call_profiled "$@" || profile_rc=$?
    IFS=' ' read -r profile_end profile_rest </proc/uptime
    printf '%s %s %s %s %s\\n' "$profile_start" "$profile_end" "$$" "$1" "$profile_rc" >>"$BRORAY_ROOT/tmp/coordinator-timings"
    return "$profile_rc"
}
''')
        coordinator=self.app/'lib/operation-coordinator.sh'
        coordinator.write_text(coordinator.read_text().replace('umask 077\n','''umask 077
IFS=' ' read -r profile_begin profile_unused </proc/uptime
profile_command="$1"
trap 'profile_status="$?"; IFS=" " read -r profile_finish profile_unused </proc/uptime; printf "%s %s %s %s %s\\n" "$profile_begin" "$profile_finish" "$$" "$profile_command" "$profile_status" >>"$BRORAY_ROOT/tmp/guard-held-timings"' EXIT
''',1))
        self.set_config(enabled=True,qualityRefreshEnabled=True,failureThreshold=1,
                        selectionRule='preferred',preferredServerId='subscription-second-0000')
        self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$(cat "$TEST_PAYLOAD")" subscription second 0')
        template=json.loads((self.app/'servers'/f'{self.server}.json').read_bytes())
        for index in range(29):
            node=dict(template,id=f'subscription-batch-{index:04d}',name=f'Batch {index}')
            path=self.app/'servers'/f'{node["id"]}.json'
            path.write_text(json.dumps(node));path.chmod(0o600)
        self.assertEqual(len(list((self.app/'servers').glob('*.json'))),31)
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.env.update(TEST_ACTIVE_ID=self.server,TEST_ACTIVE_MODE='timeout')
        self.active_runtime_fixture()
        self.failover_init_fixture()
        before=self.context()
        process=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-server-auto-switch')],
            env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        started=time.monotonic();previous=None
        try:
            cache=self.app/'run/server-auto-switch-state.json'
            # Original c19 remained on the failed active node after 306s with
            # the same 31-node workload. The fixture runtime lives 300s.
            deadline=started+300
            while True:
                # This runner is the fixture subreaper: perform init's duty,
                # as collect() does for foreground tests. Never signal here.
                self.reap_adopted_helpers()
                self.assertIsNone(process.poll())
                state=json.loads(cache.read_bytes()) if cache.exists() else {}
                progress=(state.get('status'),(state.get('failover') or {}).get('status'),
                          (state.get('qualityRefresh') or {}).get('checkedCount'))
                if progress!=previous:
                    print('FAILOVER_PROGRESS',round(time.monotonic()-started,3),progress,flush=True)
                    previous=progress
                if state.get('status')=='switched': break
                self.assertLess(time.monotonic(),deadline,('failover starved',state))
                time.sleep(.05)
            self.assertEqual(state['activeServerId'],'subscription-second-0000')
            self.assertEqual((self.app/'config/active-server').read_text().strip(),'subscription-second-0000')
            self.assertEqual(state['lastSwitchFrom'],self.server)
            self.assertEqual(state['lastSwitchTo'],'subscription-second-0000')
            self.assertEqual((self.app/'restarts').read_text(),'restart\n')
            self.assertLess((state.get('qualityRefresh') or {}).get('checkedCount',0),31)
            self.assertEqual(state['failover']['checkedCount'],1,'Preferred candidate must avoid scanning the full batch')
            self.assertIsNone(state['activeHealth'],'Old failed-proxy observation survived activation')
            print('FAILOVER_COMPLETED_SECONDS',round(time.monotonic()-started,3),flush=True)
        except BaseException:
            self.evidence('failover-failure')
            raise
        finally:
            for name in ['coordinator-timings','guard-held-timings']:
                timing=self.app/'tmp'/name
                if timing.exists():
                    rows=[]
                    for row in timing.read_text().splitlines():
                        start,end,pid,command,rc=row.split()
                        rows.append(dict(seconds=round(float(end)-float(start),3),command=command,rc=int(rc),pid=int(pid)))
                    print('COORDINATOR_SLOWEST',name,json.dumps(sorted(rows,key=lambda x:x['seconds'],reverse=True)[:20]),flush=True)
            self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call stop-background')
            self.stop_daemon('auto-switch',process)
            deadline=time.monotonic()+60
            while any((self.temp/'ram/resources'/name).is_symlink() for name in ['background-prepare','active-observer']):
                self.reap_adopted_helpers()
                self.assertLess(time.monotonic(),deadline,'failover test workers did not drain')
                time.sleep(.05)


class CombinedAutomation(unittest.TestCase):
    setUp=AutoSwitchJobs.setUp
    set_config=AutoSwitchJobs.set_config
    shell=ServerJobs.shell
    collect=ServerJobs.collect
    reap_adopted_helpers=ServerJobs.reap_adopted_helpers
    record=SubscriptionJobs.record

    def test_quality_subscriptions_dot_all_progress_through_real_handlers(self):
        self.set_config(enabled=False,qualityRefreshEnabled=True)
        template=json.loads((self.app/'servers'/f'{self.server}.json').read_bytes())
        for index in range(5):
            node=dict(template,id=f'subscription-batch-{index:04d}',name=f'Batch {index}')
            path=self.app/'servers'/f'{node["id"]}.json'
            path.write_text(json.dumps(node));path.chmod(0o600)
        record=self.record()
        dot=self.app/'routes/dot';dot.mkdir(parents=True,exist_ok=True)
        config={'schemaVersion':3,'requestedIds':['google-primary'],
            'selectedIds':['google-primary'],'effectiveIds':[],'managed':[],'quarantinedReceipts':[]}
        (dot/'config.json').write_text(json.dumps(config))
        (dot/'state.json').write_text(json.dumps({'schemaVersion':1,'tests':[],
            'lastError':'KEEP','lastOperation':{'type':'apply','success':False}}))
        (dot/'auto-check.json').write_text('{"schemaVersion":1,"enabled":true}')
        for p in dot.glob('*.json'):p.chmod(0o600)
        dot_before=(dot/'config.json').read_bytes()
        openssl=self.app/'bin/openssl'
        openssl.write_text('#!/bin/ash\necho probe >>"$BRORAY_ROOT/tls-calls"\nexit 0\n')
        openssl.chmod(0o755);self.env['BRORAY_DOT_OPENSSL']=str(openssl)
        curl=self.app/'bin/curl';original=curl.read_text()
        curl.write_text('''#!/bin/ash
case "$*" in *--dump-header*)
  echo fetch >>"$BRORAY_ROOT/fetch-calls"
  while [ "$#" -gt 0 ]; do
    case "$1" in --dump-header) headers="$2"; shift ;; --output) body="$2"; shift ;; esac
    shift
  done
  printf 'HTTP/1.1 200 OK\\r\\nContent-Type: text/plain\\r\\n\\r\\n' >"$headers"
  cat "$TEST_PAYLOAD" >"$body"
  printf 200
  exit 0 ;;
esac
'''+original.removeprefix('#!/bin/ash\n'))
        self.shell('''. "$BRORAY_ROOT/lib/server-service.sh"
. "$BRORAY_ROOT/lib/active-proxy-health.sh"
broray_auto_enqueue_due >"$BRORAY_ROOT/tmp/quality-admission"
. "$BRORAY_ROOT/lib/subscription-service.sh"
broray_subscription_enqueue_due >"$BRORAY_ROOT/tmp/scheduled-admission"
''')
        quality=json.loads((self.app/'tmp/quality-admission').read_bytes())['quality']
        scheduled=json.loads((self.app/'tmp/scheduled-admission').read_bytes())
        self.assertEqual(len(scheduled['subscriptionRequests']),1,scheduled)
        self.assertIsNotNone(scheduled['dotRequest'],scheduled)
        requests=[quality,scheduled['subscriptionRequests'][0],scheduled['dotRequest']]
        self.assertEqual([r['priority'] for r in requests],[3,4,5])
        sequence=[];peak=0;started=time.monotonic()
        for _ in range(11):
            selected=json.loads(self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_queue_next').stdout)
            if selected['requestId'] is None:break
            sequence.append(selected['priority'])
            process=subprocess.Popen(['/bin/ash',str(self.app/'lib/operation-worker.sh'),
                '--request',selected['requestId']],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
            out,err=self.collect(process,90)
            self.assertEqual(process.returncode,0,(selected,out,err))
            self.assertFalse((self.temp/'global.lock').is_symlink())
            self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
            peak=max(peak,sum(p.stat().st_size for p in (self.temp/'ram').rglob('*')
                if p.is_file() and not p.is_symlink()))
        self.assertEqual(sequence[:6],[3,3,3,4,4,5])
        self.assertEqual(sequence,[3,3,3,4,4,5,3,3,3,4])
        for request in requests:
            receipt=json.loads(self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_queue_lookup '+
                request['requestId'][2:]).stdout)
            self.assertEqual(receipt['state'],'completed')
        progress=json.loads((self.app/'run/server-auto-switch-state.json').read_bytes())['qualityRefresh']
        self.assertEqual(progress['checkedCount'],6);self.assertEqual(progress['status'],'success')
        self.assertEqual((self.app/'fetch-calls').read_text(),'fetch\n')
        self.assertEqual(json.loads(record.read_bytes())['lastUpdateStatus'],'success')
        self.assertEqual((self.app/'tls-calls').read_text(),'probe\n')
        self.assertEqual(json.loads((dot/'state.json').read_bytes())['autoCheck']['status'],'success')
        self.assertEqual((dot/'config.json').read_bytes(),dot_before)
        self.assertFalse((self.app/'config/active-server').exists(),'Automation enabled a manually stopped VPN')
        self.assertFalse((self.app/'restarts').exists())
        print('COMBINED_AUTOMATION_RESULT',json.dumps(dict(sequence=sequence,
            seconds=round(time.monotonic()-started,3),peakRamPayloadBytes=peak)),flush=True)


if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    unittest.main(verbosity=2,failfast=True)
