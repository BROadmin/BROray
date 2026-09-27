"""Automatic request producer: real queue, real process identity, fixture transport."""
import ctypes
import json
import unittest
from pathlib import Path
import test_auto_switch_queue as automatic


class AutoProducer(unittest.TestCase):
    setUp=automatic.AutoSwitchQueue.setUp
    shell=automatic.AutoSwitchQueue.shell
    collect=automatic.AutoSwitchQueue.collect
    reap_adopted_helpers=automatic.AutoSwitchQueue.reap_adopted_helpers
    active_runtime_fixture=automatic.AutoSwitchQueue.active_runtime_fixture
    set_config=automatic.AutoSwitchQueue.set_config
    context=automatic.AutoSwitchQueue.context
    health=automatic.AutoSwitchQueue.health
    auto_state=automatic.AutoSwitchQueue.auto_state

    def produce(self, expected=0):
        result=self.shell('. "$BRORAY_ROOT/lib/server-service.sh"; . "$BRORAY_ROOT/lib/active-proxy-health.sh"; broray_auto_enqueue_due',expected=expected)
        return json.loads(result.stdout) if result.stdout else None

    def rows(self):
        boot=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
        path=self.temp/'ram/queue'/boot/'state.json'
        return json.loads(path.read_bytes())['requests'] if path.exists() else []

    def active(self):
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.env.update(TEST_ACTIVE_ID=self.server,TEST_ACTIVE_MODE='working')
        self.active_runtime_fixture()

    def test_repeated_due_cycles_coalesce_same_quality_request(self):
        self.set_config(qualityRefreshEnabled=True)
        first=self.produce()
        second=self.produce()
        self.assertEqual(first['quality']['requestId'],second['quality']['requestId'])
        self.assertEqual(len(self.rows()),1)
        self.assertEqual(self.rows()[0]['priority'],3)
        self.assertEqual(self.rows()[0]['state'],'queued')
        self.assertFalse((self.temp/'global.lock').exists())
        self.assertFalse((self.temp/'xray-pid').exists())

    def test_paused_producer_admits_nothing(self):
        self.set_config(enabled=True,qualityRefreshEnabled=True)
        self.active()
        before=self.context()
        self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call pause')
        self.produce()
        self.assertEqual(self.rows(),[])
        self.assertEqual(self.context(),before)

    def test_same_service_cycle_tracks_pending_request_without_new_receipt(self):
        self.set_config(qualityRefreshEnabled=True)
        self.shell('''. "$BRORAY_ROOT/lib/server-service.sh"
. "$BRORAY_ROOT/lib/active-proxy-health.sh"
broray_auto_enqueue_due >"$BRORAY_ROOT/tmp/first-due"
broray_auto_enqueue_due >"$BRORAY_ROOT/tmp/second-due"
''')
        first=json.loads((self.app/'tmp/first-due').read_bytes())
        second=json.loads((self.app/'tmp/second-due').read_bytes())
        self.assertEqual(first['quality']['requestId'],second['quality']['requestId'])
        boot=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
        value=json.loads((self.temp/'ram/queue'/boot/'state.json').read_bytes())
        self.assertEqual(len(value['requests']),1)
        self.assertEqual(len(value['receipts']),1,
                         'An existing pending request must not acquire another receipt on every service tick')

    def test_terminal_request_can_be_admitted_again_by_same_service(self):
        self.set_config(qualityRefreshEnabled=True)
        self.shell('''. "$BRORAY_ROOT/lib/server-service.sh"
. "$BRORAY_ROOT/lib/active-proxy-health.sh"
broray_auto_enqueue_due >"$BRORAY_ROOT/tmp/first-due"
request="$(jq -r .quality.requestId "$BRORAY_ROOT/tmp/first-due")"
broray_ops_queue_cancel "$request" >/dev/null
broray_auto_enqueue_due >"$BRORAY_ROOT/tmp/second-due"
''')
        first=json.loads((self.app/'tmp/first-due').read_bytes())
        second=json.loads((self.app/'tmp/second-due').read_bytes())
        self.assertNotEqual(first['quality']['requestId'],second['quality']['requestId'])
        self.assertEqual(second['quality']['state'],'queued')
        self.assertEqual(len(self.rows()),1)

    def test_manual_vpn_off_does_not_create_recovery_request(self):
        self.set_config(enabled=True,qualityRefreshEnabled=False)
        self.produce()
        self.assertEqual(self.rows(),[])
        self.assertFalse((self.temp/'xray-pid').exists())

    def test_fresh_health_and_future_quality_are_not_resubmitted(self):
        self.set_config(enabled=True,qualityRefreshEnabled=True)
        self.active()
        self.health()
        cache=self.app/'run/server-auto-switch-state.json'
        value=json.loads(cache.read_bytes())
        value['qualityRefresh']={'status':'success','intervalMinutes':60,
                                'nextCheckEpoch':value['activeHealth']['checkedEpoch']+3600}
        cache.write_text(json.dumps(value))
        # Fix only the producer's wall-clock decision to one second after the
        # actual sample. Native ownership still uses real proc/monotonic facts.
        date=self.app/'bin/date'
        executable=str(date.readlink())
        date.unlink()
        date.write_text('#!/bin/ash\nif [ "$*" = "+%s" ]; then echo '+
                        str(value['activeHealth']['checkedEpoch']+1)+
                        '; else exec "'+executable+'" "$@"; fi\n')
        date.chmod(0o755)
        self.assertEqual(self.shell('date "+%s"').stdout.decode().strip(),
                         str(value['activeHealth']['checkedEpoch']+1))
        self.produce()
        self.assertEqual(self.rows(),[])

    def test_due_active_observer_has_priority_over_quality(self):
        self.set_config(enabled=True,qualityRefreshEnabled=True)
        self.active()
        before=self.context()
        result=self.produce()
        self.assertEqual(result['health']['priority'],0)
        self.assertEqual(result['quality']['priority'],3)
        selected=json.loads(self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_queue_next').stdout)
        self.assertEqual(selected['requestId'],result['health']['requestId'])
        self.assertEqual(self.context(),before)

    def test_service_dispatches_health_before_quality_catalog_work(self):
        self.set_config(enabled=True,qualityRefreshEnabled=True)
        self.active()
        response=self.shell('''. "$BRORAY_ROOT/lib/server-service.sh"
. "$BRORAY_ROOT/lib/active-proxy-health.sh"
broray_job_dispatch_step() {
    [ "$1" = continue ] || return 94
    broray_ops_queue_next >"$BRORAY_ROOT/tmp/urgent-dispatch"
}
broray_quality_context() {
    [ -f "$BRORAY_ROOT/tmp/urgent-dispatch" ] || return 93
    printf '%064d\\n' 1
}
broray_auto_enqueue_due continue
''')
        value=json.loads(response.stdout)
        dispatched=json.loads((self.app/'tmp/urgent-dispatch').read_bytes())
        self.assertEqual(dispatched['requestId'],value['health']['requestId'])
        self.assertEqual(value['quality']['priority'],3)

    def pending_failover(self):
        self.set_config(enabled=True,failureThreshold=1)
        self.active();self.env['TEST_ACTIVE_MODE']='timeout'
        self.health()
        request=next(r for r in self.rows() if r['action']=='servers:failover')
        cache=self.app/'run/server-auto-switch-state.json'
        value=json.loads(cache.read_bytes())
        # Producer-only fixture: the live queue request is real. Its progress
        # projection represents a candidate stage; full publication and
        # activation use the independent real-service failover test.
        value['failover']={'status':'probing','requestId':request['requestId'],
            'sourceContext':value['activeHealth']['context'],
            'serverId':self.server,'autoConfigSha256':value['autoConfigSha256']}
        cache.write_text(json.dumps(value))
        date=self.app/'bin/date';executable=str(date.readlink());date.unlink()
        date.write_text('#!/bin/ash\nif [ "$*" = "+%s" ]; then cat "$BRORAY_ROOT/tmp/producer-now"; '
                        'else exec "'+executable+'" "$@"; fi\n')
        date.chmod(0o755)
        (self.app/'tmp/producer-now').write_text(str(value['activeHealth']['checkedEpoch']+20))
        return cache,value,request

    def test_pending_failover_coalesces_periodic_health_but_manual_remains_first(self):
        cache,value,request=self.pending_failover()
        before=cache.read_bytes()
        self.assertIsNone(self.produce()['health'])
        self.assertEqual([r['requestId'] for r in self.rows()],[request['requestId']])
        self.assertEqual(cache.read_bytes(),before)
        context=value['activeHealth']['context']
        manual=json.loads(self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; '
            'broray_ops_queue_submit servers:active-health "'+self.server+'" USER "'+context+'" "'+'c'*32+'"').stdout)
        selected=json.loads(self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_queue_next').stdout)
        self.assertEqual(selected['requestId'],manual['requestId'])
        self.assertEqual(selected['priority'],0)

    def test_periodic_health_resumes_without_current_failed_health_or_pending_failover(self):
        cache,value,request=self.pending_failover()
        variations=[
            ('expired',{'activeHealth':dict(value['activeHealth'],checkedEpoch=value['activeHealth']['checkedEpoch']-101)}),
            ('healthy',{'activeHealth':dict(value['activeHealth'],status='healthy')}),
            ('other-context',{'failover':dict(value['failover'],sourceContext='a'*64)}),
            ('other-settings',{'failover':dict(value['failover'],autoConfigSha256='a'*64)}),
        ]
        for name,changes in variations:
            with self.subTest(reason=name):
                cache.write_text(json.dumps(value|changes))
                result=self.produce()['health']
                self.assertEqual(result['priority'],0)
                self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_queue_cancel "'+result['requestId']+'"')
        cache.write_text(json.dumps(value))
        self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_queue_cancel "'+request['requestId']+'"')
        self.assertEqual(self.produce()['health']['priority'],0)

    def test_pending_failover_uses_latest_published_sample_without_hiding_recovery(self):
        cache,value,request=self.pending_failover()
        now=value['activeHealth']['checkedEpoch']+140
        (self.app/'tmp/producer-now').write_text(str(now))
        value['failover']['activeHealth']=dict(value['activeHealth'],checkedEpoch=now-20)
        cache.write_text(json.dumps(value))
        self.assertIsNone(self.produce()['health'])
        # A newer healthy observer result always overrides the failed P1
        # sample. The normal 15-second cadence is due, so it must be admitted.
        value['activeHealth']=dict(value['activeHealth'],status='healthy',checkedEpoch=now-16)
        cache.write_text(json.dumps(value))
        self.assertEqual(self.produce()['health']['priority'],0)


if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    unittest.main(verbosity=2,failfast=True)
