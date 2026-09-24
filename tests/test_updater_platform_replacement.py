"""An authenticated new platform must not be mistaken for a failed replay."""
import json, shutil, unittest, os, select, signal
from pathlib import Path
from test_updater_preflight import PreflightAfterBoot, TARGETS, UPDATER, digest
from test_installed_generation_stop import InstalledGenerationStop


class ReplacementFixture(InstalledGenerationStop):
    def clear_start(self):
        # All generations were drained by stop_created_generation. Match each
        # remaining test host to that generation's retained executable; the
        # original single-runtime cleanup cannot represent A, B and C together.
        hosts = self.updater/'hosts'
        if hosts.exists():
            for record in hosts.glob('*/host.record'):
                rows = record.read_text().splitlines()
                self.assertEqual(rows[0], 'BROray-independent-app-service/1')
                self.assertEqual(rows[1], str(record.parent))
                self.assertEqual(rows[5], str(self.root/'router'))
                launch = (self.updater/'starts'/record.parent.name/'launch.record').read_text().splitlines()
                self.assertEqual(launch[3], record.parent.name)
                runtime = self.updater/'runtimes'/launch[5]/'runtime'
                self.assertEqual(digest(runtime), launch[5])
                owner = json.loads(rows[-1])
                self.assertEqual(owner['executable'], str(runtime))
                proc = Path('/proc')/str(owner['pid'])
                if not proc.exists():
                    continue
                fd = os.pidfd_open(owner['pid'])
                try:
                    self.assertEqual(proc.joinpath('stat').read_text().rsplit(') ',1)[1].split()[19], owner['startTicks'])
                    self.assertEqual(Path('/proc/sys/kernel/random/boot_id').read_text().strip(), owner['bootId'])
                    self.assertEqual(os.readlink(proc/'exe'), str(runtime))
                    self.assertEqual(digest(proc/'cmdline'), owner['commandDigest'])
                    signal.pidfd_send_signal(fd, signal.SIGTERM)
                    self.assertTrue(select.select([fd], [], [], 3)[0], 'owned fixture host did not exit')
                finally:
                    os.close(fd)
            shutil.rmtree(hosts)
        super().clear_start()

    def stop_created_generation(self):
        original = self.native
        try:
            if hasattr(self, 'replacement_native'):
                self.native = self.replacement_native
            super().stop_created_generation()
        finally:
            # The earlier fixture's host still belongs to its original native.
            self.native = original


