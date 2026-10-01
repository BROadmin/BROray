"""Bundled command execution without an optional Entware timeout applet."""
import os, shutil, subprocess, tempfile, time, unittest
from pathlib import Path

RUNNER=Path(os.environ.get('BRORAY_TEST_NDMC_RUNNER','/.local/bin/linux-ndmc-run'))

def install_bounded_helper(app):
    root=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
    (app/'bin').mkdir(parents=True,exist_ok=True)
    shutil.copyfile(root/'runtime/app/bin/broray-timeout',app/'bin/broray-timeout')
    shutil.copyfile(RUNNER,app/'bin/broray-ndmc-run')
    for name in ['broray-timeout','broray-ndmc-run']:(app/'bin'/name).chmod(0o755)

class BoundedExecution(unittest.TestCase):
    def command(self,seconds,*args,grace=1):
        return [str(RUNNER),'--exec',str(seconds),str(grace),'--',*args]

    def test_output_arguments_and_status_without_timeout(self):
        with tempfile.TemporaryDirectory() as d:
            # No PATH command is available; executable and argv stay literal.
            result=subprocess.run(self.command(3,'/bin/ash','-c',
                'printf "%s\\n" "$1"; echo diagnostic >&2; exit 23','probe','a ; $(bad) +'),
                env={**os.environ,'PATH':d},capture_output=True,timeout=8)
            self.assertEqual((result.returncode,result.stdout,result.stderr),
                             (23,b'a ; $(bad) +\n',b'diagnostic\n'))

    def test_fast_completion_does_not_wait_for_deadline(self):
        start=time.monotonic()
        result=subprocess.run(self.command(120,'/bin/true'),timeout=5)
        self.assertEqual(result.returncode,0)
        self.assertLess(time.monotonic()-start,5)

    def test_timeout_drains_detached_writer(self):
        with tempfile.TemporaryDirectory() as d:
            marker=Path(d)/'late'
            result=subprocess.run(self.command(1,'/bin/ash','-c',
                'setsid /bin/ash -c \'sleep 3; echo BAD >"$1"\' child "$1" & wait',
                'parent',str(marker)),capture_output=True,timeout=8)
            self.assertEqual(result.returncode,124,result.stderr)
            time.sleep(3)
            self.assertFalse(marker.exists())

    def test_foreign_process_preserved(self):
        foreign=subprocess.Popen(['/bin/sleep','15'])
        try:
            result=subprocess.run(self.command(1,'/bin/sleep','15'),timeout=6)
            self.assertEqual(result.returncode,124)
            self.assertIsNone(foreign.poll())
        finally:
            foreign.terminate();foreign.wait(timeout=5)

    def test_no_command_on_invalid_budget(self):
        with tempfile.TemporaryDirectory() as d:
            marker=Path(d)/'bad'
            for seconds in ['0','-1','1x','3601']:
                result=subprocess.run(self.command(seconds,'/bin/touch',str(marker)),timeout=5)
                self.assertEqual(result.returncode,64)
                self.assertFalse(marker.exists())

    def test_exec_failure_is_not_success(self):
        result=subprocess.run(self.command(2,'/nonexistent/command'),timeout=5)
        self.assertEqual(result.returncode,127)

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
