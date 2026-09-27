"""Queued active checks: actual Linux owner/runtime identity, fixture network."""
import ctypes
import hashlib
import json
import subprocess
import time
import unittest
import uuid
import shutil
from pathlib import Path
import test_auto_switch_jobs as automatic
import test_server_jobs as servers
from test_operation_resources import disable_jq_regex


class AutoSwitchQueue(unittest.TestCase):
    setUp = automatic.AutoSwitchJobs.setUp
    collect = servers.ServerJobs.collect
    reap_adopted_helpers = servers.ServerJobs.reap_adopted_helpers
    active_runtime_fixture = automatic.AutoSwitchJobs.active_runtime_fixture
    set_config = automatic.AutoSwitchJobs.set_config
    auto_state = automatic.AutoSwitchJobs.auto_state
    shell = servers.ServerJobs.shell

    def context(self):
        p=self.shell('. "$BRORAY_ROOT/lib/server-service.sh"; . "$BRORAY_ROOT/lib/active-proxy-health.sh"; broray_active_proxy_context "$TEST_ACTIVE_ID"')
        return p.stdout.decode().strip()

    def test_active_health_without_jq_regex(self):
        disable_jq_regex(self)
        self.test_failure_threshold_queues_one_context_bound_failover()

    def test_failover_policy_without_jq_regex(self):
        disable_jq_regex(self)
        self.test_failover_retries_after_attempt_guard_not_switch_cooldown()

    def test_protected_activation_without_jq_regex(self):
        disable_jq_regex(self)
        self.test_prepared_activation_commits_known_bytes()

    def health(self):
        context=self.context()
        nonce=uuid.uuid4().hex
        response=self.shell('. "$BRORAY_ROOT/lib/operation-job.sh"; broray_ops_queue_submit servers:active-health "$TEST_ACTIVE_ID" AUTO_SWITCH '+context+' '+nonce)
        request=json.loads(response.stdout)
        worker=subprocess.Popen(['/bin/ash',str(self.app/'lib/operation-worker.sh'),'--request',request['requestId']],
                                env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        out,err=self.collect(worker,60)
        self.assertEqual(worker.returncode,0,(out,err))
        result=json.loads(self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_queue_lookup '+nonce).stdout)
        self.assertEqual(result['state'],'completed')
        return self.auto_state()

    def test_active_health_queue_samples_current_proxy(self):
        self.set_config(enabled=True,failureThreshold=3)
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.env.update(TEST_ACTIVE_ID=self.server,TEST_ACTIVE_MODE='working')
        self.active_runtime_fixture()
        before=self.context()
        peer={'qualityRefresh':{'status':'running','checkedCount':2,'totalCount':8},
              'lastSwitchEpoch':123,'lastSwitchTo':'preserve-peer'}
        cache=self.app/'run/server-auto-switch-state.json'
        cache.write_text(json.dumps(peer));cache.chmod(0o600)
        result=self.health()
        self.assertEqual(result['activeHealth']['status'],'healthy')
        self.assertEqual(result['activeHealth']['context'],before)
        self.assertEqual(result['consecutiveFailures'],0)
        self.assertEqual(result['status'],'healthy')
        self.assertEqual(self.context(),before,'Active check changed Xray identity/config')
        self.assertFalse((self.temp/'global.lock').is_symlink())
        for key,value in peer.items(): self.assertEqual(result[key],value)

    def test_failure_samples_increment_once_and_success_resets(self):
        self.set_config(enabled=True,failureThreshold=3)
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.env.update(TEST_ACTIVE_ID=self.server,TEST_ACTIVE_MODE='timeout')
        self.active_runtime_fixture()
        before=self.context()
        first=self.health()
        self.assertEqual(first['activeHealth']['status'],'unhealthy')
        self.assertEqual(first['consecutiveFailures'],1)
        self.assertEqual(first['status'],'waiting-threshold')
        second=self.health()
        self.assertEqual(second['consecutiveFailures'],2)
        self.assertNotEqual(first['backgroundOperationId'],second['backgroundOperationId'])
        self.env['TEST_ACTIVE_MODE']='working'
        third=self.health()
        self.assertEqual(third['consecutiveFailures'],0)
        self.assertEqual(third['status'],'healthy')
        self.assertEqual(self.context(),before)

    def test_failure_threshold_queues_one_context_bound_failover(self):
        self.set_config(enabled=True,failureThreshold=2)
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.env.update(TEST_ACTIVE_ID=self.server,TEST_ACTIVE_MODE='timeout')
        self.active_runtime_fixture()
        context=self.context()
        boot=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
        queue_file=self.temp/'ram/queue'/boot/'state.json'
        self.health()
        self.assertFalse(any(r['action']=='servers:failover' for r in json.loads(queue_file.read_bytes())['requests']))
        self.health()
        waiting=[r for r in json.loads(queue_file.read_bytes())['requests'] if r['action']=='servers:failover']
        self.assertEqual(len(waiting),1)
        self.assertEqual(waiting[0]['priority'],1)
        self.assertEqual(waiting[0]['context'],context)
        self.assertEqual(waiting[0]['targetId'],self.server)
        self.assertEqual(waiting[0]['stage'],'verify')
        self.health()
        repeated=[r for r in json.loads(queue_file.read_bytes())['requests'] if r['action']=='servers:failover']
        self.assertEqual([r['requestId'] for r in repeated],[waiting[0]['requestId']])

    def failover_request(self):
        return json.loads(self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_queue_next').stdout)

    def failover_stage(self, request, expected=0):
        worker=subprocess.Popen(['/bin/ash',str(self.app/'lib/operation-worker.sh'),'--request',request['requestId']],
                                env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        out,err=self.collect(worker,90)
        details=self.activation_diagnostics() if worker.returncode!=expected else []
        self.assertEqual(worker.returncode,expected,(out,err,details))

    def activation_diagnostics(self):
        names={'activate-output','activate-error','rollback-output','rollback-error','prepare-output','prepare-error','state.json','supervisors.json',
               'server-auto-switch-state.json','result.json'}
        return [(str(p.relative_to(self.temp)),p.read_text(errors='replace')[-6000:])
                for p in self.temp.rglob('*') if p.is_file() and not p.is_symlink() and p.name in names
                and ('operations' in p.parts or 'requests' in p.parts or 'prepared' in p.parts)]

    def setup_failover(self):
        self.set_config(enabled=True,failureThreshold=1,selectionRule='preferred',
                        preferredServerId='subscription-second-0000')
        self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$(cat "$TEST_PAYLOAD")" subscription second 0')
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.env.update(TEST_ACTIVE_ID=self.server,TEST_ACTIVE_MODE='timeout')
        self.active_runtime_fixture()
        self.health()
        request=self.failover_request()
        self.assertEqual(request['priority'],1)
        return request

    def test_recovered_active_retires_failover_without_candidate_probe(self):
        request=self.setup_failover()
        before=self.context()
        self.env['TEST_ACTIVE_MODE']='working'
        self.failover_stage(request)
        self.assertIsNone(self.failover_request()['requestId'])
        self.assertEqual(self.context(),before)
        self.assertFalse(Path(self.env['TEST_XRAY_PID']).exists())
        self.assertFalse((self.temp/'global.lock').exists())

    def test_failover_retries_after_attempt_guard_not_switch_cooldown(self):
        request=self.setup_failover()
        cache=self.app/'run/server-auto-switch-state.json'
        data=json.loads(cache.read_bytes())
        data.update(lastAttemptEpoch=int(time.time())-61,lastSwitchEpoch=0)
        cache.write_text(json.dumps(data))
        self.failover_stage(request)
        self.assertEqual(self.failover_request()['requestId'],request['requestId'])
        self.assertEqual(self.failover_request()['stage'],'probe')
        self.assertFalse((self.app/'restarts').exists())

    def test_recent_successful_switch_enforces_configured_cooldown(self):
        request=self.setup_failover()
        cache=self.app/'run/server-auto-switch-state.json'
        data=json.loads(cache.read_bytes())
        data.update(lastAttemptEpoch=0,lastSwitchEpoch=int(time.time())-120)
        cache.write_text(json.dumps(data))
        self.failover_stage(request)
        self.assertIsNone(self.failover_request()['requestId'])
        self.assertFalse(Path(self.env['TEST_XRAY_PID']).exists())
        self.assertFalse((self.app/'restarts').exists())

    def test_failover_verify_yields_bound_candidate_snapshot(self):
        request=self.setup_failover()
        before=self.context()
        self.failover_stage(request)
        waiting=self.failover_request()
        self.assertEqual(waiting['requestId'],request['requestId'])
        self.assertEqual(waiting['stage'],'probe')
        result=json.loads((self.temp/'ram/requests'/request['requestId']/'result.json').read_bytes())
        self.assertEqual(result['context'],before)
        self.assertEqual(result['activeServerId'],self.server)
        self.assertEqual(result['cursor'],0)
        self.assertEqual([r['id'] for r in result['servers']],['subscription-second-0000'])
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
        self.assertEqual(self.context(),before)

    def test_failover_verify_publishes_attempt_preserving_observer_fields(self):
        request=self.setup_failover()
        cache=self.app/'run/server-auto-switch-state.json'
        before=json.loads(cache.read_bytes())
        before['qualityRefresh']={'status':'running','checkedCount':3,'totalCount':8}
        cache.write_text(json.dumps(before))
        started=int(time.time())
        self.failover_stage(request)
        after=json.loads(cache.read_bytes())
        self.assertEqual(after['activeHealth'],before['activeHealth'])
        self.assertEqual(after['consecutiveFailures'],before['consecutiveFailures'])
        self.assertEqual(after['qualityRefresh'],before['qualityRefresh'])
        self.assertEqual(after['failover']['requestId'],request['requestId'])
        self.assertEqual(after['failover']['status'],'probing')
        self.assertGreaterEqual(after['failover']['lastAttemptEpoch'],started)
        self.assertLessEqual(after['failover']['lastAttemptEpoch'],int(time.time()))

    def test_disabling_auto_switch_rejects_queued_failover(self):
        request=self.setup_failover()
        before=self.context()
        self.set_config(enabled=False)
        self.failover_stage(request,expected=76)
        self.assertIsNone(self.failover_request()['requestId'])
        self.assertEqual(self.context(),before)
        self.assertFalse(Path(self.env['TEST_XRAY_PID']).exists())
        self.assertFalse((self.temp/'global.lock').exists())

    def test_manual_off_rejects_queued_failover_without_restart(self):
        request=self.setup_failover()
        before=self.current.read_bytes()
        (self.app/'config/active-server').unlink()
        self.failover_stage(request,expected=76)
        self.assertFalse((self.app/'config/active-server').exists())
        self.assertEqual(self.current.read_bytes(),before)
        self.assertFalse(Path(self.env['TEST_XRAY_PID']).exists())
        self.assertFalse((self.temp/'global.lock').exists())

    def test_failover_probe_checks_one_preferred_candidate_then_yields(self):
        request=self.setup_failover()
        before=self.context()
        self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$(cat "$TEST_PAYLOAD")" subscription third 0')
        self.failover_stage(request)
        self.failover_stage(request)
        result=json.loads((self.temp/'ram/requests'/request['requestId']/'result.json').read_bytes())
        self.assertEqual(result['cursor'],1)
        self.assertEqual(result['status'],'preparing')
        self.assertEqual(result['selected']['id'],'subscription-second-0000')
        self.assertEqual(len(list((self.app/'run/server-quality').glob('*.json'))),1)
        self.assertEqual(self.failover_request()['stage'],'probe')
        self.assertEqual(self.context(),before)
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
        self.health()
        self.assertEqual(self.failover_request()['requestId'],request['requestId'])
        self.assertEqual(self.context(),before)

    def test_failover_changed_candidate_rejected_before_probe(self):
        request=self.setup_failover()
        before=self.context()
        self.failover_stage(request)
        node=self.app/'servers/subscription-second-0000.json'
        data=json.loads(node.read_bytes());data['uuid']='11111111-2222-4333-8444-666666666666'
        node.write_text(json.dumps(data))
        self.failover_stage(request,expected=76)
        self.assertEqual(list((self.app/'run/server-quality').glob('*.json')),[])
        self.assertEqual(self.context(),before)
        self.assertFalse((self.temp/'global.lock').exists())

    def test_failover_corrupt_continuation_rejected_before_probe(self):
        request=self.setup_failover()
        self.failover_stage(request)
        result=self.temp/'ram/requests'/request['requestId']/'result.json'
        result.write_bytes(b'{"cursor":9999}')
        self.failover_stage(request,expected=76)
        self.assertEqual(result.read_bytes(),b'{"cursor":9999}')
        self.assertFalse(Path(self.env['TEST_XRAY_PID']).exists())
        self.assertFalse((self.temp/'global.lock').exists())

    def track_post_prepare_health(self, recovered=False):
        self.env['TEST_PREPARE_DONE']=str(self.temp/'prepare-finished')
        self.env['TEST_POST_PREPARE_HEALTH']=str(self.temp/'post-prepare-health')
        self.env['TEST_RECOVER_AFTER_PREPARE']='yes' if recovered else 'no'
        prepare=self.app/'lib/server-activate-prepare.sh'
        prepare.write_text(prepare.read_text()+'\ndate +%s >"$TEST_PREPARE_DONE"\n')
        curl=self.app/'bin/curl';original=curl.read_text()
        curl.write_text('''#!/bin/ash
case "$*" in
  *'--proxy socks5h://127.0.0.1:2080'*)
    if [ -f "$TEST_PREPARE_DONE" ]; then
      date +%s >"$TEST_POST_PREPARE_HEALTH"
      [ "$TEST_RECOVER_AFTER_PREPARE" != yes ] || export TEST_ACTIVE_MODE=working
    fi ;;
esac
'''+original.removeprefix('#!/bin/ash\n'))

    def test_failover_prepares_exact_config_before_global_activation(self):
        request=self.setup_failover()
        self.track_post_prepare_health()
        before_context=self.context()
        before_config=self.current.read_bytes()
        self.failover_stage(request)
        self.failover_stage(request)
        self.failover_stage(request)
        self.assertEqual(self.failover_request()['stage'],'activate')
        directory=self.temp/'ram/requests'/request['requestId']
        result=json.loads((directory/'result.json').read_bytes())
        self.assertEqual(result['status'],'prepared')
        self.assertEqual((directory/'previous-runtime.json').read_bytes(),before_config)
        self.assertEqual(json.loads((directory/'new-runtime.json').read_bytes())['outbounds'][0]['protocol'],'vless')
        self.assertEqual(self.current.read_bytes(),before_config)
        self.assertEqual(self.context(),before_context)
        self.assertFalse((self.temp/'global.lock').exists())
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
        self.assertTrue((self.temp/'post-prepare-health').exists(),
                        'Prepared activation must carry a fresh sample taken after config validation')
        self.assertGreaterEqual(result['activeHealth']['checkedEpoch'],
                                int((self.temp/'prepare-finished').read_text()))
        self.assertEqual(self.auto_state()['failover']['activeHealth'],result['activeHealth'])

    def test_recovery_during_preparation_prevents_activation(self):
        request=self.setup_failover()
        before_context=self.context();before_config=self.current.read_bytes()
        self.track_post_prepare_health(recovered=True)
        for _ in range(3):self.failover_stage(request)
        self.assertIsNone(self.failover_request()['requestId'])
        self.assertTrue((self.temp/'post-prepare-health').exists())
        self.assertEqual(self.auto_state()['failover']['status'],'recovered')
        self.assertEqual(self.current.read_bytes(),before_config)
        self.assertEqual(self.context(),before_context)
        self.assertFalse((self.app/'restarts').exists())
        self.assertFalse((self.temp/'global.lock').exists())

    def test_preparation_validates_with_bound_runtime_binary(self):
        request=self.setup_failover()
        self.failover_stage(request)
        self.failover_stage(request)
        self.health()
        before_context=self.context()
        before_config=self.current.read_bytes()
        wrong=self.temp/'unconfigured-validator'
        wrong.write_text('#!/bin/ash\necho WRONG_VALIDATOR >"$TEST_WRONG_VALIDATOR"\necho WRONG_VALIDATOR >&2\nexit 97\n')
        wrong.chmod(0o755)
        self.env.update(BRORAY_XRAY_BINARY=str(wrong),
                        TEST_WRONG_VALIDATOR=str(self.temp/'wrong-validator-called'))
        self.failover_stage(request)
        self.assertEqual(self.failover_request()['stage'],'activate')
        self.assertFalse((self.temp/'wrong-validator-called').exists())
        self.assertEqual(self.current.read_bytes(),before_config)
        self.assertEqual(self.context(),before_context)
        directory=self.temp/'ram/requests'/request['requestId']
        result=json.loads((directory/'result.json').read_bytes())
        self.assertEqual(result['prepared']['xraySha256'],
                         hashlib.sha256((self.app/'runtime/xray').read_bytes()).hexdigest())

    def failover_init_fixture(self):
        init=self.app/'bin/fixture-init'
        init.write_text('''#!/bin/ash
jq -e '.phase=="switching" and .cancelability=="protected" and .resourceLocks==["global"]' "$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/state.json" >/dev/null || exit 91
ledger="$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/supervisors.json"
# This new protected stage never registers a helper. As in the coordinator,
# absent ledger is empty; an existing ledger must prove no supervisors.
if [ -e "$ledger" ] || [ -L "$ledger" ]; then
  [ ! -L "$ledger" ] && jq -e '.supervisors==[]' "$ledger" >/dev/null || exit 92
fi
for resource in active-observer background-prepare; do
  [ ! -e "$BRORAY_OPS_RAM_ROOT/resources/$resource" ] && [ ! -L "$BRORAY_OPS_RAM_ROOT/resources/$resource" ] || exit 93
done
jq -r '.inbounds[0].port' "$BRORAY_ROOT/config/config.json" >"$TEST_PORT"
echo restart >>"$BRORAY_ROOT/restarts"
''')
        init.chmod(0o755);self.env['BRORAY_INIT']=str(init)

    def test_failover_activates_only_prepared_config_under_global_owner(self):
        request=self.setup_failover()
        self.failover_init_fixture()
        for _ in range(3):self.failover_stage(request)
        directory=self.temp/'ram/requests'/request['requestId']
        prepared=(directory/'new-runtime.json').read_bytes()
        # The service normally continues P0 while P1 prepares. Exercise the
        # real observer here; never extend the 120s authorization window.
        refreshed=self.health()
        self.assertEqual(refreshed['activeHealth']['status'],'unhealthy')
        self.assertEqual(self.failover_request()['requestId'],request['requestId'])
        self.failover_stage(request)
        self.assertEqual(self.current.read_bytes(),prepared)
        self.assertEqual((self.app/'config/active-server').read_text().strip(),'subscription-second-0000')
        self.assertEqual((self.app/'restarts').read_text(),'restart\n')
        self.assertIsNone(self.failover_request()['requestId'])
        self.assertFalse((self.temp/'global.lock').is_symlink())
        states=[json.loads(p.read_bytes()) for p in (self.state/'operations').glob('*/state.json')]
        activated=[s for s in states if s.get('queueStep',{}).get('stage')=='activate']
        self.assertEqual(len(activated),1)
        self.assertEqual(activated[0]['state'],'completed')
        cache=json.loads((self.app/'run/server-auto-switch-state.json').read_bytes())
        self.assertEqual(cache['activeServerId'],'subscription-second-0000')
        self.assertEqual(cache['status'],'switched')
        self.assertEqual(cache['lastSwitchFrom'],self.server)
        self.assertEqual(cache['lastSwitchTo'],'subscription-second-0000')
        self.assertGreater(cache['lastSwitchEpoch'],0)
        self.assertEqual(cache['consecutiveFailures'],0)
        self.assertIsNone(cache['activeHealth'])
        self.assertIsNone(cache['lastProxyContext'])
        self.assertEqual(cache['failover']['status'],'switched')

    def prepared_activation_fixture(self):
        # Focused activation fixture: prepare known-valid bytes directly.
        # The separate full queue test covers the real three preceding stages.
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.env.update(TEST_ACTIVE_ID=self.server,TEST_ACTIVE_MODE='timeout')
        self.active_runtime_fixture()
        self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$(cat "$TEST_PAYLOAD")" subscription second 0')
        self.failover_init_fixture()
        self.prepared=self.temp/'prepared';self.prepared.mkdir(mode=0o700)
        self.before=self.current.read_bytes()
        self.current.chmod(0o640)
        (self.prepared/'previous-runtime.json').write_bytes(self.before)
        generated=self.shell('. "$BRORAY_ROOT/lib/server-service.sh"; broray_generate_server_config subscription-second-0000')
        shutil.copyfile(Path(generated.stdout.decode().strip()),self.prepared/'new-runtime.json')
        for p in self.prepared.iterdir():p.chmod(0o600)
        sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
        state={'status':'prepared','activeServerId':self.server,
               'selected':{'id':'subscription-second-0000','sha256':sha(self.app/'servers/subscription-second-0000.json')},
               'prepared':{'previousSha256':sha(self.current),'newSha256':sha(self.prepared/'new-runtime.json'),
                           'previousMode':'640','systemSettingsSha256':sha(self.app/'config/system/settings.json'),
                           'xraySha256':sha(self.app/'runtime/xray')}}
        (self.prepared/'result.json').write_text(json.dumps(state));(self.prepared/'result.json').chmod(0o600)
        self.env['TEST_PREPARED']=str(self.prepared)
        self.activate_script='''. "$BRORAY_ROOT/lib/server-service.sh"
broray_job_begin system servers:failover "$TEST_ACTIVE_ID" AUTO_SWITCH protected || exit $?
trap 'rc=$?; trap - EXIT; broray_job_exit "$rc" || rc=75; exit "$rc"' EXIT
broray_auto_failover_activate "$(cat "$TEST_PREPARED/result.json")" "$TEST_PREPARED"
'''

    def assert_activation_untouched(self):
        self.assertEqual(self.current.read_bytes(),self.before)
        self.assertEqual((self.app/'config/active-server').read_text().strip(),self.server)
        self.assertFalse((self.app/'restarts').exists())
        self.assertFalse((self.temp/'global.lock').is_symlink())

    def test_prepared_activation_commits_known_bytes(self):
        self.prepared_activation_fixture()
        expected=(self.prepared/'new-runtime.json').read_bytes()
        try:self.shell(self.activate_script,timeout=90)
        except AssertionError as error:self.fail((str(error),self.activation_diagnostics()))
        self.assertEqual(self.current.read_bytes(),expected)
        self.assertEqual((self.app/'config/active-server').read_text().strip(),'subscription-second-0000')
        self.assertEqual((self.app/'restarts').read_text(),'restart\n')
        self.assertFalse((self.temp/'global.lock').is_symlink())

    def test_completed_rollback_preserves_successful_switch_history(self):
        # Publication-only fixture; canonical exact rollback is independently
        # exercised below. This uses a real global owner, no runtime mutation.
        directory=self.temp/'completed-rollback';directory.mkdir(mode=0o700)
        request='q-'+'d'*32
        result={'status':'rolled-back','activeServerId':self.server,
                'selected':{'id':self.server},'context':'a'*64,
                'autoConfigSha256':'b'*64,'candidates':[],'attemptEpoch':123}
        (directory/'result.json').write_text(json.dumps(result))
        (directory/'result.json').chmod(0o600)
        quality={'status':'completed','totalCount':3,'checkedCount':3,
                 'availableCount':2,'unavailableCount':1,'errorCount':0}
        history={'lastSwitchEpoch':100,'lastSwitchAt':'2026-09-26T00:00:00Z',
                 'lastSwitchFrom':'old','lastSwitchTo':'saved','lastSwitchName':'Saved'}
        before={'schemaVersion':3,'enabled':True,'status':'waiting-threshold',
                'activeServerId':self.server,'activeHealth':{'status':'unhealthy'},
                'consecutiveFailures':3,'lastProxyContext':'a'*64,'autoConfigSha256':'b'*64,
                'candidateCount':0,'qualityRefresh':quality,
                'failover':{'requestId':request,'sourceContext':'a'*64,'status':'prepared'}}|history
        cache=self.app/'run/server-auto-switch-state.json'
        cache.write_text(json.dumps(before));cache.chmod(0o600)
        self.env.update(TEST_PREPARED=str(directory),TEST_REQUEST=request)
        self.shell('''. "$BRORAY_ROOT/lib/server-service.sh"
broray_job_begin system auto-switch servers AUTO_SWITCH protected || exit $?
trap 'rc=$?; trap - EXIT; broray_job_exit "$rc" || rc=75; exit "$rc"' EXIT
BRORAY_JOB_UNRESOLVED=true
broray_auto_failover_complete "$TEST_REQUEST" "$TEST_PREPARED" || exit $?
BRORAY_JOB_UNRESOLVED=false
''',timeout=90)
        after=json.loads(cache.read_bytes())
        self.assertEqual(after['status'],'rolled-back')
        self.assertEqual(after['lastError'],'FAILOVER_ACTIVATION_ROLLED_BACK')
        self.assertEqual(after['activeServerId'],self.server)
        self.assertEqual(after['lastAttemptEpoch'],123)
        self.assertIsNone(after['activeHealth'])
        self.assertIsNone(after['lastProxyContext'])
        self.assertEqual(after['consecutiveFailures'],0)
        self.assertEqual(after['qualityRefresh'],quality)
        for key,value in history.items():self.assertEqual(after[key],value)
        self.assertFalse((self.app/'restarts').exists())
        self.assertFalse((self.temp/'global.lock').is_symlink())

    def test_activation_rejects_changed_system_settings(self):
        self.prepared_activation_fixture()
        settings=self.app/'config/system/settings.json'
        settings.write_text(json.dumps(json.loads(settings.read_bytes())|{'socksPort':2090}))
        self.shell(self.activate_script,expected=76)
        self.assert_activation_untouched()

    def test_activation_rejects_changed_selected_credentials(self):
        self.prepared_activation_fixture()
        node=self.app/'servers/subscription-second-0000.json'
        node.write_text(json.dumps(json.loads(node.read_bytes())|{'uuid':'11111111-2222-4333-8444-666666666666'}))
        self.shell(self.activate_script,expected=76)
        self.assert_activation_untouched()

    def test_activation_rejects_corrupt_prepared_config(self):
        self.prepared_activation_fixture()
        (self.prepared/'new-runtime.json').write_bytes(b'{broken')
        self.shell(self.activate_script,expected=76)
        self.assert_activation_untouched()

    def test_activation_failed_postcheck_rolls_back_exact_bytes_and_mode(self):
        self.prepared_activation_fixture()
        listener=self.app/'bin/netstat'
        listener.write_text('#!/bin/ash\n[ "$(cat "$BRORAY_ROOT/config/active-server")" = "$TEST_ACTIVE_ID" ] || exit 0\n'
                            +listener.read_text().removeprefix('#!/bin/ash\n'))
        self.shell(self.activate_script,expected=1,timeout=90)
        self.assertEqual(self.current.read_bytes(),self.before)
        self.assertEqual(self.current.stat().st_mode & 0o777,0o640)
        self.assertEqual((self.app/'config/active-server').read_text().strip(),self.server)
        self.assertEqual((self.app/'restarts').read_text(),'restart\nrestart\n')
        self.assertEqual(json.loads((self.prepared/'result.json').read_bytes())['status'],'rolled-back')
        self.assertFalse((self.temp/'global.lock').is_symlink())

    def test_activation_restart_failure_retains_protected_fence(self):
        self.prepared_activation_fixture()
        init=self.app/'bin/fixture-init';init.write_text(init.read_text()+'exit 1\n')
        self.shell(self.activate_script,expected=75,timeout=90)
        self.assertEqual(self.current.read_bytes(),self.before)
        self.assertTrue((self.temp/'global.lock').is_symlink())
        states=[json.loads(p.read_bytes()) for p in (self.state/'operations').glob('*/state.json')]
        self.assertEqual(len(states),1)
        self.assertTrue(states[0]['running'])
        self.assertEqual(states[0]['cancelability'],'protected')


if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    unittest.main(verbosity=2,failfast=True)
