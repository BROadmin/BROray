"""First-update platform migration must establish a real launch origin."""
import json
import os
from pathlib import Path
import subprocess
import unittest
from test_legacy_handoff_schedule import LegacySchedule, HANDOFF
from test_updater_preflight import UpdaterPreflight


class LegacyTransition(LegacySchedule):
    def test_01_exact_new_init_refuses_without_protected_origin(self):
        # Real installed target S22, no service hook or fabricated origin.
        init = self.root / 'opt/etc/init.d/S22broray-updater'
        p = subprocess.run(['/bin/ash', str(init), 'start'],
                           env={**os.environ, 'BRORAY_UPDATER_ROOT_PREFIX': str(self.root)},
                           capture_output=True, text=True, timeout=15)
        self.assertEqual(p.returncode, 75, p.stdout + p.stderr)
        self.assertEqual(json.loads(p.stdout)['errorCode'],
                         'UPDATER_SERVICE_IDENTITY_UNCONFIRMED')

    def test_02_matching_files_are_not_platform_readiness(self):
        before = self.inventory()
        p = subprocess.run(['/bin/ash', str(HANDOFF), 'status'],
                           env=self.env, capture_output=True, text=True, timeout=15)
        self.assertNotEqual(json.loads(p.stdout)['state'], 'success', p.stdout)
        self.assertEqual(self.inventory(), before)

    def transition_fixture(self, reply=None, rc=75):
        self.operation = 'update-fixture-310'
        manifest = __import__('hashlib').sha256((self.payload / 'SHA256SUMS').read_bytes()).hexdigest()
        request = dict(schemaVersion=1, contract='broray-universal-platform-handoff/1',
                       operationId=self.operation, previousSlot='old--bootstrap',
                       targetSlot='new--update-test', candidateId='new',
                       payloadManifestSha256=manifest, createdAt='2026-10-02T12:00:00Z')
        (self.state / 'request.json').write_text(json.dumps(request))
        (self.state / 'phase').write_text('queued\n')
        op = self.root / 'opt/var/lib/broray/operations' / self.operation
        op.mkdir(parents=True)
        (op / 'state.json').write_text('{"state":"success","stage":"complete","running":false}')
        boot = self.root / 'proc/sys/kernel/random/boot_id'
        boot.parent.mkdir(parents=True)
        boot.write_text('11111111-2222-3333-4444-555555555555\n')
        # Fake filesystem root has no process table. Read real VM process
        # start ticks through the existing hook; never invent owner identity.
        proc = self.root / 'process-starttime.sh'
        proc.write_text('#!/bin/ash\nawk \'NR==1 {print $22; exit}\' "/proc/$1/stat"\n')
        proc.chmod(0o755)
        self.env['BRORAY_HANDOFF_PROCESS_STARTTIME_HOOK'] = str(proc)
        # Only the existing preflight boundary and final reboot command are
        # doubles. Application handoff state/control flow is production code.
        self.preflight_calls = self.root / 'preflight.calls'
        script = self.root / 'preflight.sh'
        reply = reply or dict(ok=False, errorCode='UPDATER_LEGACY_REBOOT_REQUIRED',
                             phase='REBOOT_REQUIRED', operationId='op-fixture-preflight',
                             expectedPlatformManifestSha256=manifest, platformReady=False,
                             serviceStopped=False, activationAllowed=False, signalsAuthorized=False)
        script.write_text('#!/bin/ash\n[ "$1" = preflight ] || exit 99\n'
                          + 'echo preflight >>"' + str(self.preflight_calls) + '"\n'
                          + "printf '%s\\n' '" + json.dumps(reply) + "'\nexit " + str(rc) + '\n')
        script.chmod(0o755)
        self.reboot_calls = self.root / 'reboot.calls'
        reboot = self.app / 'current/app/bin/broray-system-ndmc'
        reboot.parent.mkdir(parents=True)
        reboot.write_text('#!/bin/ash\n[ "$1" = -c ] && [ "$2" = "system reboot" ] || exit 99\n'
                          + 'echo reboot >>"' + str(self.reboot_calls) + '"\n')
        reboot.chmod(0o755)
        self.init_calls = self.root / 'legacy-init.calls'
        init = self.root / 'legacy-init.sh'
        init.write_text('#!/bin/ash\necho "$1" >>"' + str(self.init_calls) + '"\nexit 1\n')
        init.chmod(0o755)
        # Installed legacy bytes differ from target; never replace them in
        # the async handoff before the protected boot transition.
        (self.root / self.files[0]).write_text('legacy platform bytes\n')
        self.env.update(BRORAY_HANDOFF_SELF=str(script), BRORAY_HANDOFF_INIT_HOOK=str(init))

    def run_entry(self, command):
        return subprocess.run(['/bin/ash', '-x', str(HANDOFF), command], env=self.env,
                              capture_output=True, text=True, timeout=20)

    def test_03_first_transition_uses_preflight_then_one_reboot(self):
        self.transition_fixture()
        platform = {n: (self.root / n).read_bytes() for n in self.files}
        p = self.run_entry('finalize')
        self.assertEqual(p.returncode, 0, p.stdout + p.stderr)
        self.assertEqual(self.preflight_calls.read_text(), 'preflight\n')
        self.assertEqual(self.reboot_calls.read_text(), 'reboot\n')
        self.assertFalse(self.init_calls.exists(), 'plain legacy init must never control migration')
        self.assertEqual({n: (self.root / n).read_bytes() for n in self.files}, platform)
        state = json.loads((self.state / 'status.json').read_text())
        self.assertEqual(state['code'], 'PLATFORM_REBOOT_PENDING')
        self.assertTrue(state['running'])
        before = self.inventory()
        p = self.run_entry('schedule')
        self.assertEqual(p.returncode, 0, p.stdout + p.stderr)
        self.assertEqual(self.inventory(), before, 'another service start must not repeat reboot')

    def test_04_failed_preflight_never_reboots_or_replaces_platform(self):
        self.transition_fixture(dict(ok=False, errorCode='UPDATER_SERVICE_UNCONFIRMED'), 75)
        platform = {n: (self.root / n).read_bytes() for n in self.files}
        p = self.run_entry('finalize')
        self.assertNotEqual(p.returncode, 0)
        self.assertEqual(self.preflight_calls.read_text(), 'preflight\n')
        self.assertFalse(self.reboot_calls.exists())
        self.assertFalse(self.init_calls.exists())
        self.assertEqual({n: (self.root / n).read_bytes() for n in self.files}, platform)
        self.assertEqual(json.loads((self.state / 'status.json').read_text())['state'], 'error')


