import os,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
class CleanNdmc(unittest.TestCase):
    def test_interface_dot_preflight_resolve_default_and_absolute_system_path(self):
        cases=[('interface-owner.sh','BRORAY_INTERFACE_NDMC','broray_interface_ndmc_path'),
               ('routes-dot.sh','BRORAY_DOT_NDMC','broray_dot_ndmc_path'),
               ('routes-operation-preflight.sh','BRORAY_ROUTES_OPERATION_PREFLIGHT_NDMC','broray_routes_operation_preflight_ndmc_path')]
        with tempfile.TemporaryDirectory() as d:
            d=Path(d);(d/'bin').mkdir();fake=d/'ndmc'
            fake.write_text('#!/bin/ash\n[ "${LD_LIBRARY_PATH+x}" != x ] && [ "${LD_PRELOAD+x}" != x ] || exit 98\nprintf "%s\\n" "$*"\n');fake.chmod(0o755)
            helper=d/'bin/broray-system-ndmc';helper.write_text((ROOT/'runtime/app/bin/broray-system-ndmc').read_text().replace('/bin/ndmc',str(fake)));helper.chmod(0o755)
            for name,var,function in cases:
                for value in ['', 'ndmc', '/bin/ndmc']:
                    with self.subTest(library=name,executable=value):
                        env={**os.environ,'BRORAY_ROOT':str(d),'BRORAY_BASE':str(d),'BRORAY_PROXY_HOST':'192.168.1.1','BRORAY_PROXY_PORT':'2080',var:value,'LD_LIBRARY_PATH':'/opt/lib','LD_PRELOAD':'missing.so'}
                        p=subprocess.run(['/bin/ash','-c','. "$1"; helper="$('+function+')" || exit 90; "$helper" -c "show running-config"; rc=$?; [ "$LD_LIBRARY_PATH" = /opt/lib ] || exit 91; exit "$rc"','test',str(ROOT/'runtime/app/lib'/name)],env=env,capture_output=True,timeout=10)
                        self.assertEqual(p.returncode,0,(p.stdout,p.stderr));self.assertEqual(p.stdout,b'-c show running-config\n')
    def test_system_dispatch_preserves_arguments_rc_and_parent_environment(self):
        with tempfile.TemporaryDirectory() as d:
            d=Path(d);fake=d/'ndmc';helper=d/'helper'
            fake.write_text('#!/bin/ash\n[ "${LD_LIBRARY_PATH+x}" != x ] && [ "${LD_PRELOAD+x}" != x ] || exit 98\nprintf "arg1=%s\\narg2=%s\\n" "$1" "$2"\necho diagnostic >&2\nexit 7\n');fake.chmod(0o755)
            source=(ROOT/'runtime/app/bin/broray-system-ndmc').read_text()
            # The production endpoint stays absolute, with no environment switch.
            self.assertIn('exec /bin/ndmc "$@"',source)
            helper.write_text(source.replace('/bin/ndmc',str(fake)))
            env={**os.environ,'LD_LIBRARY_PATH':'/opt/lib:/opt/usr/lib','LD_PRELOAD':'fixture-missing.so'}
            code='ash "$1" -c "show interface Proxy0"; rc=$?; printf "parent=%s|%s\\n" "$LD_LIBRARY_PATH" "$LD_PRELOAD";exit "$rc"'
            p=subprocess.run(['/bin/ash','-c',code,'ash',str(helper)],env=env,capture_output=True,timeout=10)
            self.assertEqual(p.returncode,7,p.stderr)
            self.assertIn(b'arg1=-c\narg2=show interface Proxy0\n',p.stdout)
            self.assertIn(b'parent=/opt/lib:/opt/usr/lib|fixture-missing.so\n',p.stdout)
            self.assertIn(b'diagnostic',p.stderr)
if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