class PlatformReplacement(PreflightAfterBoot):
    # Replacement leaves retired A and live B; use the existing fixture whose
    # exact-generation cleanup accounts for both, rather than restoring A.
    fixture_type = ReplacementFixture
    def test_healthy_platform_accepts_distinct_target(self):
        first = self.completed(self.bootstrap(self.slot, self.slot_sha))
        origin = self.parent_fixture.op
        old_state = (origin / 'state.json').read_bytes()
        old_manifest = self.sha
        old_platform = self.snapshot()
        status = self.parent_fixture.init('status')
        self.assertEqual(status.returncode, 0, status.stdout + status.stderr)
        self.assertEqual(json.loads(status.stdout)['generationId'], first['generationId'])
        target_slot = self.home / 'next-authenticated-slot'
        shutil.copytree(self.slot, target_slot)
        self.addCleanup(shutil.rmtree, target_slot)
        new_native_sha = None
        if getattr(self, 'different_native', False):
            # A different executable identity with the same fixture ABI. ELF
            # ignores the trailing marker; every hash/provenance check must
            # nevertheless distinguish the old and new retained runtimes.
            native = target_slot/'app/bin/broray-updater-generation'
            old_native_sha = digest(native)
            native.write_bytes(native.read_bytes() + b'\nBROray cross-native fixture B\n')
            new_native_sha = digest(native)
            self.assertNotEqual(old_native_sha, new_native_sha)
        payload = target_slot / 'app/share/updater-platform'
        daemon = payload / UPDATER
        daemon.write_bytes(daemon.read_bytes() + b'\n# Distinct authenticated updater fixture B.\n')
        inner = payload / 'SHA256SUMS'
        inner.write_text(''.join(digest(payload/n) + '  ' + n + '\n' for n in TARGETS))
        self.sha = digest(inner)
        self.assertNotEqual(self.sha, old_manifest)
        outer = target_slot / 'SHA256SUMS'
        outer.write_text(''.join(digest(p) + '  ' + p.relative_to(target_slot).as_posix() + '\n'
                                for p in sorted(target_slot.rglob('*')) if p.is_file() and p != outer))
        result = self.bootstrap(target_slot, digest(outer))
        print('PLATFORM_A_TO_B ' + json.dumps(dict(returnCode=result.returncode,
              stdout=result.stdout, stderr=result.stderr, oldManifest=old_manifest,
              targetManifest=self.sha, oldGeneration=first['generationId'])), flush=True)
        # Preserve the completed origin; an update is a NEW transaction.
        self.assertEqual((origin / 'state.json').read_bytes(), old_state)
        replies = [json.loads(line) for line in result.stdout.splitlines() if line.startswith('{')]
        self.assertEqual(len(replies), 1, result.stdout + result.stderr)
        reply = replies[0]
        self.assertIn(reply.get('phase'), ['REBOOT_REQUIRED', 'PREFLIGHT_COMPLETED'],
                      'A confirmed installed platform needs a protected A-to-B transition')
        self.assertNotEqual(reply['operationId'], origin.name)
        if reply['phase'] == 'REBOOT_REQUIRED':
            self.assertEqual(result.returncode, 75)
            self.assertEqual(reply['errorCode'], 'UPDATER_LEGACY_REBOOT_REQUIRED')
            self.assertFalse(reply['platformReady'])
            self.assertEqual(self.snapshot(), old_platform)
        else:
            self.assertEqual(result.returncode, 0)
            self.assertTrue(reply['platformReady'])
            for name in TARGETS:
                self.assertEqual((self.root/name).read_bytes(), (payload/name).read_bytes())
            states = {p: p.read_bytes() for p in
                      (self.root/'opt/var/lib/broray/operations').glob('op-*/state.json')}
            repeated = self.bootstrap(target_slot, digest(outer))
            self.assertEqual(repeated.returncode, 0, repeated.stdout + repeated.stderr)
            self.assertEqual(json.loads(repeated.stdout)['generationId'], reply['generationId'])
            self.assertEqual(states, {p: p.read_bytes() for p in states})
            status = self.parent_fixture.init('status')
            self.assertEqual(status.returncode, 0, status.stdout + status.stderr)
            self.assertEqual(json.loads(status.stdout)['generationId'], reply['generationId'])
            if new_native_sha:
                domain = self.parent_fixture.updater/'generations'/reply['generationId']
                ledger = json.loads((domain/'state.json').read_bytes())
                self.assertEqual(ledger['platformLaunch']['nativeSha256'], new_native_sha)
                # Teardown must talk to live B with B's authenticated executable.
                self.parent_fixture.replacement_native = self.parent_fixture.updater/'runtimes'/new_native_sha/'runtime'

    def test_distinct_native_uses_old_runtime_only_for_old_generation(self):
        self.different_native = True
        self.test_healthy_platform_accepts_distinct_target()
        target = self.home/'next-authenticated-slot'
        native = target/'app/bin/broray-updater-generation'
        native.write_bytes(native.read_bytes() + b'\nBROray native-only fixture C\n')
        expected_native = digest(native)
        outer = target/'SHA256SUMS'
        outer.write_text(''.join(digest(p) + '  ' + p.relative_to(target).as_posix() + '\n'
                                for p in sorted(target.rglob('*')) if p.is_file() and p != outer))
        before = self.snapshot()
        result = self.bootstrap(target, digest(outer))
        print('NATIVE_ONLY_REPLACEMENT ' + json.dumps(dict(rc=result.returncode,
              stdout=result.stdout, stderr=result.stderr)), flush=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.parent_fixture.replacement_native = self.parent_fixture.updater/'runtimes'/expected_native/'runtime'
        reply = json.loads(result.stdout)
        self.assertEqual(reply['phase'], 'PREFLIGHT_COMPLETED')
        self.assertEqual(self.snapshot(), before)
        repeated = self.bootstrap(target, digest(outer))
        self.assertEqual(repeated.returncode, 0, repeated.stdout + repeated.stderr)
        self.assertEqual(json.loads(repeated.stdout)['generationId'], reply['generationId'])
        status = self.parent_fixture.init('status')
        self.assertEqual(status.returncode, 0, status.stdout + status.stderr)
        self.assertEqual(json.loads(status.stdout)['generationId'], reply['generationId'])


if __name__ == '__main__':
    result = unittest.TextTestRunner(verbosity=2, failfast=True).run(
        unittest.TestSuite([PlatformReplacement('test_healthy_platform_accepts_distinct_target')]))
    raise SystemExit(not result.wasSuccessful())
