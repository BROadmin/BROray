"""Local owner/operation tests. Windows uses a labelled guard adapter."""
import concurrent.futures
import json
import os
import subprocess
import tempfile
import unittest
import uuid
from pathlib import Path

REPO=Path(__file__).resolve().parents[1]
WORKSPACE=REPO.parent
BB=WORKSPACE/'.local/bin/busybox64u.exe' if os.name=='nt' else Path('/bin/busybox')
GUARD=WORKSPACE/'.local/bin/host-guard.exe' if os.name=='nt' else WORKSPACE/'.local/bin/linux-guard'
APP=REPO/'runtime/app'

class Operations(unittest.TestCase):
    def setUp(self):
        self.temp=Path(tempfile.mkdtemp(prefix='ops-test-',dir=WORKSPACE/'.local'))
        self.state=self.temp/'state'
        self.proc=self.temp/'proc'
        (self.proc/'sys/kernel/random').mkdir(parents=True)
        (self.proc/'sys/kernel/random/boot_id').write_text('boot-one\n')
        (self.proc/'uptime').write_text('1000.10 200.20\n')
        self.identities=self.temp/'identities.json'
        self.owner={'pid':900001,'startTicks':'100','bootId':'boot-one','executable':'/opt/bin/ash','commandDigest':'a'*64}
        self.set_owner(self.owner)
        self.env=os.environ.copy()
        self.env.update({
            'PATH':str(WORKSPACE/'.local/bin')+os.pathsep+self.env.get('PATH',''),
            'BRORAY_OPS_TEST':'1','BRORAY_ROOT':APP.as_posix(),
            'BRORAY_STATE_ROOT':self.state.as_posix(),
            'BRORAY_ROUTES_API_LOCK':(self.temp/'global.lock').as_posix(),
            'BRORAY_LEGACY_GLOBAL_LOCK':(self.temp/'legacy.lock').as_posix(),
            'BRORAY_OPS_UPDATER_ROOT':(self.temp/'updater').as_posix(),
            'BRORAY_OPS_PROC_ROOT':self.proc.as_posix(),
            'BRORAY_OPS_TEST_IDENTITIES':self.identities.as_posix(),
            'BRORAY_OPS_GUARD':GUARD.as_posix(),'BRORAY_OPS_ASH':BB.as_posix(),
            'BRORAY_OPS_TEST_NONCE':uuid.uuid4().hex,
            'BRORAY_OPS_RAM_ROOT':(self.temp/'ram').as_posix(),
        })
        self.state.mkdir()
    def set_owner(self,owner):
        self.identities.write_text(json.dumps({'900001':owner}))
    def call(self,*args,expected=0):
        command=[str(GUARD),str(self.state/'operations.guard'),str(BB),'ash',str(APP/'lib/operation-coordinator.sh'),*args]
        p=subprocess.run(command,env=self.env,capture_output=True,timeout=20)
        raw=p.stdout.decode('utf-8',errors='replace').strip()
        self.assertEqual(p.returncode,expected,(args,raw,p.stderr.decode('utf-8',errors='replace')))
        self.assertTrue(raw,(args,p.stderr))
        return json.loads(raw)
    def begin(self,mode='cooperative',source='USER',expected=0,ack=True,launch=None):
        self.launch=launch or uuid.uuid4().hex
        result=self.call('begin','system','subscriptions:scheduler','subscriptions',source,'900001',mode,self.launch,expected=expected)
        if expected==0 and ack:self.call('ack',result['operationId'],result['token'],'900001')
        return result
    def opfile(self,result,name):
        return self.state/'operations'/result['operationId']/name
    def test_normal_finish_and_next_begin(self):
        a=self.begin()
        self.call('finish',a['operationId'],a['token'],'completed','')
        self.assertFalse((self.temp/'global.lock').exists())
        self.env['BRORAY_OPS_TEST_NONCE']=uuid.uuid4().hex
        self.assertTrue(self.begin()['ok'])
    def test_live_owner_blocks_recovery_and_new_writer(self):
        self.begin()
        self.assertEqual(self.call('recover',expected=2)['result'],'ACTIVE')
        self.assertEqual(self.begin(expected=2)['errorCode'],'OPERATION_BUSY')
    def test_dead_owner_recovers(self):
        a=self.begin(); self.set_owner({'status':'absent'})
        self.assertEqual(self.call('recover')['result'],'recovered')
        self.assertEqual(json.loads(self.opfile(a,'state.json').read_text())['state'],'recovered')
        self.assertEqual(self.call('recover')['result'],'absent')
    def test_reused_pid_is_not_owner(self):
        a=self.begin(); self.set_owner({**self.owner,'startTicks':'999'})
        self.assertEqual(self.call('classify',a['operationId'])['reason'],'pid_reused')
        self.assertTrue(self.call('recover')['ok'])
    def test_unreadable_owner_fails_closed(self):
        self.begin(); self.set_owner({'status':'unreadable'})
        self.assertEqual(self.call('recover',expected=2)['result'],'AMBIGUOUS')
        self.assertTrue((self.temp/'global.lock').exists())
    def test_changed_command_fails_closed(self):
        self.begin(); self.set_owner({**self.owner,'commandDigest':'b'*64})
        self.assertEqual(self.call('recover',expected=2)['result'],'AMBIGUOUS')
    def test_previous_boot_recovers(self):
        self.begin(); (self.proc/'sys/kernel/random/boot_id').write_text('boot-two\n')
        self.assertEqual(self.call('recover')['result'],'recovered')
    def test_missing_boot_fails_closed(self):
        self.begin(); (self.proc/'sys/kernel/random/boot_id').unlink()
        self.assertEqual(self.call('recover',expected=2)['result'],'AMBIGUOUS')
    def test_stale_heartbeat_does_not_remove_live_lock(self):
        a=self.begin(); self.opfile(a,'heartbeat.json').write_text('{"heartbeatAt":"2000-01-01T00:00:00Z"}')
        self.assertEqual(self.call('recover',expected=2)['result'],'ACTIVE')
    def test_cancel_is_idempotent(self):
        a=self.begin()
        self.assertTrue(self.call('cancel',a['operationId'])['cancelRequested'])
        self.assertTrue(self.call('cancel',a['operationId'])['cancelRequested'])
        self.assertTrue(self.call('status')['operations'][0]['cancelRequested'])
    def test_protected_operation_cannot_cancel_or_auto_recover(self):
        a=self.begin('protected')
        self.assertEqual(self.call('cancel',a['operationId'],expected=2)['errorCode'],'CANCEL_NOT_SUPPORTED')
        self.set_owner({'status':'absent'})
        self.assertEqual(self.call('recover',expected=2)['result'],'protected_recovery')
    def test_foreign_token_cannot_release(self):
        a=self.begin()
        self.assertEqual(self.call('finish',a['operationId'],'b'*32,'completed','',expected=2)['errorCode'],'OWNER_CHANGED')
        self.assertTrue((self.temp/'global.lock').exists())
    def test_heartbeat_after_terminal_cannot_resurrect(self):
        a=self.begin(); self.call('finish',a['operationId'],a['token'],'completed','')
        self.assertEqual(self.call('tick',a['operationId'],a['token'],'working',expected=2)['errorCode'],'OPERATION_FINISHED')
    def test_changed_lock_generation_preserved(self):
        a=self.begin(); lock=self.temp/'global.lock/owner.json'
        changed=json.loads(lock.read_text()); changed['token']='b'*32; lock.write_text(json.dumps(changed))
        self.assertEqual(self.call('finish',a['operationId'],a['token'],'completed','',expected=2)['errorCode'],'OWNER_CHANGED')
    def test_live_child_prevents_cleanup(self):
        a=self.begin(); self.opfile(a,'children.json').write_text(json.dumps({'children':[self.owner]}))
        self.set_owner({**self.owner,'startTicks':'999'})
        # Parent birth is stale, but child below is the current process.
        self.opfile(a,'children.json').write_text(json.dumps({'children':[{**self.owner,'startTicks':'999'}]}))
        self.assertEqual(self.call('recover',expected=2)['result'],'children_unconfirmed')
    def test_legacy_five_file_lock_needs_preflight(self):
        lock=self.temp/'global.lock'; lock.mkdir()
        for k,v in {'pid':'900001','scope':'system','action':'auto-switch','bundle':'','startedAt':'2020'}.items(): (lock/k).write_text(v+'\n')
        self.set_owner({'status':'absent'})
        self.assertEqual(self.call('recover',expected=2)['result'],'legacy_owner_ambiguous')
    def test_ownerless_lock_not_deleted(self):
        (self.temp/'global.lock').mkdir()
        self.assertEqual(self.call('recover',expected=2)['result'],'legacy_owner_ambiguous')
    def test_updater_request_fence_preserved(self):
        (self.temp/'updater/request.lock').mkdir(parents=True)
        self.assertEqual(self.begin(expected=2)['errorCode'],'DOMAIN_OPERATION_BUSY')
    def test_automation_pause_keeps_manual_admission(self):
        self.call('pause')
        self.assertEqual(self.begin(source='SUBSCRIPTION_AUTO',expected=2)['errorCode'],'AUTOMATION_PAUSED')
        self.assertTrue(self.begin()['ok'])
    def test_pause_resume(self):
        self.call('pause'); self.assertTrue(self.call('status')['automationPaused'])
        self.call('resume'); self.assertFalse(self.call('status')['automationPaused'])
        self.assertTrue(self.begin(source='SUBSCRIPTION_AUTO')['ok'])
    def test_malformed_state_never_looks_idle(self):
        a=self.begin(); self.opfile(a,'state.json').write_text('broken')
        self.assertFalse(self.call('status')['ok'])
    def test_path_traversal_rejected(self):
        self.assertEqual(self.call('cancel','../../unrelated',expected=1)['errorCode'],'STATE_UNAVAILABLE')
    def test_public_status_has_no_owner_token_or_command_digest(self):
        a=self.begin(); data=self.call('status')
        raw=json.dumps(data)
        self.assertNotIn(a['token'],raw)
        self.assertNotIn(self.owner['commandDigest'],raw)
    def test_new_fence_is_complete_atomic_symlink(self):
        a=self.begin();lock=self.temp/'global.lock'
        self.assertTrue(lock.is_symlink())
        self.assertEqual(lock.readlink(),self.opfile(a,'fence'))
        self.assertEqual({p.name for p in lock.iterdir()},{'pid','scope','action','bundle','startedAt','owner.json'})
    def test_real_entropy_produces_distinct_private_tokens(self):
        self.env.pop('BRORAY_OPS_TEST_NONCE')
        a=self.begin();self.call('finish',a['operationId'],a['token'],'completed','')
        b=self.begin()
        self.assertRegex(a['token'],r'^[a-f0-9]{32}$');self.assertRegex(b['token'],r'^[a-f0-9]{32}$')
        self.assertNotEqual(a['token'],b['token']);self.assertNotEqual(a['operationId'],b['operationId'])
    def test_old_updater_preserves_new_fence(self):
        self.begin();lock=self.temp/'global.lock';before=lock.readlink()
        src=(APP/'share/updater-platform/opt/libexec/broray-updater/broray-updater.sh').read_text()
        body=src[src.index('global_operation_lock_classify()'):src.index('conflicting_operation_admission_clear()')]
        env={**self.env,'GLOBAL_OPERATION_LOCK':str(lock)}
        p=subprocess.run([str(BB),'ash','-c',body+'\nrc=0; global_operation_lock_classify || rc=$?; printf "%s:%s" "$rc" "$GLOBAL_OPERATION_LOCK_STATE"'],env=env,capture_output=True,timeout=10)
        self.assertEqual(p.stdout,b'1:unsafe-object')
        self.assertEqual(lock.readlink(),before)
    def test_terminal_result_survives_interrupted_retirement(self):
        a=self.begin();file=self.opfile(a,'state.json');state=json.loads(file.read_text())
        state.update(state='completed',running=False,phase='finished',errorCode=None)
        file.write_text(json.dumps(state))
        self.assertEqual(self.call('recover')['result'],'terminal_lock_retired')
        self.assertEqual(json.loads(file.read_text())['state'],'completed')
    def test_journal_records_start_cancel_and_completion(self):
        a=self.begin();self.call('cancel',a['operationId']);self.call('finish',a['operationId'],a['token'],'aborted','CANCELLED')
        events=self.call('events')['events']
        self.assertEqual([e['event'] for e in events],['lock_acquired','started','cancel_requested','aborted'])
        self.assertNotIn(a['token'],json.dumps(events))
    def test_rotation_is_bounded_and_returns_valid_projected_events(self):
        a=self.begin();journal=self.state/'operation-events';f=journal/'events.jsonl'
        row=json.dumps({'event':'started','message':'SECRET_CANARY_DO_NOT_EMIT'})+'\n'
        f.write_text(row*(262144//len(row)))
        self.call('cancel',a['operationId'])
        self.assertTrue((journal/'events.1.jsonl').is_file())
        events=self.call('events')['events']
        self.assertLessEqual(len(events),500)
        self.assertNotIn('SECRET_CANARY_DO_NOT_EMIT',json.dumps(events))
        self.assertTrue(all(p.stat().st_size<=262144 for p in journal.iterdir()))
    def test_foreign_symlink_does_not_expose_or_remove_target(self):
        target=self.temp/'foreign';target.mkdir();(target/'owner.json').write_text('SECRET_CANARY')
        (self.temp/'global.lock').symlink_to(target,target_is_directory=True)
        self.assertEqual(self.call('recover',expected=2)['result'],'unsafe_lock')
        self.assertEqual((target/'owner.json').read_text(),'SECRET_CANARY')

    def test_lost_begin_response_returns_same_launch(self):
        a=self.begin(ack=False); nonce=self.launch
        b=self.begin(ack=False,launch=nonce)
        self.assertEqual(a,b)
        self.assertEqual(len(list((self.state/'operations').iterdir())),1)
        state=json.loads(self.opfile(a,'state.json').read_text())
        self.assertEqual(state['state'],'starting');self.assertFalse(state['acknowledged'])

    def test_ack_repeated_after_lost_response_does_not_duplicate_start(self):
        a=self.begin(); before=self.opfile(a,'state.json').read_bytes()
        self.call('ack',a['operationId'],a['token'],'900001')
        self.assertEqual(self.opfile(a,'state.json').read_bytes(),before)
        self.assertEqual(sum(e['event']=='started' for e in self.call('events')['events']),1)

    def test_cancel_before_ack_does_not_admit_work(self):
        a=self.begin(ack=False);self.call('cancel',a['operationId'])
        self.assertEqual(self.call('ack',a['operationId'],a['token'],'900001',expected=2)['errorCode'],'CANCELLED')
        self.assertFalse(json.loads(self.opfile(a,'state.json').read_text())['acknowledged'])

    def test_ack_rejects_reused_pid(self):
        a=self.begin(ack=False);self.set_owner({**self.owner,'startTicks':'999'})
        self.assertEqual(self.call('ack',a['operationId'],a['token'],'900001',expected=2)['errorCode'],'OWNER_CHANGED')

    def test_same_launch_rejects_different_parameters(self):
        self.begin(ack=False); nonce=self.launch
        self.assertEqual(self.begin(mode='protected',launch=nonce,expected=2)['errorCode'],'LAUNCH_MISMATCH')

    def test_dead_unacknowledged_protected_launch_can_recover(self):
        self.begin(mode='protected',ack=False);self.set_owner({'status':'absent'})
        self.assertEqual(self.call('recover')['result'],'recovered')

    def test_retries_unpublished_launch(self):
        a=self.begin(ack=False); nonce=self.launch;(self.temp/'global.lock').unlink()
        self.assertEqual(self.begin(launch=nonce),a)
        self.assertTrue((self.temp/'global.lock').is_symlink())

    def test_dead_unpublished_launch_recovers(self):
        a=self.begin(ack=False);(self.temp/'global.lock').unlink();self.set_owner({'status':'absent'})
        self.assertTrue(self.call('recover')['ok'])
        self.assertEqual(json.loads(self.opfile(a,'state.json').read_text())['state'],'recovered')

    def test_live_orphan_is_not_reported_recovered(self):
        self.begin(ack=False);(self.temp/'global.lock').unlink()
        self.assertEqual(self.call('recover',expected=2)['result'],'orphan_unconfirmed')

    def test_finish_retry_does_not_remove_next_generation(self):
        a=self.begin();self.call('finish',a['operationId'],a['token'],'completed','')
        b=self.begin();target=(self.temp/'global.lock').readlink()
        self.assertTrue(self.call('finish',a['operationId'],a['token'],'failed','OPERATION_FAILED')['alreadyFinished'])
        self.assertEqual((self.temp/'global.lock').readlink(),target)
        self.assertEqual(json.loads(self.opfile(a,'state.json').read_text())['state'],'completed')
        self.assertTrue(json.loads(self.opfile(b,'state.json').read_text())['running'])

    def test_heartbeat_writes_ram_without_flash_revision_churn(self):
        a=self.begin();self.call('tick',a['operationId'],a['token'],'checking')
        before=self.opfile(a,'state.json').read_bytes();events=self.call('events')['events']
        self.call('tick',a['operationId'],a['token'],'checking')
        self.assertEqual(self.opfile(a,'state.json').read_bytes(),before)
        self.assertEqual(self.call('events')['events'],events)
        self.assertFalse(self.opfile(a,'heartbeat.json').exists())
        self.assertEqual(json.loads((self.temp/'ram'/f"{a['operationId']}.json").read_text())['bootId'],'boot-one')

    def test_cancellation_wins_before_commit_boundary(self):
        a=self.begin();self.call('cancel',a['operationId'])
        self.assertEqual(self.call('tick',a['operationId'],a['token'],'committing',expected=2)['errorCode'],'CANCELLED')
        self.assertEqual(json.loads(self.opfile(a,'state.json').read_text())['cancelability'],'cooperative')

    def test_commit_boundary_protects_until_next_phase(self):
        a=self.begin();self.call('tick',a['operationId'],a['token'],'committing')
        self.assertEqual(self.call('cancel',a['operationId'],expected=2)['errorCode'],'CANCEL_NOT_SUPPORTED')
        self.call('tick',a['operationId'],a['token'],'fetching')
        self.assertTrue(self.call('cancel',a['operationId'])['cancelRequested'])

    def test_cancel_retry_does_not_duplicate_event(self):
        a=self.begin();self.call('cancel',a['operationId']);before=self.opfile(a,'cancel.json').read_bytes()
        self.call('cancel',a['operationId'])
        self.assertEqual(self.opfile(a,'cancel.json').read_bytes(),before)
        self.assertEqual(sum(e['event']=='cancel_requested' for e in self.call('events')['events']),1)

    def test_journal_partial_line_is_reported_without_raw_fallback(self):
        a=self.begin();journal=self.state/'operation-events/events.jsonl'
        with journal.open('a') as f:f.write('{"message":"SECRET_CANARY_UNFINISHED')
        result=self.call('events')
        self.assertFalse(result['complete']);self.assertEqual(result['errors'],['JOURNAL_GAP'])
        self.assertNotIn('SECRET_CANARY_UNFINISHED',json.dumps(result))
        self.assertEqual(len(result['events']),2)

    def test_journal_failure_does_not_hold_terminal_fence(self):
        a=self.begin();journal=self.state/'operation-events/events.jsonl';journal.unlink();journal.mkdir()
        self.assertTrue(self.call('finish',a['operationId'],a['token'],'completed','')['ok'])
        self.assertFalse((self.temp/'global.lock').exists())
        self.assertTrue((self.temp/'ram/journal-gap').exists())

    def test_report_excludes_secrets_and_marks_unavailable_components(self):
        a=self.begin();result=self.call('report');raw=json.dumps(result)
        self.assertEqual(result['reportKind'],'broray-diagnostics')
        self.assertFalse(result['complete']);self.assertIn('serviceIdentities',result['unavailable'])
        self.assertNotIn(a['token'],raw);self.assertNotIn(self.launch,raw);self.assertNotIn(self.owner['commandDigest'],raw)
        self.assertEqual(result['fences']['global'],'managed_active')
        self.assertLess(len(raw.encode()),1048576)

    def test_status_report_and_events_leave_registry_bytes_unchanged(self):
        self.begin()
        def snapshot():return {str(p.relative_to(self.state)):p.read_bytes() for p in self.state.rglob('*') if p.is_file() and not p.is_symlink()}
        before=snapshot();self.call('status');self.call('events');self.call('report')
        self.assertEqual(snapshot(),before)

    def test_legacy_lock_is_not_presented_as_idle(self):
        self.call('pause');(self.temp/'global.lock').mkdir()
        result=self.call('status');self.assertFalse(result['ok']);self.assertEqual(result['globalFence'],'ambiguous')

    def test_stop_background_pauses_and_cancels_current_job(self):
        a=self.begin();result=self.call('stop-background')
        self.assertTrue(result['automationPaused']);self.assertTrue(result['operations'][0]['cancelRequested'])
        self.call('finish',a['operationId'],a['token'],'aborted','CANCELLED')
        self.assertEqual(self.begin(source='SUBSCRIPTION_AUTO',expected=2)['errorCode'],'AUTOMATION_PAUSED')
        self.assertTrue(self.begin()['ok'])

    def test_stop_background_preserves_protected_commit(self):
        a=self.begin();self.call('tick',a['operationId'],a['token'],'committing')
        result=self.call('stop-background')
        self.assertTrue(result['automationPaused']);self.assertTrue(result['operations'][0]['protected'])
        self.assertFalse(self.opfile(a,'cancel.json').exists())

    def test_history_prunes_only_confirmed_dead_terminal_owners(self):
        a=self.begin();self.call('finish',a['operationId'],a['token'],'completed','')
        template_state=json.loads(self.opfile(a,'state.json').read_text())
        template_owner=json.loads(self.opfile(a,'owner.json').read_text())
        root=self.state/'operations'
        for index in range(25):
            id=f'op-20000101000000-900001-{index:012x}';d=root/id;d.mkdir()
            (d/'state.json').write_text(json.dumps({**template_state,'operationId':id}))
            (d/'owner.json').write_text(json.dumps({**template_owner,'operationId':id}))
        self.set_owner({**self.owner,'startTicks':'999'})
        self.begin()
        self.assertEqual(len(list(root.iterdir())),21)

    def test_history_prune_preserves_live_and_corrupt_records(self):
        a=self.begin();self.call('finish',a['operationId'],a['token'],'completed','')
        state=json.loads(self.opfile(a,'state.json').read_text());owner=json.loads(self.opfile(a,'owner.json').read_text())
        root=self.state/'operations'
        for index in range(25):
            id=f'op-20000101000000-900001-{index:012x}';d=root/id;d.mkdir()
            (d/'state.json').write_text(json.dumps({**state,'operationId':id}))
            (d/'owner.json').write_text(json.dumps({**owner,'operationId':id}))
        self.begin();self.assertEqual(len(list(root.iterdir())),27)

if __name__=='__main__':
    if os.name=='nt':raise SystemExit('Atomic publication requires Linux. Use tools/run_linux_tests.py; stage02 contains the earlier Windows-adapter evidence.')
    suite=unittest.defaultTestLoader.loadTestsFromTestCase(Operations)
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(suite)
    report={'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),'environment':('Windows guard adapter' if os.name=='nt' else 'Linux native fcntl guard')+' + BusyBox + synthetic process identities','linuxKernelLockTests':'SEPARATE_SUITE','routerAccessed':False}
    (WORKSPACE/'docs/evidence/stage02-operations-tests.json').write_text(json.dumps(report,indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
