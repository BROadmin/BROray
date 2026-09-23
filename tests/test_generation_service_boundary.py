"""A supervised updater must never start application services as tracees.

Extract the exact production service_call function; fixtures replace only the
init script and installation root. No router/network access.
"""
from pathlib import Path
import hashlib,os,shlex,subprocess,unittest
from test_updater_generation import Generation

UPDATER=Path(os.environ.get('BRORAY_TEST_ROOT','/work/implementation'))/'runtime/app/share/updater-platform/opt/libexec/broray-updater/broray-updater.sh'

class ServiceBoundary(Generation):
 def setUp(self):
  super().setUp();self.domain.rmdir();root=self.home/'generations';root.mkdir(mode=0o700);self.domain=root/'domain';self.domain.mkdir(mode=0o700)
 def test_supervised_service_start_requires_independent_launcher(self):
  source=UPDATER.read_text();start=source.index('\nservice_call()\n')+1;end=source.index('\nservices_capture()\n',start)
  function=source[start:end];print('SERVICE_BOUNDARY_SOURCE '+hashlib.sha256(function.encode()).hexdigest(),flush=True)
  service=self.home/'opt/etc/init.d/S24broray';service.parent.mkdir(parents=True)
  service.write_text('#!/bin/ash\necho started >"$TEST_HOME/service.started"\n');service.chmod(0o755)
  body='SERVICE_HOOK=\nASH_BIN=/bin/ash\nroot_path() { printf "%s%s\\n" "$TEST_HOME" "$1"; }\n'+function+'\nservice_call start S24broray\nprintf "%s\\n" "$?" >"$TEST_HOME/service.rc.tmp"\nmv "$TEST_HOME/service.rc.tmp" "$TEST_HOME/service.rc"\nwhile :; do :; done\n'
  self.start(body);self.wait(lambda:(self.home/'service.rc').exists())
  self.assertNotEqual(int((self.home/'service.rc').read_text()),0,'supervised updater started application init in its writer generation')
  self.assertFalse((self.home/'service.started').exists(),'Xray init must not run without an independent authenticated launcher')
  self.assertEqual(self.call('STOP').returncode,0);self.stopped()

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([ServiceBoundary('test_supervised_service_start_requires_independent_launcher')]))
 raise SystemExit(0 if result.wasSuccessful() else 1)
