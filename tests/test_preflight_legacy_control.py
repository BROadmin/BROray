"""Legacy control metadata must be bound before a reboot can retire it safely."""
import hashlib,json,stat,unittest
from pathlib import Path
from test_preflight_bootguard import PreflightBootguard

class LegacyControl(PreflightBootguard):
 def snapshot(self):return json.loads((self.operation()/'platform-legacy-control/snapshot.json').read_bytes())
 def test_live_legacy_control_inventory_retained_before_reboot(self):
  service,_=self.start_service();expected={n:(self.updater/n).read_bytes() for n in ['daemon.pid','daemon.ready']}
  modes={n:stat.S_IMODE((self.updater/n).stat().st_mode) for n in ['daemon.pid','daemon.ready','daemon.lock']}
  r=self.guarded();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertTrue((self.operation()/'platform-legacy-control/snapshot.json').is_file(),'legacy control state has no durable before-inventory')
  record=self.snapshot();bound=self.bound();op=self.operation()
  self.assertEqual(record['contract'],'broray-legacy-updater-control/1');self.assertEqual(record['operationId'],op.name)
  self.assertEqual(record['stopNonce'],bound['stopNonce']);self.assertEqual(record['oldOwner'],bound['service']['owner']);self.assertTrue(record['oldServiceWasRunning'])
  self.assertEqual(record['oldBootId'],Path('/proc/sys/kernel/random/boot_id').read_text().strip())
  self.assertEqual(record['serviceReceiptSha256'],hashlib.sha256((op/'platform-service.json').read_bytes()).hexdigest())
  self.assertFalse(record['signalsAuthorized']);self.assertFalse(record['serviceStopped']);self.assertFalse(record['activationAllowed'])
  rows={x['name']:x for x in record['entries']};self.assertEqual(set(rows),{'daemon.pid','daemon.ready','daemon.lock'})
  for name,body in expected.items():
   self.assertTrue(rows[name]['present']);self.assertEqual(rows[name]['kind'],'file');self.assertEqual(rows[name]['mode'],modes[name]);self.assertEqual(rows[name]['sha256'],hashlib.sha256(body).hexdigest());self.assertEqual((self.updater/name).read_bytes(),body)
  self.assertEqual(rows['daemon.lock']['kind'],'directory');self.assertTrue(rows['daemon.lock']['present']);self.assertTrue(rows['daemon.lock']['empty']);self.assertEqual(rows['daemon.lock']['mode'],modes['daemon.lock'])
  self.assertTrue(self.lock.is_symlink());self.assertIsNone(service.poll());self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOP_INTENT')
 def test_absent_legacy_metadata_is_evidence_not_stopped(self):
  r=self.guarded();self.assertEqual(r.returncode,0,r.stdout+r.stderr);record=self.snapshot()
  self.assertIsNone(record['oldOwner']);self.assertFalse(record['oldServiceWasRunning']);self.assertTrue(all(not x['present'] for x in record['entries']))
  self.assertFalse(record['serviceStopped']);self.assertTrue(self.lock.is_symlink())
 def test_replay_preserves_corrupt_control_evidence(self):
  self.start_service();tail='record="$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/platform-legacy-control/snapshot.json"; [ -f "$record" ] || exit 99; printf "{broken" >"$record"; broray_ops_preflight_stage_bootguard'
  r=self.guarded(tail);self.assertNotEqual(r.returncode,0);self.assertNotEqual(r.returncode,99,'initial control inventory missing')
  self.assertEqual((self.operation()/'platform-legacy-control/snapshot.json').read_bytes(),b'{broken');self.assertTrue(self.lock.is_symlink())

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(LegacyControl(n) for n in LegacyControl.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