class BootSettlement(LegacyTransition):
    def prepare_completed_boot(self):
        self.transition_fixture()
        (self.root / self.files[0]).write_bytes((self.payload / self.files[0]).read_bytes())
        (self.state / 'phase').write_text('boot-pending\n')
        manifest=__import__('hashlib').sha256((self.payload/'SHA256SUMS').read_bytes()).hexdigest()
        self.receipt=dict(schemaVersion=1,contract='broray-first-platform-reboot/1',
            operationId=self.operation,candidateId='new',preflightOperationId='op-fixture-preflight',
            bootId='00000000-2222-3333-4444-555555555555',platformManifestSha256=manifest)
        (self.state/'reboot-request.json').write_text(json.dumps(self.receipt))
        pf=self.root/'opt/var/lib/broray/operations/op-fixture-preflight';pf.mkdir()
        (pf/'state.json').write_text(json.dumps(dict(operation='system:platform-preflight',
            state='completed',running=False,platformPreflight=dict(expectedPlatformManifestSha256=manifest))))
        self.proof_calls=self.root/'native-proof.calls'

    def settle(self, ready='1'):
        source=Path(os.environ.get('BRORAY_SETTLE_BASELINE',str(HANDOFF))).read_text()
        source=source.rsplit('case "${1:-}" in',1)[0]
        # Boundary double only for authenticated native readiness; settlement
        # state validation and exclusive owner checks are production functions.
        source+='\npreflight_installed() { echo proof >>"$TEST_PROOF"; pf_old_manifest="$1"; [ "$TEST_READY" = 1 ]; }\n'
        source+='preflight_settle_legacy "$(payload_manifest_sha)" "$CURRENT_PATH/app"\n'
        return subprocess.run(['/bin/ash','-c',source],env={**self.env,'TEST_PROOF':str(self.proof_calls),'TEST_READY':ready},capture_output=True,text=True,timeout=15)

    def test_settlement_requires_live_native_proof(self):
        self.prepare_completed_boot();r=self.settle();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
        self.assertEqual((self.state/'phase').read_text(),'complete\n')
        self.assertEqual(self.proof_calls.read_text(),'proof\n')
        self.assertEqual(json.loads((self.state/'status.json').read_text())['state'],'success')
        self.assertFalse((self.state/'worker.lock').exists())

    def test_settlement_same_boot_preserves_pending(self):
        self.prepare_completed_boot();self.receipt['bootId']='11111111-2222-3333-4444-555555555555'
        (self.state/'reboot-request.json').write_text(json.dumps(self.receipt));before=self.inventory()
        r=self.settle();self.assertNotEqual(r.returncode,0);self.assertEqual(self.inventory(),before)
        self.assertFalse(self.proof_calls.exists())

    def test_settlement_existing_fence_is_preserved(self):
        self.prepare_completed_boot();lock=self.state/'worker.lock';lock.mkdir();(lock/'foreign').write_text('preserve')
        before=self.inventory();r=self.settle();self.assertNotEqual(r.returncode,0);self.assertEqual(self.inventory(),before)

    def test_settlement_without_native_readiness_stays_pending(self):
        self.prepare_completed_boot();r=self.settle('0');self.assertNotEqual(r.returncode,0)
        self.assertEqual((self.state/'phase').read_text(),'boot-pending\n')
        self.assertEqual(self.proof_calls.read_text(),'proof\n')


