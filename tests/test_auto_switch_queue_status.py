"""Read-only cache projection against the real queue; labelled fake boot identity."""
import json
import subprocess
import unittest
import test_operation_queue as queue
import test_operations as support


class QueueStatus(unittest.TestCase):
    setUp = queue.OperationQueue.setUp
    tearDown = queue.OperationQueue.tearDown
    set_owner = queue.OperationQueue.set_owner
    call = queue.OperationQueue.call
    submit = queue.OperationQueue.submit
    queue_file = queue.OperationQueue.queue_file

    def cache(self, request):
        self.cache_file = self.temp/'auto-state.json'
        value = {'schemaVersion':3, 'status':'healthy', 'consecutiveFailures':0,
                 'activeHealth':{'status':'healthy','context':'a'*64},
                 'qualityRefresh':{'status':'running','requestId':request['requestId'],
                   'runStartedAt':'2026-09-27T00:00:00Z','totalCount':3,
                   'checkedCount':1,'availableCount':1,'unavailableCount':0,'errorCount':0}}
        self.cache_file.write_text(json.dumps(value))
        self.cache_file.chmod(0o600)
        return value

    def project(self):
        self.env['TEST_AUTO_CACHE']=str(self.cache_file)
        before=self.cache_file.read_bytes()
        queue_before=self.queue_file().read_bytes()
        result=subprocess.run([str(support.BB),'ash','-c',
            '. "$BRORAY_ROOT/lib/auto-switch-status.sh"; broray_auto_switch_public_state "$TEST_AUTO_CACHE"'],
            env=self.env,capture_output=True,timeout=30)
        self.assertEqual(result.returncode,0,(result.stdout,result.stderr))
        self.assertEqual(self.cache_file.read_bytes(),before)
        self.assertEqual(self.queue_file().read_bytes(),queue_before)
        return json.loads(result.stdout)

    def test_cancelled_quality_is_not_reported_as_running(self):
        request=self.submit()
        old=self.cache(request)
        self.call('queue-cancel',request['requestId'])
        actual=self.project()
        self.assertEqual(actual['qualityRefresh']['status'],'error')
        self.assertEqual(actual['qualityRefresh']['queueState'],'cancelled')
        self.assertIsNone(actual['qualityRefresh']['runStartedAt'])
        self.assertEqual(actual['qualityRefresh']['checkedCount'],1)
        self.assertEqual(actual['status'],old['status'])
        self.assertEqual(actual['activeHealth'],old['activeHealth'])

    def test_queued_continuation_preserves_completed_measurements(self):
        request=self.submit()
        self.cache(request)
        actual=self.project()
        self.assertEqual(actual['qualityRefresh']['status'],'running')
        self.assertEqual(actual['qualityRefresh']['queueState'],'queued')
        self.assertEqual(actual['qualityRefresh']['checkedCount'],1)

    def test_prior_boot_request_is_unknown_without_recreating_queue(self):
        request=self.submit()
        self.cache(request)
        (self.proc/'sys/kernel/random/boot_id').write_text('boot-two\n')
        actual=self.project()
        self.assertEqual(actual['qualityRefresh']['status'],'error')
        self.assertEqual(actual['qualityRefresh']['queueState'],'unknown')
        self.assertFalse((self.temp/'ram/queue/boot-two').exists())

    def test_finished_failover_does_not_override_new_health(self):
        request=self.submit()
        old=self.cache(request)
        old['failover']={'status':'recovered','requestId':request['requestId'],
                         'sourceContext':'a'*64}
        self.cache_file.write_text(json.dumps(old))
        actual=self.project()
        self.assertEqual(actual['status'],'healthy')
        self.assertEqual(actual['activeHealth'],old['activeHealth'])

    def test_corrupt_queue_stays_inspectable_and_status_is_unknown(self):
        request=self.submit()
        self.cache(request)
        self.queue_file().write_bytes(b'{broken')
        actual=self.project()
        self.assertEqual(actual['qualityRefresh']['status'],'error')
        self.assertEqual(actual['qualityRefresh']['queueState'],'unknown')

    def test_legacy_failed_cycle_still_projects_error_without_mutating_cache(self):
        old=self.cache(self.submit())
        del old['qualityRefresh']['requestId']
        old['backgroundOperationId']='op-old-cycle'
        old['status']='checking-candidates'
        self.cache_file.write_text(json.dumps(old))
        operation=self.state/'operations/op-old-cycle'
        operation.mkdir(parents=True)
        (operation/'state.json').write_text(json.dumps({'kind':'background',
            'operationId':'op-old-cycle','operation':'auto-switch','running':False,'state':'failed'}))
        actual=self.project()
        self.assertEqual(actual['status'],'error')
        self.assertEqual(actual['qualityRefresh']['status'],'error')
        self.assertEqual(actual['backgroundOperationState'],'failed')


if __name__=='__main__':
    unittest.main(verbosity=2,failfast=True)
