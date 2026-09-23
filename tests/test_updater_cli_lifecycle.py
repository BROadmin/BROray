"""Real external CLI calls must preserve the supervised persistent updater."""
import json, os, stat, subprocess, unittest
from pathlib import Path
from test_installed_generation_stop import InstalledGenerationStop


class UpdaterCliLifecycle(InstalledGenerationStop):
    def setUp(self):
        super().setUp()
        # Reuse the already validated disposable Entware dependency fixture.
        shell = Path(os.path.realpath(self.root / 'router/opt/bin/ash'))
        original_mode = stat.S_IMODE(shell.stat().st_mode)
        hidden = []
        def restore_dependencies():
            for original, saved in reversed(hidden):
                saved.rename(original)
            shell.chmod(original_mode)
        self.addCleanup(restore_dependencies)
        shell.chmod(0o4755)
        for directory in ['/opt/bin', '/opt/sbin', '/usr/bin', '/bin', '/usr/sbin', '/sbin']:
            original = Path(directory) / 'stat'
            if original.exists() or original.is_symlink():
                saved = original.with_name('stat-cli-fixture-saved')
                self.assertFalse(saved.exists() or saved.is_symlink())
                original.rename(saved)
                hidden.append((original, saved))

    def ready(self):
        self.installed()
        result = self.init('start')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        reply = json.loads(result.stdout)
        self.assertTrue(reply['platformReady'])
        return reply['generationId']

    def cli(self, *args):
        live = self.root / 'router'
        # Invoke the exact installed script with the same public arguments as
        # broray-updaterctl. No ASSUME_DAEMON, readiness stub, or fake /proc.
        env = {**os.environ, 'BRORAY_UPDATER_ROOT_PREFIX': str(live)}
        self.assertNotIn('BRORAY_UPDATER_GENERATION', env)
        self.assertNotIn('BRORAY_UPDATER_ASSUME_DAEMON', env)
        return subprocess.run([str(live / 'opt/bin/ash'),
                               str(live / 'opt/libexec/broray-updater/broray-updater.sh'), *args],
                              env=env, capture_output=True, text=True, timeout=30)

    def assert_still_ready(self, generation):
        result = self.init('status')
        logs = {str(p.relative_to(self.updater)): p.read_text()
                for p in (self.updater / 'starts').glob('*/supervisor.log')}
        print('REAL_CLI_LIFETIME ' + json.dumps({'rc': result.returncode,
              'stdout': result.stdout, 'stderr': result.stderr, 'supervisorLogs': logs}), flush=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr + json.dumps(logs))
        reply = json.loads(result.stdout)
        self.assertTrue(reply['platformReady'])
        self.assertEqual(reply['generationId'], generation)

    def test_status_preserves_live_generation(self):
        generation = self.ready()
        before = self.updater.stat()
        result = self.cli('status')
        after = self.updater.stat()
        print('REAL_CLI_STATUS ' + json.dumps({'rc': result.returncode,
              'stdout': result.stdout, 'stderr': result.stderr,
              'modeBefore': oct(before.st_mode), 'modeAfter': oct(after.st_mode),
              'ctimeChanged': before.st_ctime_ns != after.st_ctime_ns}), flush=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assert_still_ready(generation)
        self.assertEqual(before.st_ctime_ns, after.st_ctime_ns,
                         'Status must not chmod the live generation ancestor')
        # The exported boot intent is single-use in this VM. Exercise both
        # public calls against this same live generation before base cleanup.
        self.assert_request_validation_preserves_generation(generation)
        self.assert_unknown_layout_is_preserved(generation)
        self.assert_stopped_generation_rejects_foreign_pid()

    def assert_request_validation_preserves_generation(self, generation):
        result = self.cli('request', 'invalid-operation-for-fixture')
        print('REAL_CLI_REQUEST ' + json.dumps({'rc': result.returncode,
              'stdout': result.stdout, 'stderr': result.stderr}), flush=True)
        self.assertNotEqual(result.returncode, 0)
        reply = json.loads(result.stdout)
        self.assertEqual(reply['error']['code'], 'INVALID_OPERATION',
                         'Live generation must pass admission before argument validation')
        self.assert_still_ready(generation)
        self.assertFalse((self.updater / 'request.lock').exists())
        self.assertEqual(list((self.updater / 'queue').glob('*.json')), [])

    def assert_unknown_layout_is_preserved(self, generation):
        work = self.root / 'router/tmp/broray-updater-work'
        saved = work.with_name('work-cli-fixture-saved')
        self.assertTrue(work.is_dir())
        self.assertFalse(saved.exists())
        work.rename(saved)
        try:
            result = self.cli('status')
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(work.exists(), 'External status recreated missing managed state')
        finally:
            saved.rename(work)
        work.chmod(0o755)
        try:
            result = self.cli('status')
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(stat.S_IMODE(work.stat().st_mode), 0o755,
                             'External status silently repaired unknown permissions')
        finally:
            work.chmod(0o700)
        self.assert_still_ready(generation)

    def assert_stopped_generation_rejects_foreign_pid(self):
        result = self.init('stop')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertTrue(json.loads(result.stdout)['serviceStopped'])
        foreign = subprocess.Popen(['/bin/ash', '-c', 'while :; do sleep 1; done',
                                    'broray-updater-foreign-fixture', 'daemon'],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        hints = [self.updater / 'daemon.pid', self.updater / 'daemon.ready']
        try:
            for hint in hints:
                self.assertFalse(hint.exists() or hint.is_symlink())
                hint.write_text(str(foreign.pid) + '\n')
                hint.chmod(0o600)
            result = self.cli('request', 'invalid-operation-for-fixture')
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(json.loads(result.stdout)['error']['code'], 'UPDATER_NOT_RUNNING')
            self.assertIsNone(foreign.poll(), 'Client touched an unrelated process')
            self.assertFalse((self.updater / 'request.lock').exists())
            for hint in hints:
                self.assertEqual(hint.read_text(), str(foreign.pid) + '\n')
            print('REAL_CLI_FOREIGN_PID_REJECTED', flush=True)
        finally:
            for hint in hints:
                if hint.is_file() and hint.read_text() == str(foreign.pid) + '\n':
                    hint.unlink()
            if foreign.poll() is None:
                foreign.terminate()
            foreign.wait(timeout=5)


if __name__ == '__main__':
    suite = unittest.TestSuite(UpdaterCliLifecycle(name) for name in UpdaterCliLifecycle.__dict__
                              if name.startswith('test_'))
    result = unittest.TextTestRunner(verbosity=2, failfast=True).run(suite)
    raise SystemExit(not result.wasSuccessful())