class RealPreflightTransition(UpdaterPreflight):
    def test_finalize_stages_native_registration_before_single_reboot(self):
        import shutil
        current = self.root / 'opt/broray/current'
        shutil.copytree(self.code_root, current / 'app')
        (current / '.broray-slot').write_text('new--update-test\n')
        (current / 'release.json').write_text('{"candidateId":"new"}\n')
        state = self.root / 'opt/var/lib/broray-platform-handoff'
        state.mkdir(parents=True)
        (state / 'phase').write_text('queued\n')
        (state / 'request.json').write_text(json.dumps(dict(
            schemaVersion=1, contract='broray-universal-platform-handoff/1',
            operationId='update-real-preflight', previousSlot='old--bootstrap',
            targetSlot='new--update-test', candidateId='new',
            payloadManifestSha256=self.sha, createdAt='2026-10-02T12:00:00Z')))
        op = self.root / 'opt/var/lib/broray/operations/update-real-preflight'
        op.mkdir(parents=True)
        (op / 'state.json').write_text('{"state":"success","stage":"complete","running":false}')
        boot = self.root / 'proc/sys/kernel/random/boot_id'
        boot.parent.mkdir(parents=True)
        boot.write_bytes(Path('/proc/sys/kernel/random/boot_id').read_bytes())
        proc = self.home / 'process-starttime.sh'
        proc.write_text('#!/bin/ash\nawk \'NR==1 {print $22; exit}\' "/proc/$1/stat"\n')
        proc.chmod(0o755)
        reboot = current / 'app/bin/broray-system-ndmc'
        calls = self.home / 'reboot.calls'
        reboot.write_text('#!/bin/ash\n[ "$1" = -c ] && [ "$2" = "system reboot" ] || exit 99\n'
                          + 'echo reboot >>"' + str(calls) + '"\n')
        reboot.chmod(0o755)
        env = {**self.env, 'BRORAY_HANDOFF_SELF': str(HANDOFF),
               'BRORAY_HANDOFF_PROCESS_STARTTIME_HOOK': str(proc)}
        p = subprocess.run(['/bin/ash', str(HANDOFF), 'finalize'], env=env,
                           capture_output=True, text=True, timeout=120)
        details = '\n'.join(p.read_text() for p in state.glob('preflight-*') if p.is_file())
        self.assertEqual(p.returncode, 0, p.stdout + p.stderr + details)
        reply = json.loads(next(state.glob('preflight-result.*.json')).read_text())
        native_op = self.root / 'opt/var/lib/broray/operations' / reply['operationId']
        self.assertTrue((native_op / 'platform-bootguard/staged.receipt').is_file())
        self.assertTrue((native_op / 'platform-migration/intent.record').is_file())
        # Canonical bootguard replaces exactly entries 1 and 5 before reboot,
        # retaining their originals. It does not yet install the new platform.
        guarded = {'opt/etc/init.d/S22broray-updater': 1,
                   'opt/libexec/broray-updater/broray-updater.sh': 5}
        for name, before in self.expected_old.items():
            if name in guarded:
                self.assertEqual((native_op / 'platform-bootguard' /
                                  ('before-' + str(guarded[name]))).read_bytes(), before[0])
            else:
                self.assertEqual(self.snapshot()[name], before)
        binding = json.loads((native_op / 'platform-bootguard.json').read_text())
        verification = subprocess.run([
            str(current / 'app/bin/broray-updater-generation'), 'guard-verify-bound',
            str(native_op / 'platform-bootguard'), str(self.root),
            str(native_op / 'platform-migration'), binding['migrationIntentSha256']],
            capture_output=True, text=True, timeout=5)
        self.assertEqual(verification.returncode, 0, verification.stdout + verification.stderr)
        self.assertEqual(calls.read_text(), 'reboot\n')
        self.assertFalse(self.log.exists(), 'legacy init never supplies readiness')
        self.assertEqual(json.loads((state / 'status.json').read_text())['code'], 'PLATFORM_REBOOT_PENDING')


if __name__ == '__main__':
    unittest.main(verbosity=2, failfast=True)
