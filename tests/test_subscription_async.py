"""Backend async handoff, cancellation and result projection; no browser."""
import ctypes,json,os,subprocess,time,unittest
from test_subscription_jobs import ROOT,SubscriptionJobs

class SubscriptionAsync(unittest.TestCase):
    setUp=SubscriptionJobs.setUp
    clean_fixture=SubscriptionJobs.clean_fixture
    record=SubscriptionJobs.record
    states=SubscriptionJobs.states
    shell=SubscriptionJobs.shell
    reap_adopted_helpers=SubscriptionJobs.reap_adopted_helpers
    def launch(self,create=False):
        if create:
            body=self.temp/'request.json'
            body.write_text(json.dumps(dict(name='Test',url='https://93.184.216.34/sub/PRIVATE_CANARY',enabled=True,autoUpdateEnabled=True,updateIntervalMinutes=360,updateImmediately=True)))
            self.env['TEST_BODY']=str(body)
            action='BRORAY_SUB_WEB_ASYNC=true; broray_subscription_create "$TEST_BODY"'
        else:
            self.record();action='broray_subscription_launch_update test manual'
        script='''
. "$BRORAY_ROOT/lib/subscription-service.sh"
. "$BRORAY_ROOT/lib/subscription-web-job.sh"
broray_job_begin system subscriptions:refresh subscriptions USER cooperative || exit $?
trap 'broray_job_exit "$?"' EXIT
'''+action
        try:
            p=self.shell(script,timeout=60)
        except subprocess.TimeoutExpired as error:
            print('LAUNCH_TIMEOUT_STATES='+json.dumps(self.states()),flush=True)
            print('LAUNCH_TIMEOUT_TRACE='+str(error.stderr[-12000:] if error.stderr else b''),flush=True)
            raise
        result=json.loads(p.stdout)
        self.assertTrue(result['accepted']);self.assertNotIn('PRIVATE_CANARY',p.stdout.decode())
        return result
    def wait(self,predicate,timeout=50):
        end=time.monotonic()+timeout
        while time.monotonic()<end:
            self.reap_adopted_helpers()
            if predicate():return
            time.sleep(.05)
        logs=[p.read_text() for p in (self.state/'operations').glob('*/subscription-worker.log')]
        self.fail(('backend worker did not reach expected state',self.states(),logs))
    def cancel(self,operation):
        self.env['TEST_OPERATION']=operation
        self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call cancel "$TEST_OPERATION"')
    def terminal(self):
        # Terminal state is durably published before fence retirement. Wait
        # for both steps; a reader can legitimately observe the interval.
        return self.states() and not self.states()[0]['running'] and not (self.temp/'global.lock').is_symlink()
    def test_create_returns_before_download_and_can_be_cancelled(self):
        self.env['TEST_MODE']='wait'
        result=self.launch(create=True)
        try:
            self.wait(lambda:(self.temp/'transport-ready').exists())
            self.assertTrue((self.temp/'global.lock').is_symlink())
        finally:
            self.cancel(result['backgroundOperationId']);self.wait(self.terminal)
        self.assertEqual(self.states()[0]['state'],'aborted')
        self.assertFalse((self.temp/'global.lock').is_symlink())
        data=json.loads(self.shell('. "$BRORAY_ROOT/lib/subscription-service.sh"; broray_subscription_list').stdout)[0]
        self.assertEqual(data['lastUpdateResult']['errorCode'],'CANCELLED')
        self.assertFalse(data['updateOperation']['canCancel'])
    def test_update_commits_real_nodes_after_parent_exit(self):
        self.launch();self.wait(self.terminal,timeout=90)
        data=json.loads((self.subdir/'test.json').read_text())
        self.assertEqual(data['lastUpdateStatus'],'success')
        self.assertEqual(data['lastUpdateResult']['accepted'],1)
        self.assertEqual(self.states()[0]['state'],'completed')
    def test_cancel_before_worker_acceptance_finishes_without_download(self):
        gate=self.temp/'accept-gate';self.env['TEST_ACCEPT_GATE']=str(gate)
        curl=self.app/'bin/curl';curl.write_text(curl.read_text().replace('#!/bin/ash\n','#!/bin/ash\necho requested >"$TEST_READY"\n',1))
        worker=self.app/'lib/subscription-web-worker.sh'
        worker.write_text(worker.read_text().replace('broray_ops_accept_handoff "$web_nonce"',
            'while [ ! -e "$TEST_ACCEPT_GATE" ]; do sleep .1; done\nbroray_ops_accept_handoff "$web_nonce"'))
        result=self.launch()
        try:self.cancel(result['backgroundOperationId'])
        finally:gate.touch()
        self.wait(self.terminal)
        self.assertEqual(self.states()[0]['state'],'aborted')
        self.assertFalse((self.temp/'transport-ready').exists())
    def test_cancel_during_parse_preserves_previous_catalog(self):
        self.env['TEST_PARSE_READY']=str(self.temp/'parse-ready')
        helper=self.app/'lib/subscription-prepare.sh'
        helper.write_text(helper.read_text().replace('  parse)\n','  parse)\n    echo ready >"$TEST_PARSE_READY"; sleep 60\n'))
        servers=self.app/'servers';servers.mkdir(exist_ok=True)
        keep=servers/'previous.json';keep.write_text('{"canary":"previous catalog"}')
        before=keep.read_bytes()
        result=self.launch()
        try:self.wait(lambda:(self.temp/'parse-ready').exists())
        finally:self.cancel(result['backgroundOperationId']);self.wait(self.terminal)
        self.assertEqual(keep.read_bytes(),before)
        self.assertEqual(self.states()[0]['state'],'aborted')
    def test_cancel_before_handoff_releases_original_owner(self):
        self.record();gate=self.temp/'parent-gate';ready=self.temp/'parent-ready'
        self.env.update(TEST_PARENT_GATE=str(gate),TEST_PARENT_READY=str(ready))
        library=self.app/'lib/subscription-web-job.sh'
        library.write_text(library.read_text().replace('    if ! broray_ops_handoff_to',
            '    echo ready >"$TEST_PARENT_READY"\n    while [ ! -e "$TEST_PARENT_GATE" ]; do sleep .1; done\n    if ! broray_ops_handoff_to'))
        script='''
. "$BRORAY_ROOT/lib/subscription-service.sh"
. "$BRORAY_ROOT/lib/subscription-web-job.sh"
broray_job_begin system subscriptions:refresh subscriptions USER cooperative || exit $?
trap 'broray_job_exit "$?"' EXIT
broray_subscription_launch_update test manual
'''
        p=subprocess.Popen(['/bin/ash','-c',script],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            self.wait(ready.exists)
            self.cancel(self.states()[0]['operationId']);gate.touch()
            out,err=p.communicate(timeout=60)
            self.assertEqual(p.returncode,130,(out,err))
            self.wait(self.terminal)
            self.assertEqual(self.states()[0]['state'],'aborted')
        finally:
            gate.touch()
            if p.poll() is None:p.kill()
            p.communicate(timeout=10)
    def test_network_error_is_visible_after_accepted_response(self):
        self.env['TEST_MODE']='error';self.launch();self.wait(self.terminal)
        data=json.loads((self.subdir/'test.json').read_text())
        self.assertEqual(data['lastUpdateStatus'],'error')
        self.assertTrue(data['lastError'])
        self.assertFalse((self.temp/'global.lock').is_symlink())

if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    class ImmediateResult(unittest.TextTestResult):
        def addFailure(self,test,error):
            super().addFailure(test,error);self.stream.writeln(self._exc_info_to_string(error,test))
        def addError(self,test,error):
            super().addError(test,error);self.stream.writeln(self._exc_info_to_string(error,test))
    r=unittest.TextTestRunner(verbosity=2,resultclass=ImmediateResult).run(unittest.defaultTestLoader.loadTestsFromTestCase(SubscriptionAsync))
    (ROOT/'docs/evidence/subscription-async-tests.json').write_text(json.dumps({'status':'PASS' if r.wasSuccessful() else 'FAIL','testsRun':r.testsRun,'routerAccessed':False,'webuiTested':False})+'\n')
    raise SystemExit(0 if r.wasSuccessful() else 1)
