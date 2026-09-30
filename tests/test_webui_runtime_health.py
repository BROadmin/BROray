"""Actual component JSON projection with a controlled S25 status provider."""
import json,os,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
class Health(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.app=Path(self.tmp.name)
        (self.app/'init').mkdir();(self.app/'lib').mkdir();(self.app/'home.html').write_text('installed')
        service=self.app/'init/S25broray-web';service.write_text('#!/bin/ash\n[ "$1" = status ] || exit 99\nexit "${TEST_SERVICE_RC:-1}"\n');service.chmod(0o755)
        (self.app/'lib/web-publish.sh').write_text('broray_web_publish_status_json(){ printf \'%s\\n\' \'{"enabled":true,"consistent":false,"reason":{"code":"WEB_ACCESS_OWNERSHIP_MISMATCH"}}\'; }\n')
        text=(ROOT/'runtime/app/lib/broray-page.sh').read_text()
        self.script=text[text.index('broray_system_component_json()'):text.index('broray_system_parser_available()')]
    def query(self,rc):
        env={**os.environ,'BRORAY_BASE':str(self.app),'BRORAY_INIT_ROOT':str(self.app/'init'),'TEST_SERVICE_RC':str(rc)}
        p=subprocess.run(['/bin/ash','-c',self.script+'\nbroray_system_component_json webui WebUI "$BRORAY_BASE/home.html" true test'],env=env,capture_output=True,timeout=15)
        self.assertEqual(p.returncode,0,p.stderr);return json.loads(p.stdout)
    def test_file_present_service_down_unhealthy(self):self.assertFalse(self.query(1)['healthy'])
    def test_status_proves_local_health(self):self.assertTrue(self.query(0)['healthy'])
    def test_publication_failure_does_not_hide_local_health(self):
        data=self.query(0);self.assertTrue(data['healthy']);self.assertFalse(data['publication']['consistent'])
    def test_service_missing_unhealthy(self):
        (self.app/'init/S25broray-web').unlink();self.assertFalse(self.query(0)['healthy'])
    def canonical_link(self):
        service=self.app/'init/S25broray-web'
        target=self.app/'current/init/S25broray-web';target.parent.mkdir(parents=True)
        service.rename(target);service.symlink_to(target)
        return service,target
    def test_canonical_compact_init_symlink_proves_health(self):
        self.canonical_link();self.assertTrue(self.query(0)['healthy'])
    def test_canonical_compact_init_symlink_keeps_runtime_failure(self):
        self.canonical_link();self.assertFalse(self.query(1)['healthy'])
    def test_foreign_init_symlink_never_executes(self):
        service=self.app/'init/S25broray-web';target=self.app/'foreign'
        target.write_text('#!/bin/ash\ntouch "$BRORAY_BASE/foreign-executed"\nexit 0\n');target.chmod(0o755)
        service.unlink();service.symlink_to(target)
        self.assertFalse(self.query(0)['healthy']);self.assertFalse((self.app/'foreign-executed').exists())
    def test_canonical_target_cannot_be_symlink(self):
        service,target=self.canonical_link();other=self.app/'other';target.rename(other);target.symlink_to(other)
        self.assertFalse(self.query(0)['healthy'])
    def test_canonical_parent_cannot_be_symlink(self):
        self.canonical_link();parent=self.app/'current/init';other=self.app/'elsewhere';parent.rename(other);parent.symlink_to(other)
        self.assertFalse(self.query(0)['healthy'])
if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
