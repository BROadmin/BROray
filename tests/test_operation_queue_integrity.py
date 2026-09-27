"""Bounded receipts must not lose live request identity or recreate its queue."""
import json
import os
import shutil
import subprocess
from pathlib import Path
import test_operations as core
import unittest
import test_operation_resources as support


class QueueIntegrity(unittest.TestCase):
    setUp = support.OperationResources.setUp
    tearDown = support.OperationResources.tearDown
    set_owner = support.OperationResources.set_owner
    call = support.OperationResources.call
    submit = support.OperationResources.submit
    queue_file = support.OperationResources.queue_file
    claim = support.OperationResources.claim
    ack = support.OperationResources.ack
    opfile = support.OperationResources.opfile

    def without_stat(self):
        # Entware on the actual KN-2710 has BusyBox stat, but no stat command.
        # A private PATH excludes standalone stat without a production switch.
        tools = self.temp/'without-stat'
        tools.mkdir()
        busybox = shutil.which('busybox', path=self.env['PATH'])
        self.assertIsNotNone(busybox)
        applets = subprocess.check_output([busybox, '--list'], text=True).splitlines()
        for name in set(applets + ['busybox']) - {'stat', 'jq'}:
            os.symlink(busybox, tools/name)
        os.symlink(shutil.which('jq', path=self.env['PATH']), tools/'jq')
        self.env['PATH'] = str(tools)

    def test_queue_works_without_standalone_stat(self):
        self.without_stat()
        self.call('pause')  # Existing installation has durable manager directories.
        ram = self.temp/'ram'
        ram.mkdir(mode=0o700)
        status = self.call('status')
        self.assertTrue(status['complete'], status)
        self.call('resume')  # Allow automatic work after the read-only status check.
        request = self.submit()
        self.assertEqual(self.call('queue-next')['requestId'], request['requestId'])
        operation = self.claim(request)
        self.ack(operation)
        self.call('finish', operation['operationId'], operation['token'], 'completed', '')
        self.assertTrue(self.call('status')['complete'])

    def metadata(self, file, fmt, client=False):
        lib = core.APP/'lib'/('operation-client.sh' if client else 'operation-owner.sh')
        name = 'broray_ops_stat' if client else 'broray_ops_file_stat'
        return subprocess.run([str(core.BB), 'ash', '-c',
            '. "$1"; '+name+' -c "$2" "$3"', 'metadata-test', str(lib), fmt, str(file)],
            env=self.env, capture_output=True, text=True, timeout=10)

    def test_available_stat_does_not_add_intermediate_shell_processes(self):
        # Native guard observes every fork: metadata wrappers must not add a
        # shell around an already available command. This detects process
        # amplification directly, without a timing threshold or sleep.
        for client in (False, True):
            lib = core.APP/'lib'/('operation-client.sh' if client else 'operation-owner.sh')
            name = 'broray_ops_stat' if client else 'broray_ops_file_stat'
            script = '''. "$1"
IFS=' ' read -r caller rest </proc/self/stat
stat() {
    IFS=' ' read -r actual rest </proc/self/stat
    [ "$actual" = "$caller" ] || { printf 'EXTRA_METADATA_SHELL %s %s\\n' "$caller" "$actual" >&2; return 97; }
    printf '640\\n'
}
'''+name+' -c %a /unused\n'
            with self.subTest(client=client):
                result = subprocess.run([str(core.BB), 'ash', '-c', script, 'stat-process-test', str(lib)],
                    env=self.env, capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout, '640\n')

    def test_busybox_metadata_formats_preserve_mode_uid_links_and_size(self):
        self.without_stat()
        file = self.temp/'file with spaces'
        file.write_bytes(b'test metadata')
        file.chmod(0o640)
        st = file.stat()
        formats = {'%a': '640', '%a:%u': f'640:{st.st_uid}',
                   '%u:%a': f'{st.st_uid}:640',
                   '%u:%a:%h': f'{st.st_uid}:640:1',
                   '%s:%u:%a:%h': f'{st.st_size}:{st.st_uid}:640:1',
                   '%u %a %h %s': f'{st.st_uid} 640 1 {st.st_size}'}
        for client in (False, True):
            for fmt, expected in formats.items():
                with self.subTest(client=client, format=fmt):
                    r = self.metadata(file, fmt, client)
                    self.assertEqual(r.returncode, 0, r.stderr)
                    self.assertEqual(r.stdout.strip(), expected)
        file.unlink()
        self.assertNotEqual(self.metadata(file, '%a').returncode, 0)

    def test_statless_queue_keeps_file_safety_checks(self):
        self.without_stat()
        self.submit()
        file = self.queue_file()
        before = file.read_bytes()
        file.chmod(0o644)
        self.assertEqual(self.call('queue-next', expected=2)['errorCode'], 'QUEUE_STATE_INVALID')
        self.assertEqual(file.read_bytes(), before)
        file.chmod(0o600)
        other = file.with_name('other-link')
        os.link(file, other)
        self.assertEqual(self.call('queue-next', expected=2)['errorCode'], 'QUEUE_STATE_INVALID')
        other.unlink()
        file.rename(other)
        file.symlink_to(other)
        self.assertEqual(self.call('queue-next', expected=2)['errorCode'], 'QUEUE_STATE_INVALID')
        self.assertEqual(other.read_bytes(), before)

    def test_existing_stat_failure_is_not_masked_by_busybox_fallback(self):
        self.without_stat()
        command = self.temp/'without-stat/stat'
        command.write_text('#!'+str(core.BB)+' sh\nexit 23\n')
        command.chmod(0o755)
        self.assertEqual(self.metadata(self.temp, '%a').returncode, 23)

    def test_live_nonce_remains_bound_after_receipt_eviction(self):
        nonce = '7'*32
        self.submit(nonce=nonce)
        data = json.loads(self.queue_file().read_bytes())
        # Equivalent bounded-receipt eviction while the original job is alive.
        data['receipts'] = []
        self.queue_file().write_text(json.dumps(data))
        before = self.queue_file().read_bytes()
        self.assertEqual(self.submit(target='different', nonce=nonce, expected=2)['errorCode'], 'REQUEST_MISMATCH')
        self.assertEqual(self.queue_file().read_bytes(), before)
        self.assertEqual(self.submit(nonce=nonce)['requestId'], 'q-'+nonce)

    def test_missing_namespace_with_same_boot_step_is_not_recreated(self):
        operation = self.claim(self.submit())
        self.ack(operation)
        self.queue_file().unlink()
        self.queue_file().parent.rmdir()
        self.assertEqual(self.submit(expected=2)['errorCode'], 'QUEUE_STATE_INVALID')
        self.assertFalse(self.queue_file().parent.exists())
        self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_queue_priority_tamper_is_rejected_before_selection(self):
        self.submit()
        data = json.loads(self.queue_file().read_bytes())
        data['requests'][0]['priority'] = 0
        self.queue_file().write_text(json.dumps(data))
        before = self.queue_file().read_bytes()
        self.assertEqual(self.call('queue-next', expected=2)['errorCode'], 'QUEUE_STATE_INVALID')
        self.assertEqual(self.queue_file().read_bytes(), before)


if __name__ == '__main__':
    unittest.main(verbosity=2, failfast=True)
