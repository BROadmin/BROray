"""Existing full service-cycle assertions under actual Entware dependency modes."""
from pathlib import Path
import os,stat,unittest
from test_installed_service_cycles import ServiceCycles

class EntwarePlatformLifecycle(ServiceCycles):
 def test_setuid_busybox_and_applet_stat_full_service_cycles(self):
  self.assertTrue(Path('/work/tools/guest_suite_runner.py').is_file())
  shell=Path(os.path.realpath(self.root/'router/opt/bin/ash'))
  mode=stat.S_IMODE(shell.stat().st_mode);hidden=[]
  try:
   shell.chmod(0o4755)
   # Only the disposable VM: reproduce Entware without standalone stat.
   for directory in ['/opt/bin','/opt/sbin','/usr/bin','/bin','/usr/sbin','/sbin']:
    p=Path(directory)/'stat'
    if p.exists() or p.is_symlink():
     destination=p.with_name('stat-entware-fixture-saved')
     self.assertFalse(destination.exists() or destination.is_symlink())
     p.rename(destination);hidden.append((p,destination))
   self.test_multiple_cycles_idempotent_start_and_public_restart()
  finally:
   for p,destination in reversed(hidden):destination.rename(p)
   shell.chmod(mode)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([EntwarePlatformLifecycle('test_setuid_busybox_and_applet_stat_full_service_cycles')]))
 raise SystemExit(not r.wasSuccessful())
