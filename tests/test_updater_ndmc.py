"""Real updater route path: exact staged dispatcher while the old app lacks it."""
import hashlib, os, subprocess, unittest
import test_field_regressions as field

class UpdaterNdmc(unittest.TestCase):
    def setUp(self):
        self.f=field.FieldRegressions(); self.f.setUp(); self.addCleanup(self.f.doCleanups)
        self.f.route_fixture(count=1101)
        self.target='3.2.0-test--update-fixture'
        self.slot=self.f.app/'releases'/self.target
        (self.slot/'app/bin').mkdir(parents=True)
        self.helper=self.slot/'app/bin/broray-system-ndmc'
        source=(field.APP/'bin/broray-system-ndmc').read_text()
        self.helper.write_text(source.replace('#!/opt/bin/ash','#!/bin/ash').replace('/bin/ndmc',str(self.f.bin/'ndmc')))
        self.helper.chmod(0o755)
        self.manifest=self.slot/'SHA256SUMS'
        self.manifest.write_text(hashlib.sha256(self.helper.read_bytes()).hexdigest()+'  app/bin/broray-system-ndmc\n')
        (self.slot/'.broray-slot').write_text(self.target+'\n')
        (self.f.operation/'target').write_text(self.target+'\n')
        (self.f.operation/'target-slot').write_text(self.target+'\n')
        self.f.env.update(LD_LIBRARY_PATH='/opt/lib:/opt/usr/lib',LD_PRELOAD='missing-fixture.so')
        p=self.f.bin/'ndmc'
        p.write_text(p.read_text().replace('[ "$1" = -c ]','[ "${LD_LIBRARY_PATH+x}" != x ] && [ "${LD_PRELOAD+x}" != x ] || exit 98\n[ "$1" = -c ]'))
        self.before=self.f.running.read_bytes()
    def call(self):
        return self.f.routes_call('routes_capture && routes_restore_captured && routes_verify_captured')
    def test_old_current_without_helper_uses_verified_target_for_1101_routes(self):
        self.assertFalse((self.f.app/'bin/broray-system-ndmc').exists())
        p=self.call(); self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertEqual(self.f.running.read_bytes(),self.before)
        self.assertFalse(self.f.commands.exists())
        self.assertEqual(len((self.f.operation/'managed-routes.before').read_text().splitlines()),1101)
    def rejected(self):
        p=self.call(); self.assertNotEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertFalse(self.f.commands.exists());self.assertEqual(self.f.running.read_bytes(),self.before)
        self.assertFalse((self.f.operation/'managed-routes.before').exists())
    def test_changed_helper_refused(self):
        self.helper.write_text(self.helper.read_text()+'\n# changed\n'); self.rejected()
    def test_missing_helper_refused(self):
        self.helper.unlink(); self.rejected()
    def test_symlink_helper_refused(self):
        other=self.f.base/'other'; self.helper.rename(other); self.helper.symlink_to(other);self.rejected()
    def test_duplicate_manifest_entry_refused(self):
        self.manifest.write_text(self.manifest.read_text()*2);self.rejected()
    def test_wrong_slot_marker_refused(self):
        (self.slot/'.broray-slot').write_text('foreign\n');self.rejected()
    def test_missing_target_receipt_refused(self):
        (self.f.operation/'target').unlink();self.rejected()
    def test_switched_target_is_resolved_without_old_app_fallback(self):
        self.slot.rename(self.f.app/'current')
        p=self.call();self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertEqual(self.f.running.read_bytes(),self.before)

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
