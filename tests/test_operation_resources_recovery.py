"""Interrupted queue/resource publication, lifecycle and writer transition."""
import hashlib
import json
import shutil
import unittest
import uuid
import test_operation_resources as support


class ResourceRecovery(unittest.TestCase):
    setUp = support.OperationResources.setUp
    tearDown = support.OperationResources.tearDown
    set_owner = support.OperationResources.set_owner
    call = support.OperationResources.call
    submit = support.OperationResources.submit
    queue_file = support.OperationResources.queue_file
    claim = support.OperationResources.claim
    ack = support.OperationResources.ack
    opfile = support.OperationResources.opfile
    finish = support.OperationResources.finish

    def test_published_unacknowledged_owner_with_previous_queue_bytes_is_replayed(self):
        request = self.submit()
        before_publication = self.queue_file().read_bytes()
        nonce = uuid.uuid4().hex
        operation = self.claim(request, nonce)
        # Exact disk combination at the publication-before-queue-store boundary.
        # No ack was sent, therefore no helper may have started.
        self.queue_file().write_bytes(before_publication)
        self.assertEqual(self.claim(request, nonce), operation)
        self.ack(operation)
        self.assertEqual(len(list((self.temp/'ram/steps').glob('*/owner.json'))), 1)

    def test_dead_step_recovers_without_signals_or_replaying_old_user_work(self):
        request = self.submit(source='USER', nonce='d'*32)
        operation = self.claim(request)
        self.ack(operation)
        self.set_owner({'status': 'absent'})
        self.assertTrue(self.call('queue-recover')['ok'])
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
        self.assertEqual(self.call('queue-lookup', 'd'*32)['state'], 'cancelled')
        self.assertIsNone(self.call('queue-next')['requestId'])
        self.assertTrue(self.call('queue-recover')['ok'])

    def test_missing_children_evidence_and_live_owner_are_preserved(self):
        operation = self.claim(self.submit())
        self.ack(operation)
        before = self.opfile(operation, 'state.json').read_bytes()
        self.call('queue-recover')
        self.assertEqual(self.opfile(operation, 'state.json').read_bytes(), before)
        self.opfile(operation, 'children.json').symlink_to(self.temp/'missing-child-evidence')
        self.set_owner({'status': 'absent'})
        self.assertEqual(self.call('queue-recover', expected=2)['errorCode'], 'CHILDREN_UNCONFIRMED')
        self.assertEqual(self.opfile(operation, 'state.json').read_bytes(), before)
        self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_prepare_transfers_to_protected_global_apply(self):
        request = self.submit('subscriptions:refresh', 'sub-one', 'SUBSCRIPTION_AUTO')
        for next_stage in ['parse', 'apply']:
            operation = self.claim(request)
            self.ack(operation)
            result = self.temp/'ram/requests'/request['requestId']/'result.json'
            result.write_bytes(b'{"prepared":true}\n')
            result.chmod(0o600)
            self.call('queue-yield', operation['operationId'], operation['token'], '900001',
                      next_stage, hashlib.sha256(result.read_bytes()).hexdigest())
        apply = self.claim(request)
        self.ack(apply)
        self.assertEqual(apply['resourceLocks'], ['global'])
        self.assertTrue((self.temp/'global.lock').is_symlink())
        self.assertEqual(self.call('cancel', apply['operationId'], expected=2)['errorCode'], 'CANCEL_NOT_SUPPORTED')
        self.finish(apply)
        self.assertFalse((self.temp/'global.lock').is_symlink())

    def ready_apply(self, source='SUBSCRIPTION_AUTO'):
        request = self.submit('subscriptions:refresh', 'sub-one', source)
        for stage in ['parse', 'apply']:
            operation = self.claim(request)
            self.ack(operation)
            result = self.temp/'ram/requests'/request['requestId']/'result.json'
            result.write_bytes(b'{"prepared":true}\n'); result.chmod(0o600)
            self.call('queue-yield', operation['operationId'], operation['token'], '900001',
                      stage, hashlib.sha256(result.read_bytes()).hexdigest())
        return request

    def test_global_apply_not_selected_while_observer_runs(self):
        request=self.ready_apply()
        observer=self.claim(self.submit('servers:active-health','active','AUTO_SWITCH'))
        self.ack(observer)
        self.assertIsNone(self.call('queue-next')['requestId'])
        self.finish(observer)
        self.assertEqual(self.call('queue-next')['requestId'],request['requestId'])

    def admission_boot(self, phase=None, revision=None):
        request=self.ready_apply()
        operation=self.claim(request);self.ack(operation)
        path=self.state/'operations'/operation['operationId']/'state.json'
        if phase:
            self.call('tick',operation['operationId'],operation['token'],phase)
        if revision is not None:
            data=json.loads(path.read_bytes());data['revision']=revision
            path.write_text(json.dumps(data))
        shutil.rmtree(self.temp/'ram')
        (self.proc/'sys/kernel/random/boot_id').write_text('boot-two\n')
        return operation,path

    def updater_stub(self, failure=False):
        stub=self.temp/'updater-init';self.starts=self.temp/'updater-starts'
        stub.write_text('#!/bin/sh\n'+
            'echo "$1" >> '+str(self.starts)+'\n'+
            ('exit 75\n' if failure else
             "echo '{\"ok\":true,\"phase\":\"COMMIT_VERIFIED\",\"generationId\":\"g-0123456789012345678901\",\"platformReady\":true,\"activationAllowed\":false}'\n"))
        stub.chmod(0o700)
        self.env['BRORAY_OPS_BOOT_UPDATER_INIT']=str(stub)

    def test_boot_recovers_acknowledged_subscription_before_first_write(self):
        operation,path=self.admission_boot();self.updater_stub()
        self.assertEqual(json.loads(path.read_bytes())['revision'],2)
        self.assertTrue(self.call('initialize')['ok'])
        self.assertFalse((self.temp/'global.lock').is_symlink())
        state=json.loads(path.read_bytes())
        self.assertEqual(state['state'],'recovered')
        self.assertEqual(state['bootAdmissionRecovery']['state'],'ready')
        self.assertEqual(self.starts.read_text().splitlines(),['start','status'])
        before=path.read_bytes();self.call('initialize')
        self.assertEqual(path.read_bytes(),before)
        self.assertEqual(self.starts.read_text().splitlines(),['start','status'])

    def test_boot_resume_failure_retains_intent_and_retry_is_idempotent(self):
        operation,path=self.admission_boot();self.updater_stub(failure=True)
        self.assertEqual(self.call('initialize',expected=75)['errorCode'],'PLATFORM_RECOVERY_UNCONFIRMED')
        state=json.loads(path.read_bytes())
        self.assertEqual(state['state'],'recovered')
        self.assertEqual(state['bootAdmissionRecovery']['state'],'pending')
        self.assertFalse((self.temp/'global.lock').is_symlink())
        self.updater_stub();self.assertTrue(self.call('initialize')['ok'])
        self.assertEqual(json.loads(path.read_bytes())['bootAdmissionRecovery']['state'],'ready')

    def test_working_after_mutation_checkpoint_is_not_admission(self):
        operation,path=self.admission_boot(phase='committing',revision=4)
        data=json.loads(path.read_bytes());data['phase']='working';path.write_text(json.dumps(data))
        before=path.read_bytes();self.updater_stub()
        self.call('initialize')
        self.assertEqual(path.read_bytes(),before)
        self.assertTrue((self.temp/'global.lock').is_symlink())
        self.assertFalse(self.starts.exists())

    def test_admission_unknown_evidence_is_preserved(self):
        operation,path=self.admission_boot();self.updater_stub()
        (path.parent/'unknown-writer').write_text('KEEP')
        before=path.read_bytes();self.call('initialize')
        self.assertEqual(path.read_bytes(),before)
        self.assertTrue((self.temp/'global.lock').is_symlink())
        self.assertFalse(self.starts.exists())

    def test_same_boot_absent_admission_is_not_recovered(self):
        operation,path=self.admission_boot();self.updater_stub()
        (self.proc/'sys/kernel/random/boot_id').write_text('boot-one\n')
        self.set_owner({'status':'absent'});before=path.read_bytes()
        self.call('initialize')
        self.assertEqual(path.read_bytes(),before)
        self.assertTrue((self.temp/'global.lock').is_symlink())
        self.assertFalse(self.starts.exists())

    def test_reboot_drops_ram_queue_without_discarding_protected_commit(self):
        request=self.ready_apply(source='USER')
        operation=self.claim(request);self.ack(operation)
        self.call('tick',operation['operationId'],operation['token'],'committing')
        record=self.state/'operations'/operation['operationId']/'state.json'
        before=record.read_bytes()
        fence=(self.temp/'global.lock/owner.json').read_bytes()
        # Private synthetic /proc + RAM boot boundary, not a physical reboot.
        # An acknowledged domain commit remains protected; its queue receipt
        # cannot authorize deleting the fence or replaying the user mutation.
        ram=(self.temp/'ram').resolve()
        self.assertEqual(ram.parent,self.temp.resolve())
        shutil.rmtree(ram)
        (self.proc/'sys/kernel/random/boot_id').write_text('boot-two\n')
        self.set_owner({'status':'absent'})
        self.assertTrue(self.call('initialize')['ok'])
        self.assertIsNone(self.call('queue-next')['requestId'])
        self.assertEqual(self.call('queue-lookup',request['requestId'][2:],expected=2)['errorCode'],
                         'REQUEST_UNCONFIRMED')
        self.assertTrue(self.call('queue-recover')['ok'])
        self.assertEqual(self.call('recover',expected=2)['result'],'protected_recovery')
        self.assertEqual(record.read_bytes(),before)
        self.assertEqual((self.temp/'global.lock/owner.json').read_bytes(),fence)
        self.assertFalse((ram/'queue/boot-two').exists(),'A read-only query recreated the RAM queue')

    def test_waiting_urgent_apply_reserves_runtime_before_background_probe(self):
        request=self.ready_apply(source='USER')
        observer=self.claim(self.submit('servers:active-health','active','AUTO_SWITCH'))
        self.ack(observer)
        self.submit('servers:quality','background','SERVER_CHECK_AUTO')
        self.assertIsNone(self.call('queue-next')['requestId'],
                          'A lower-priority probe must not prolong the wait of an urgent runtime writer')
        self.finish(observer)
        self.assertEqual(self.call('queue-next')['requestId'],request['requestId'])

    def test_global_claim_reply_lost_before_queue_store(self):
        request = self.ready_apply()
        before = self.queue_file().read_bytes()
        nonce = uuid.uuid4().hex
        operation = self.claim(request, nonce)
        fence = (self.temp/'global.lock/owner.json').read_bytes()
        self.queue_file().write_bytes(before)
        self.assertEqual(self.claim(request, nonce), operation)
        self.assertEqual((self.temp/'global.lock/owner.json').read_bytes(), fence)
        self.ack(operation)
        self.finish(operation)

    def test_retired_step_reconciles_queue_after_lost_settlement(self):
        nonce = uuid.uuid4().hex
        operation = self.claim(self.submit(nonce=nonce))
        self.ack(operation)
        before = self.queue_file().read_bytes()
        self.finish(operation)
        self.queue_file().write_bytes(before)
        self.call('queue-recover')
        self.assertEqual(self.call('queue-lookup', nonce)['state'], 'completed')
        self.assertIsNone(self.call('queue-next')['requestId'])

    def test_terminal_missing_retirement_proof_preserves_queue(self):
        operation = self.claim(self.submit())
        self.ack(operation)
        before = self.queue_file().read_bytes()
        self.finish(operation)
        self.queue_file().write_bytes(before)
        self.opfile(operation, 'retired-lock').unlink()
        self.assertEqual(self.call('finish', operation['operationId'], operation['token'],
                                  'completed', '', expected=2)['errorCode'], 'OWNER_CHANGED')
        self.assertEqual(self.queue_file().read_bytes(), before)

    def test_never_acknowledged_global_recovery_settles_queue(self):
        request = self.ready_apply()
        operation = self.claim(request)
        self.set_owner({'status':'absent'})
        self.assertTrue(self.call('recover')['ok'])
        self.assertFalse((self.temp/'global.lock').is_symlink())
        self.assertFalse(any(row['requestId']==request['requestId']
                             for row in json.loads(self.queue_file().read_bytes())['requests']))
        self.assertTrue(self.call('queue-next')['ok'])

    def test_stop_background_requests_ram_step_cancel(self):
        operation = self.claim(self.submit())
        self.ack(operation)
        self.call('stop-background')
        self.assertTrue(self.opfile(operation, 'cancel.json').exists())
        self.assertTrue(json.loads(self.opfile(operation, 'cancel.json').read_bytes())['cancelRequested'])
        self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_ram_history_is_bounded_without_retiring_live_or_unknown_records(self):
        operation = self.claim(self.submit())
        self.ack(operation)
        self.finish(operation)
        root = self.temp/'ram/steps'
        original = self.opfile(operation, 'state.json').parent
        for number in range(25):
            identity = 'op-q-'+f'{number:032x}'
            target = root/identity
            shutil.copytree(original, target, symlinks=True)
            for name in ['owner.json', 'fence/owner.json', 'state.json']:
                path = target/name
                record = json.loads(path.read_bytes())
                record['operationId'] = identity
                path.write_text(json.dumps(record))
            (target/'retired-lock').unlink()
            (target/'retired-lock').symlink_to(target/'fence')
        unknown = root/'op-q-ffffffffffffffffffffffffffffffff'
        unknown.mkdir()
        (unknown/'state.json').write_bytes(b'{broken')
        self.set_owner({**self.owner,'startTicks':'101'})
        active = self.claim(self.submit())
        self.ack(active)
        self.assertLessEqual(len(list(root.glob('*/owner.json'))), 21)
        self.assertEqual((unknown/'state.json').read_bytes(), b'{broken')
        self.assertTrue(self.opfile(active, 'owner.json').exists())

    def test_ram_observer_can_finish_while_foreign_route_writer_remains_owned(self):
        operation = self.claim(self.submit('servers:active-health', 'active', 'AUTO_SWITCH'))
        self.ack(operation)
        # Existing route-domain fence must not be retired by an observer.
        app = self.temp/'live-app'
        route = app/'routes/locks/operation.lock'
        route.mkdir(parents=True)
        (route/'receipt').write_bytes(b'foreign-route-preserve')
        self.env['BRORAY_ROOT'] = str(app)
        self.env['BRORAY_OPS_CODE_ROOT'] = str(support.queue.support.APP)
        self.finish(operation)
        self.assertEqual((route/'receipt').read_bytes(), b'foreign-route-preserve')


if __name__ == '__main__':
    unittest.main(verbosity=2, failfast=True)
