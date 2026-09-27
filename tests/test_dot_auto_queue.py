"""One selected DoT endpoint per native-owned stage; fixture TLS only."""
import ctypes
import json
import subprocess
import time
import unittest
import uuid
from pathlib import Path
from test_dot_auto_jobs import Jobs
from test_server_jobs import ServerJobs
from test_operation_resources import OperationResources


class DotQueue(unittest.TestCase):
    setUp=Jobs.setUp
    shell=ServerJobs.shell
    collect=ServerJobs.collect
    reap_adopted_helpers=ServerJobs.reap_adopted_helpers

    def submit(self):
        for p in self.dot.glob('*.json'): p.chmod(0o600)
        self.nonce=uuid.uuid4().hex
        reply=self.shell(''' . "$BRORAY_ROOT/lib/dot-auto.sh"
. "$BRORAY_ROOT/lib/operation-client.sh"
context="$(broray_dot_auto_context)" || exit $?
broray_ops_queue_submit dot:auto-check selected SCHEDULER "$context" '''+self.nonce)
        self.request=json.loads(reply.stdout)

    def run_stage(self, expected=0):
        p=subprocess.Popen(['/bin/ash',str(self.app/'lib/operation-worker.sh'),
            '--request',self.request['requestId']],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        out,err=self.collect(p,90)
        self.assertEqual(p.returncode,expected,(out,err))

    def lookup(self):
        return json.loads(self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_queue_lookup '+self.nonce).stdout)

    def test_dot_yields_after_one_probe_and_preserves_other_results(self):
        self.config.update(requestedIds=['google-primary','cloudflare-primary'],
                           selectedIds=['google-primary','cloudflare-primary'])
        (self.dot/'config.json').write_text(json.dumps(self.config))
        state=json.loads((self.dot/'state.json').read_bytes())
        foreign={'id':'foreign-test-result','ok':False,'testedEpoch':17}
        state['tests']=[foreign]
        (self.dot/'state.json').write_text(json.dumps(state))
        self.submit()
        before=(self.dot/'config.json').read_bytes()
        self.run_stage()
        first=json.loads((self.dot/'state.json').read_bytes())
        self.assertEqual(len((self.app/'tls-calls').read_text().splitlines()),1)
        self.assertEqual(self.lookup()['state'],'queued')
        self.assertEqual(first['autoCheck']['checkedCount'],1)
        self.assertIn(foreign,first['tests'])
        self.assertEqual(first['lastError'],'KEEP')
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
        self.run_stage()
        final=json.loads((self.dot/'state.json').read_bytes())
        self.assertEqual(len((self.app/'tls-calls').read_text().splitlines()),2)
        self.assertEqual(self.lookup()['state'],'completed')
        self.assertEqual(final['autoCheck']['status'],'success')
        self.assertEqual(final['autoCheck']['checkedCount'],2)
        self.assertIn(foreign,final['tests'])
        self.assertEqual(final['lastOperation'],state['lastOperation'])
        self.assertEqual((self.dot/'config.json').read_bytes(),before)
        self.run_stage(expected=2)
        self.assertEqual(json.loads((self.dot/'state.json').read_bytes()),final)

    def test_selection_change_rejects_pending_result(self):
        self.submit()
        before=(self.dot/'state.json').read_bytes()
        fake=self.app/'bin/openssl'
        fake.write_text('''#!/bin/ash
echo ready >"$BRORAY_ROOT/tmp/dot-ready"
while [ ! -f "$BRORAY_ROOT/tmp/dot-release" ]; do sleep .05; done
exit 0
''')
        p=subprocess.Popen(['/bin/ash',str(self.app/'lib/operation-worker.sh'),
            '--request',self.request['requestId']],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            until=time.monotonic()+40
            while not (self.app/'tmp/dot-ready').exists():
                self.assertIsNone(p.poll())
                self.assertLess(time.monotonic(),until,'DoT probe not started')
                time.sleep(.05)
            self.config.update(requestedIds=['cloudflare-primary'],selectedIds=['cloudflare-primary'])
            (self.dot/'config.json').write_text(json.dumps(self.config))
            changed=(self.dot/'config.json').read_bytes()
            (self.app/'tmp/dot-release').touch()
            out,err=self.collect(p,60)
            self.assertEqual(p.returncode,76,(out,err))
            self.assertEqual((self.dot/'state.json').read_bytes(),before)
            self.assertEqual((self.dot/'config.json').read_bytes(),changed)
            self.assertEqual(self.lookup()['state'],'failed')
        finally:
            (self.app/'tmp/dot-release').touch()
            if p.poll() is None: self.collect(p,60)

    def test_disabled_between_probes_does_not_probe_next(self):
        self.config.update(requestedIds=['google-primary','cloudflare-primary'],
                           selectedIds=['google-primary','cloudflare-primary'])
        (self.dot/'config.json').write_text(json.dumps(self.config))
        self.submit()
        self.run_stage()
        before=(self.dot/'state.json').read_bytes()
        (self.dot/'auto-check.json').write_text('{"schemaVersion":1,"enabled":false}')
        self.run_stage(expected=76)
        self.assertEqual(len((self.app/'tls-calls').read_text().splitlines()),1)
        self.assertEqual((self.dot/'state.json').read_bytes(),before)
        self.assertEqual(self.lookup()['state'],'failed')

    def test_negative_tls_is_completed_measurement(self):
        self.submit()
        self.env['TEST_TLS_RC']='1'
        self.run_stage()
        state=json.loads((self.dot/'state.json').read_bytes())
        self.assertFalse(state['tests'][0]['ok'])
        self.assertEqual(state['autoCheck']['status'],'failed')
        self.assertEqual(self.lookup()['state'],'completed')

    def test_pause_resume_preserves_cursor_without_duplicate_probe(self):
        self.config.update(requestedIds=['google-primary','cloudflare-primary'],
                           selectedIds=['google-primary','cloudflare-primary'])
        (self.dot/'config.json').write_text(json.dumps(self.config))
        self.submit()
        self.run_stage()
        before=(self.dot/'state.json').read_bytes()
        self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call pause')
        self.run_stage(expected=2)
        self.assertEqual(self.lookup()['state'],'queued')
        self.assertEqual((self.dot/'state.json').read_bytes(),before)
        self.assertEqual(len((self.app/'tls-calls').read_text().splitlines()),1)
        self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call resume')
        self.run_stage()
        self.assertEqual(self.lookup()['state'],'completed')
        self.assertEqual(len((self.app/'tls-calls').read_text().splitlines()),2)


class DotAdmission(unittest.TestCase):
    setUp=OperationResources.setUp
    tearDown=OperationResources.tearDown
    set_owner=OperationResources.set_owner
    call=OperationResources.call
    submit=OperationResources.submit
    queue_file=OperationResources.queue_file
    claim=OperationResources.claim
    ack=OperationResources.ack
    finish=OperationResources.finish

    def test_manual_dot_writer_excludes_prepare_but_allows_health(self):
        writer=self.call('begin','system','dot:apply','dns-over-tls','USER',
                         '900001','protected',uuid.uuid4().hex)
        self.ack(writer)
        before=(self.temp/'global.lock/owner.json').read_bytes()
        prepare=self.submit('dot:auto-check','selected','SCHEDULER')
        self.assertEqual(self.claim(prepare,expected=2)['errorCode'],'OPERATION_BUSY')
        observer=self.claim(self.submit('servers:active-health','active','AUTO_SWITCH'))
        self.ack(observer)
        self.assertEqual((self.temp/'global.lock/owner.json').read_bytes(),before)

    def test_dot_prepare_excludes_manual_writer_until_yield(self):
        prepare=self.claim(self.submit('dot:auto-check','selected','SCHEDULER'))
        self.ack(prepare)
        before=(self.temp/'ram/resources/background-prepare/owner.json').read_bytes()
        for action in ['dot:apply','dot:test','dot:delete','dot:auto-settings']:
            self.assertEqual(self.call('begin','system',action,'dns-over-tls','USER',
                '900001','protected',uuid.uuid4().hex,expected=2)['errorCode'],'RESOURCE_BUSY')
        observer=self.claim(self.submit('servers:active-health','active','AUTO_SWITCH'))
        self.ack(observer)
        self.assertEqual((self.temp/'ram/resources/background-prepare/owner.json').read_bytes(),before)
        self.finish(prepare)
        writer=self.call('begin','system','dot:apply','dns-over-tls','USER',
                         '900001','protected',uuid.uuid4().hex)
        self.ack(writer)
        state=json.loads(((self.temp/'global.lock').resolve().parent/'state.json').read_bytes())
        self.assertEqual(state['operationId'],writer['operationId'])
        self.assertEqual(state['resourceLocks'],['global'])


if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    unittest.main(verbosity=2,failfast=True)
