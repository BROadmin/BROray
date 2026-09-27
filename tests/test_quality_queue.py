"""One actual isolated probe per queue stage; fixture network, real Linux owners."""
import ctypes
import json
import subprocess
import unittest
import uuid
from pathlib import Path
import test_auto_switch_jobs as automatic
import test_server_jobs as servers


class QualityQueue(unittest.TestCase):
    def setUp(self):
        automatic.AutoSwitchJobs.setUp(self)
        self.set_config(qualityRefreshEnabled=True)

    set_config = automatic.AutoSwitchJobs.set_config
    shell = servers.ServerJobs.shell
    collect = servers.ServerJobs.collect
    reap_adopted_helpers = servers.ServerJobs.reap_adopted_helpers
    old_quality = servers.ServerJobs.old_quality

    def submit(self, target='all', source='SERVER_CHECK_AUTO'):
        context = self.shell('. "$BRORAY_ROOT/lib/server-service.sh"; broray_quality_context '+target+' '+source).stdout.decode().strip()
        nonce = uuid.uuid4().hex
        response = self.shell('. "$BRORAY_ROOT/lib/operation-job.sh"; broray_ops_queue_submit servers:quality '+target+' '+source+' '+context+' '+nonce)
        return json.loads(response.stdout), nonce

    def run_stage(self, request, expected=0):
        process = subprocess.Popen(['/bin/ash', str(self.app/'lib/operation-worker.sh'),
                                   '--request', request['requestId']],
                                   env=self.env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        out, err = self.collect(process, 90)
        self.assertEqual(process.returncode, expected, (out, err))

    def lookup(self, nonce):
        return json.loads(self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_queue_lookup '+nonce).stdout)

    def test_batch_yields_after_each_server_and_preserves_cursor(self):
        self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$(cat "$TEST_PAYLOAD")" subscription second 0')
        request, nonce = self.submit()
        self.run_stage(request)
        self.assertEqual(self.lookup(nonce)['state'], 'queued')
        result = json.loads((self.temp/'ram/requests'/request['requestId']/'result.json').read_bytes())
        self.assertEqual(result['cursor'], 1)
        self.assertEqual(result['totalCount'], 2)
        self.assertEqual(len(list((self.app/'run/server-quality').glob('*.json'))), 1)
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
        self.run_stage(request)
        self.assertEqual(self.lookup(nonce)['state'], 'completed')
        qualities = [json.loads(f.read_bytes()) for f in (self.app/'run/server-quality').glob('*.json')]
        self.assertEqual(len(qualities), 2)
        self.assertEqual([q['successfulChecks'] for q in qualities], [1, 1])
        progress = json.loads((self.app/'run/server-auto-switch-state.json').read_bytes())['qualityRefresh']
        self.assertEqual(progress['status'], 'success')
        self.assertEqual(progress['checkedCount'], 2)
        self.assertEqual(progress['errorCount'], 0)
        self.assertFalse((self.temp/'global.lock').exists())
        self.assertFalse(Path('/proc', Path(self.env['TEST_XRAY_PID']).read_text().strip()).exists())

    def test_changed_credentials_discard_old_measurement(self):
        before = self.old_quality()
        request, nonce = self.submit(self.server)
        curl = self.app/'bin/curl'
        original = curl.read_text()
        self.env['TEST_SERVER_FILE'] = str(self.app/'servers'/f'{self.server}.json')
        curl.write_text(original.replace("printf '204 0.02'", """jq '.uuid="11111111-2222-4333-8444-666666666666"' "$TEST_SERVER_FILE" >"$TEST_SERVER_FILE.new"
mv "$TEST_SERVER_FILE.new" "$TEST_SERVER_FILE"
printf '204 0.02'"""))
        self.run_stage(request, expected=76)
        self.assertEqual(json.loads(Path(self.env['TEST_SERVER_FILE']).read_bytes())['uuid'],
                         '11111111-2222-4333-8444-666666666666')
        self.assertEqual(self.quality.read_bytes(), before)
        self.assertEqual(self.lookup(nonce)['state'], 'failed')
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
        self.assertFalse((self.temp/'global.lock').exists())

    def test_negative_probe_is_completed_quality_not_worker_failure(self):
        self.env['TEST_MODE'] = 'error'
        request, nonce = self.submit(self.server)
        self.run_stage(request)
        self.assertEqual(self.lookup(nonce)['state'], 'completed')
        quality = json.loads(self.quality.read_bytes())
        self.assertEqual(quality['status'], 'unavailable')
        progress = json.loads((self.app/'run/server-auto-switch-state.json').read_bytes())['qualityRefresh']
        self.assertEqual(progress['unavailableCount'], 1)
        self.assertEqual(progress['errorCount'], 0)

    def test_disabled_schedule_refuses_queued_probe(self):
        before = self.old_quality()
        request, nonce = self.submit()
        self.set_config(qualityRefreshEnabled=False)
        self.run_stage(request, expected=76)
        self.assertEqual(self.quality.read_bytes(), before)
        self.assertFalse(Path(self.env['TEST_XRAY_PID']).exists())
        self.assertEqual(self.lookup(nonce)['state'], 'failed')
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_schedule_change_between_nodes_preserves_cursor_and_measurements(self):
        self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$(cat "$TEST_PAYLOAD")" subscription second 0')
        request, nonce = self.submit()
        self.run_stage(request)
        result = self.temp/'ram/requests'/request['requestId']/'result.json'
        before = result.read_bytes()
        quality = {f.name:f.read_bytes() for f in (self.app/'run/server-quality').glob('*.json')}
        progress = (self.app/'run/server-auto-switch-state.json').read_bytes()
        self.set_config(qualityRefreshIntervalMinutes=180)
        self.run_stage(request, expected=76)
        self.assertEqual(result.read_bytes(), before)
        self.assertEqual({f.name:f.read_bytes() for f in (self.app/'run/server-quality').glob('*.json')}, quality)
        self.assertEqual((self.app/'run/server-auto-switch-state.json').read_bytes(), progress)
        self.assertEqual(self.lookup(nonce)['state'], 'failed')

    def test_manual_probe_works_with_automatic_schedule_disabled(self):
        self.set_config(qualityRefreshEnabled=False)
        request, nonce = self.submit(self.server, source='USER')
        self.run_stage(request)
        self.assertEqual(self.lookup(nonce)['state'], 'completed')
        self.assertEqual(json.loads(self.quality.read_bytes())['measurementSource'], 'manual')

    def test_configured_interval_is_used_for_next_schedule(self):
        self.set_config(qualityRefreshIntervalMinutes=180)
        request, nonce = self.submit()
        self.run_stage(request)
        progress = json.loads((self.app/'run/server-auto-switch-state.json').read_bytes())['qualityRefresh']
        self.assertEqual(progress['intervalMinutes'], 180)
        import datetime
        completed = int(datetime.datetime.fromisoformat(progress['lastCompletedAt'].replace('Z','+00:00')).timestamp())
        self.assertLessEqual(abs(progress['nextCheckEpoch']-completed-180*60), 2)

    def test_explicit_missing_target_has_no_successful_empty_batch(self):
        result = self.shell('. "$BRORAY_ROOT/lib/server-service.sh"; broray_quality_context missing-node SERVER_CHECK_AUTO', expected=76)
        self.assertEqual(result.stdout, b'')


if __name__ == '__main__':
    assert ctypes.CDLL(None).prctl(36, 1, 0, 0, 0) == 0
    unittest.main(verbosity=2, failfast=True)
