"""Subscription preparation under finite queue owners; real parser and fixture HTTP."""
import ctypes
import copy
import hashlib
import json
import subprocess
import time
import unittest
import uuid
import test_subscription_jobs as original
import test_server_jobs as servers
import test_auto_switch_queue as active


class SubscriptionQueue(unittest.TestCase):
    setUp=original.SubscriptionJobs.setUp
    record=original.SubscriptionJobs.record
    reap_adopted_helpers=original.SubscriptionJobs.reap_adopted_helpers
    collect=servers.ServerJobs.collect
    shell=servers.ServerJobs.shell

    def submit(self, **fields):
        path=self.record(**fields)
        self.before=path.read_bytes()
        self.nonce=uuid.uuid4().hex
        context=hashlib.sha256(self.before).hexdigest()
        reply=self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_queue_submit subscriptions:refresh test SUBSCRIPTION_AUTO '+context+' '+self.nonce)
        self.request=json.loads(reply.stdout)
        self.directory=self.temp/'ram/requests'/self.request['requestId']
        return path

    def run_stage(self, expected=0):
        process=subprocess.Popen(['/bin/ash',str(self.app/'lib/operation-worker.sh'),
            '--request',self.request['requestId']],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        out,err=self.collect(process,90)
        self.assertEqual(process.returncode,expected,(out,err))

    def lookup(self):
        return json.loads(self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_queue_lookup '+self.nonce).stdout)

    def test_fetch_yields_response_without_live_metadata_mutation(self):
        path=self.submit()
        self.run_stage()
        self.assertEqual(path.read_bytes(),self.before)
        self.assertEqual(self.lookup()['state'],'queued')
        self.assertEqual((self.directory/'preparation/download').read_bytes(),self.payload.read_bytes())
        result=json.loads((self.directory/'result.json').read_bytes())
        self.assertEqual(result['stage'],'fetch')
        self.assertFalse((self.temp/'global.lock').exists())
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_parse_reuses_download_and_keeps_catalog_private(self):
        path=self.submit()
        self.run_stage()
        downloaded=(self.directory/'preparation/download').read_bytes()
        self.env['TEST_MODE']='error'
        self.run_stage()
        self.assertEqual(path.read_bytes(),self.before)
        self.assertEqual((self.directory/'preparation/download').read_bytes(),downloaded)
        self.assertEqual(len(list((self.directory/'preparation/stage').glob('*.json'))),1)
        self.assertEqual(list((self.app/'servers').glob('*.json')),[])
        self.assertEqual(json.loads((self.directory/'result.json').read_bytes())['stage'],'parse')
        self.assertEqual(self.lookup()['state'],'queued')
        self.assertFalse((self.temp/'global.lock').exists())

    def test_changed_settings_reject_preparation_without_live_mutation(self):
        path=self.submit()
        self.run_stage()
        value=json.loads(path.read_bytes())
        value['url']='https://93.184.216.34/changed'
        path.write_text(json.dumps(value))
        changed=path.read_bytes()
        self.run_stage(expected=76)
        self.assertEqual(path.read_bytes(),changed)
        self.assertEqual(list((self.app/'servers').glob('*.json')),[])
        self.assertEqual(self.lookup()['state'],'failed')

    def test_corrupt_download_rejected_before_parse(self):
        path=self.submit()
        self.run_stage()
        (self.directory/'preparation/download').write_bytes(b'corrupt')
        self.run_stage(expected=76)
        self.assertEqual(path.read_bytes(),self.before)
        self.assertEqual(self.lookup()['state'],'failed')
        self.assertEqual(list((self.directory/'preparation/stage').glob('*.json')),[])

    def test_apply_uses_prepared_nodes_without_second_download(self):
        path=self.submit()
        self.run_stage()
        self.run_stage()
        self.env['TEST_MODE']='error'
        self.run_stage()
        value=json.loads(path.read_bytes())
        self.assertEqual(value['lastUpdateStatus'],'success')
        self.assertEqual(value['lastUpdateResult']['accepted'],1)
        self.assertEqual(value['lastUpdateResult']['trigger'],'automatic')
        self.assertEqual(len(list((self.app/'servers').glob('*.json'))),1)
        self.assertEqual(self.lookup()['state'],'completed')
        self.assertFalse((self.temp/'global.lock').exists())
        original.SubscriptionJobs.assert_generated_configs(self)

    def test_disabled_subscription_before_apply_leaves_catalog_unchanged(self):
        path=self.submit()
        self.run_stage()
        self.run_stage()
        value=json.loads(path.read_bytes())
        value['enabled']=False
        path.write_text(json.dumps(value))
        before=path.read_bytes()
        self.run_stage(expected=76)
        self.assertEqual(path.read_bytes(),before)
        self.assertEqual(list((self.app/'servers').glob('*.json')),[])
        self.assertEqual(self.lookup()['state'],'failed')
        self.assertFalse((self.temp/'global.lock').exists())

    def test_private_hwid_is_persisted_only_during_apply(self):
        path=self.submit(clientHwid=None)
        self.run_stage()
        hwid=json.loads((self.directory/'preparation/input.json').read_bytes())['clientHwid']
        self.assertTrue(hwid.startswith('broray-'))
        self.assertEqual(path.read_bytes(),self.before)
        self.run_stage()
        self.assertEqual(path.read_bytes(),self.before)
        self.run_stage()
        self.assertEqual(json.loads(path.read_bytes())['clientHwid'],hwid)
        self.assertEqual(self.lookup()['state'],'completed')

    def test_fetch_failure_is_saved_only_by_final_apply(self):
        path=self.submit()
        self.env['TEST_MODE']='error'
        self.run_stage()
        self.assertEqual(path.read_bytes(),self.before)
        self.run_stage()
        self.assertEqual(path.read_bytes(),self.before)
        self.run_stage(expected=1)
        value=json.loads(path.read_bytes())
        self.assertEqual(value['lastUpdateStatus'],'error')
        self.assertIsNotNone(value['lastError'])
        self.assertGreater(value['nextUpdateEpoch'],1)
        self.assertEqual(self.lookup()['state'],'failed')
        self.assertEqual(list((self.app/'servers').glob('*.json')),[])
        self.assertFalse((self.temp/'global.lock').exists())

    def test_partial_apply_remaps_fresh_catalog_and_preserves_active_and_foreign(self):
        path=self.submit()
        self.payload.write_text(self.payload.read_text()+'vless://broken\n')
        self.run_stage()
        self.run_stage()
        staged=json.loads(next((self.directory/'preparation/stage').glob('*.json')).read_bytes())
        preserved=copy.deepcopy(staged)
        preserved['id']='subscription-test-stable'
        active=copy.deepcopy(staged)
        active.update(id='subscription-test-retained',address='203.0.113.99')
        foreign=copy.deepcopy(staged)
        foreign.update(id='manual-added-after-parse',address='198.51.100.77',source={'type':'manual'})
        catalog=self.app/'servers'
        catalog.mkdir(exist_ok=True)
        for value in [preserved,active,foreign]:
            file=catalog/(value['id']+'.json')
            file.write_text(json.dumps(value))
            file.chmod(0o600)
        foreign_file=catalog/(foreign['id']+'.json')
        foreign_bytes=foreign_file.read_bytes()
        active_file=self.app/'config/active-server'
        active_file.write_text(active['id']+'\n')
        self.run_stage()
        value=json.loads(path.read_bytes())
        self.assertEqual(value['lastUpdateStatus'],'partial')
        self.assertEqual(value['lastUpdateResult']['accepted'],1)
        self.assertEqual(value['lastUpdateResult']['retained'],1)
        self.assertEqual(value['lastUpdateResult']['deletionPolicy'],'retain-unmatched')
        self.assertEqual(active_file.read_text().strip(),active['id'])
        self.assertEqual(foreign_file.read_bytes(),foreign_bytes)
        self.assertEqual({p.stem for p in catalog.glob('*.json')},
                         {preserved['id'],active['id'],foreign['id']})
        self.assertEqual(self.lookup()['state'],'completed')
        # A repeated/lost-response invocation must never apply a second time.
        final_metadata=path.read_bytes()
        final_catalog={p.name:p.read_bytes() for p in catalog.glob('*.json')}
        self.run_stage(expected=2)
        self.assertEqual(path.read_bytes(),final_metadata)
        self.assertEqual({p.name:p.read_bytes() for p in catalog.glob('*.json')},final_catalog)

    def test_corrupt_prepared_node_rejected_before_any_apply(self):
        path=self.submit()
        self.run_stage()
        self.run_stage()
        next((self.directory/'preparation/stage').glob('*.json')).write_bytes(b'{broken')
        self.run_stage(expected=76)
        self.assertEqual(path.read_bytes(),self.before)
        self.assertEqual(list((self.app/'servers').glob('*.json')),[])
        self.assertEqual(self.lookup()['state'],'failed')
        self.assertFalse((self.temp/'global.lock').exists())


class SubscriptionOverlap(unittest.TestCase):
    setUp=active.AutoSwitchQueue.setUp
    shell=servers.ServerJobs.shell
    collect=servers.ServerJobs.collect
    reap_adopted_helpers=servers.ServerJobs.reap_adopted_helpers
    active_runtime_fixture=active.AutoSwitchQueue.active_runtime_fixture
    set_config=active.AutoSwitchQueue.set_config
    auto_state=active.AutoSwitchQueue.auto_state
    context=active.AutoSwitchQueue.context
    health=active.AutoSwitchQueue.health
    record=original.SubscriptionJobs.record
    submit=SubscriptionQueue.submit
    lookup=SubscriptionQueue.lookup

    def test_slow_fetch_allows_real_active_health_and_cancel_drains(self):
        self.set_config(enabled=True)
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.env.update(TEST_ACTIVE_ID=self.server,TEST_ACTIVE_MODE='working')
        self.active_runtime_fixture()
        path=self.submit()
        curl=self.app/'bin/curl'
        original_curl=curl.read_text()
        curl.write_text('''#!/bin/ash
case "$*" in
 *--dump-header*)
  echo ready >"$TEST_READY"
  while [ ! -f "$BRORAY_ROOT/tmp/release-fetch" ]; do sleep .05; done
  exit 28 ;;
esac
'''+original_curl.removeprefix('#!/bin/ash\n'))
        before_context=self.context()
        process=subprocess.Popen(['/bin/ash',str(self.app/'lib/operation-worker.sh'),
            '--request',self.request['requestId']],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            until=time.monotonic()+40
            while not (self.temp/'transport-ready').exists():
                self.assertIsNone(process.poll())
                self.assertLess(time.monotonic(),until,'subscription transport not started')
                time.sleep(.05)
            result=self.health()
            health_completed=time.monotonic()
            self.assertEqual(result['activeHealth']['status'],'healthy')
            self.assertIsNone(process.poll(),'Fetch completed before health could overlap it')
            self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())
            self.assertEqual(path.read_bytes(),self.before)
            self.assertEqual(self.context(),before_context)
            pointer=(self.temp/'ram/resources/background-prepare').resolve()
            operation=pointer.parent.name
            self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call cancel '+operation)
            out,err=self.collect(process,40)
            self.assertEqual(process.returncode,130,(out,err))
            self.assertLess(health_completed,time.monotonic())
            self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
            self.assertEqual(path.read_bytes(),self.before)
            self.assertEqual(self.context(),before_context)
            self.assertEqual(self.lookup()['state'],'cancelled')
            self.assertEqual(json.loads((pointer.parent/'supervisors.json').read_bytes())['supervisors'],[])
        finally:
            (self.app/'tmp/release-fetch').touch()
            if process.poll() is None:
                self.collect(process,60)


if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    unittest.main(verbosity=2,failfast=True)
