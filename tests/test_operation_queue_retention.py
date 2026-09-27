"""RAM preparation retention uses terminal ownership proof, never file age."""
import json
import shutil
from unittest.mock import patch
import unittest
from test_operation_resources import OperationResources
import test_operations as operation_support


class Retention(unittest.TestCase):
    setUp=OperationResources.setUp
    tearDown=OperationResources.tearDown
    set_owner=OperationResources.set_owner
    call=OperationResources.call
    submit=OperationResources.submit
    queue_file=OperationResources.queue_file
    claim=OperationResources.claim
    ack=OperationResources.ack
    finish=OperationResources.finish
    opfile=OperationResources.opfile

    def complete(self, target='old'):
        request=self.submit(target=target)
        operation=self.claim(request)
        self.ack(operation)
        path=self.temp/'ram/requests'/request['requestId']
        (path/'payload').write_bytes(b'x'*1048576)
        (path/'payload').chmod(0o600)
        self.finish(operation)
        return request,operation,path

    def another_claim(self):
        operation=self.claim(self.submit(target='maintenance'))
        self.ack(operation)
        return operation

    def test_completed_payload_kept_for_live_owner_then_pruned_after_identity_exit(self):
        request,operation,path=self.complete()
        self.finish(self.another_claim())
        self.assertTrue((path/'payload').exists(),'Live owner still has private scratch')
        # A new birth for the same PID does not own the former request.
        self.set_owner(self.owner|{'startTicks':'101'})
        next_operation=self.another_claim()
        self.assertFalse(path.exists())
        self.assertTrue(self.opfile(operation,'state.json').exists(),'Small terminal evidence must remain')
        self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())
        self.assertEqual(self.call('queue-lookup',request['requestId'][2:])['state'],'completed')
        self.finish(next_operation)

    def test_unfinished_continuation_is_preserved(self):
        request=self.submit()
        operation=self.claim(request)
        self.ack(operation)
        path=self.temp/'ram/requests'/request['requestId']
        (path/'payload').write_bytes(b'private')
        (path/'payload').chmod(0o600)
        self.call('queue-cancel',request['requestId'],expected=2)
        self.assertEqual((path/'payload').read_bytes(),b'private')
        self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_refused_claim_does_not_run_unrelated_retention(self):
        _,old,path=self.complete()
        # A different live owner holds prepare. The completed former owner's
        # scratch becomes collectible, but a refused claim must not spend the
        # exclusive coordinator guard doing unrelated cleanup first.
        peer=self.owner|{'pid':900002}
        self.identities.write_text(json.dumps({'900001':self.owner,'900002':peer}))
        active=self.claim(self.submit(target='live'),pid='900002')
        self.ack(active,pid='900002')
        self.identities.write_text(json.dumps({'900001':self.owner|{'status':'absent'},'900002':peer}))
        queued=self.submit(target='waiting')
        queue_before=self.queue_file().read_bytes()
        state_before=self.opfile(active,'state.json').read_bytes()
        reply=self.claim(queued,pid='900002',expected=2)
        self.assertEqual(reply['errorCode'],'RESOURCE_BUSY')
        self.assertTrue((path/'payload').exists(),'Refused admission ran unrelated cleanup')
        self.assertEqual(self.queue_file().read_bytes(),queue_before)
        self.assertEqual(self.opfile(active,'state.json').read_bytes(),state_before)
        self.finish(active)
        admitted=self.claim(queued,pid='900002')
        self.assertFalse(path.exists(),'Successful admission must still reclaim safe scratch')
        self.assertTrue(self.opfile(old,'state.json').exists())
        self.ack(admitted,pid='900002')
        self.finish(admitted)

    def test_missing_receipt_and_unsafe_payload_preserve_evidence(self):
        request,operation,path=self.complete()
        self.set_owner(self.owner|{'startTicks':'101'})
        foreign=self.temp/'foreign';foreign.write_bytes(b'KEEP')
        (path/'unsafe').symlink_to(foreign)
        self.finish(self.another_claim())
        self.assertTrue((path/'unsafe').is_symlink())
        self.assertEqual(foreign.read_bytes(),b'KEEP')
        (path/'unsafe').unlink()
        state=json.loads(self.queue_file().read_bytes())
        state['receipts']=[r for r in state['receipts'] if r['requestId']!=request['requestId']]
        self.queue_file().write_text(json.dumps(state))
        self.set_owner(self.owner|{'startTicks':'102'})
        self.finish(self.another_claim())
        self.assertTrue((path/'payload').exists(),'No terminal queue receipt: no scratch cleanup')

    def test_unconfirmed_children_preserve_payload(self):
        request,operation,path=self.complete()
        self.opfile(operation,'children.json').symlink_to(self.temp/'unknown-child-ledger')
        self.set_owner(self.owner|{'startTicks':'101'})
        self.finish(self.another_claim())
        self.assertTrue((path/'payload').exists())
        self.assertTrue(self.opfile(operation,'children.json').is_symlink())

    def test_pruning_cost_preserves_live_payloads(self):
        # Profile only this private fixture copy. Do not change the guard,
        # client timeout, proof predicates or the live source under test.
        app=self.temp/'profile-app'
        shutil.copytree(operation_support.APP,app)
        swap=patch.object(operation_support,'APP',app)
        swap.start();self.addCleanup(swap.stop)
        self.env['BRORAY_ROOT']=str(app)
        self.env['BRORAY_OPS_CODE_ROOT']=str(app)
        script=app/'lib/operation-scheduling.sh'
        body=script.read_text()
        for name in ['ops_queue_prune_requests','ops_queue_prune_history','ops_queue_select','ops_queue_store']:
            declaration=name+'()\n'
            self.assertEqual(body.count(declaration),1)
            body=body.replace(declaration,name+'_profiled()\n',1)
            body+='\n'+name+'() {\n'+'''
    local timing_start timing_end timing_unused timing_rc
    IFS=' ' read -r timing_start timing_unused </proc/uptime
    timing_rc=0
    '''+name+'''_profiled "$@" || timing_rc=$?
    IFS=' ' read -r timing_end timing_unused </proc/uptime
    printf '%s %s '''+name+'''\\n' "$timing_start" "$timing_end" >>"$BRORAY_STATE_ROOT/queue-costs"
    return "$timing_rc"
}
'''
        script.write_text(body)
        paths=[self.complete(str(i))[2] for i in range(4)]
        self.another_claim()
        for path in paths:self.assertEqual((path/'payload').stat().st_size,1048576)
        print('QUEUE_PRUNING_COSTS', (self.state/'queue-costs').read_text(),flush=True)

    def test_bound_finish_captures_exact_owner_once(self):
        app=self.temp/'finish-profile-app'
        shutil.copytree(operation_support.APP,app)
        swap=patch.object(operation_support,'APP',app)
        swap.start();self.addCleanup(swap.stop)
        self.env['BRORAY_ROOT']=str(app)
        self.env['BRORAY_OPS_CODE_ROOT']=str(app)
        source=app/'lib/operation-owner.sh'
        text=source.read_text()
        declaration='broray_ops_capture_owner()\n'
        self.assertEqual(text.count(declaration),1)
        source.write_text(text.replace(declaration,'broray_ops_capture_owner_original()\n',1)+'''
broray_ops_capture_owner() {
    if [ "${verb:-}" = finish ]; then
        echo capture >>"$BRORAY_STATE_ROOT/finish-captures"
    fi
    broray_ops_capture_owner_original "$@"
}
''')
        operation=self.claim(self.submit())
        self.ack(operation)
        self.call('finish',operation['operationId'],operation['token'],'completed','','900001')
        self.assertEqual((self.state/'finish-captures').read_text().splitlines(),['capture'])
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_recovery_loads_each_live_resource_owner_once(self):
        app=self.temp/'recover-profile-app'
        shutil.copytree(operation_support.APP,app)
        swap=patch.object(operation_support,'APP',app)
        swap.start();self.addCleanup(swap.stop)
        self.env['BRORAY_ROOT']=str(app)
        self.env['BRORAY_OPS_CODE_ROOT']=str(app)
        source=app/'lib/operation-coordinator.sh'
        text=source.read_text()
        self.assertEqual(text.count('ops_load()\n'),1)
        text=text.replace('ops_load()\n','ops_load_original()\n',1)
        marker='verb="${1:-}"; [ "$#" -gt 0 ] && shift'
        self.assertEqual(text.count(marker),1)
        source.write_text(text.replace(marker,'''ops_load() {
    if [ "${verb:-}" = queue-recover ]; then
        printf '%s\\n' "$1" >>"$BRORAY_STATE_ROOT/recovery-loads"
    fi
    ops_load_original "$@"
}
'''+marker,1))
        prepare=self.claim(self.submit())
        self.ack(prepare)
        observer=self.claim(self.submit(action='servers:active-health',target='active',source='AUTO_SWITCH'))
        self.ack(observer)
        before=self.queue_file().read_bytes()
        self.call('queue-recover')
        self.assertCountEqual((self.state/'recovery-loads').read_text().splitlines(),
                              [prepare['operationId'],observer['operationId']])
        self.assertEqual(self.queue_file().read_bytes(),before)
        self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())
        self.assertTrue((self.temp/'ram/resources/active-observer').is_symlink())
        self.finish(observer);self.finish(prepare)

    def test_unknown_record_prevents_cleanup_of_other_terminal_request(self):
        request,operation,path=self.complete()
        self.set_owner(self.owner|{'startTicks':'101'})
        unknown=self.temp/'ram/steps/unknown'
        unknown.mkdir()
        record=unknown/'state.json'
        for content in [b'{broken',b'[]',b'{"queueStep":3}']:
            with self.subTest(content=content):
                record.write_bytes(content)
                self.finish(self.another_claim())
                self.assertEqual(record.read_bytes(),content)
                self.assertTrue((path/'payload').exists(),'Unknown index evidence must prevent deletion')
        record.unlink();unknown.rmdir()
        self.finish(self.another_claim())
        self.assertFalse(path.exists(),'Valid retired owner should remain eligible for cleanup')


if __name__=='__main__':
    unittest.main(verbosity=2,failfast=True)
