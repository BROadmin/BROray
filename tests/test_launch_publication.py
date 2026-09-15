"""Crash the isolated coordinator itself before each launch publication boundary."""
import json,os,subprocess,unittest,uuid
from test_operations import Operations,WORKSPACE,APP,GUARD,BB

class Launch(unittest.TestCase):
    setUp=Operations.setUp
    set_owner=Operations.set_owner
    call=Operations.call
    begin=Operations.begin
    opfile=Operations.opfile
    def crash_at(self,point):
        nonce=uuid.uuid4().hex
        p=subprocess.run([str(GUARD),str(self.state/'operations.guard'),str(BB),'ash',str(APP/'lib/operation-coordinator.sh'),
          'begin','system','subscriptions:scheduler','subscriptions','USER','900001','cooperative',nonce],
          env={**self.env,'BRORAY_OPS_TEST_LAUNCH_CRASH':point},capture_output=True,timeout=20)
        self.assertEqual(p.returncode,-9,(point,p.stdout,p.stderr))
        self.assertFalse((self.temp/'global.lock').exists());self.assertFalse((self.temp/'global.lock').is_symlink())
        snapshot=self.call('status');self.assertTrue(snapshot['ok'])
        if point!='published_directory':self.assertEqual(snapshot['operations'],[])
        else:self.assertEqual(len(snapshot['operations']),1)
        a=self.begin(launch=nonce)
        self.assertTrue(a['ok']);self.assertTrue((self.temp/'global.lock').is_symlink())
        self.assertEqual(list((self.state/'operations').glob('.launch-*')),[])
        self.call('finish',a['operationId'],a['token'],'completed','')
    def test_foreign_staging_object_is_preserved_without_blocking_new_job(self):
        stage=self.state/'operations'/('.launch-900003-'+uuid.uuid4().hex);stage.mkdir(parents=True)
        (stage/'foreign').write_text('KEEP')
        self.begin();self.assertEqual((stage/'foreign').read_text(),'KEEP')
    def test_acknowledged_record_cannot_be_deleted_as_launch_metadata(self):
        a=self.begin();self.call('finish',a['operationId'],a['token'],'completed','')
        stage=self.state/'operations'/('.launch-900001-'+self.launch);stage.mkdir()
        (stage/'owner.json').write_bytes(self.opfile(a,'owner.json').read_bytes())
        (stage/'state.json').write_bytes(self.opfile(a,'state.json').read_bytes())
        self.begin();self.assertTrue(stage.exists())
    def test_hidden_foreign_symlink_is_never_followed(self):
        foreign=self.temp/'foreign';foreign.mkdir();(foreign/'KEEP').write_text('KEEP')
        root=self.state/'operations';root.mkdir()
        stage=root/('.launch-900003-'+uuid.uuid4().hex);stage.symlink_to(foreign)
        self.begin();self.assertTrue(stage.is_symlink());self.assertEqual((foreign/'KEEP').read_text(),'KEEP')

for point in ['directory','owner','state','fence','published_directory']:
    def test(self,point=point):self.crash_at(point)
    setattr(Launch,'test_coordinator_death_after_'+point,test)

if __name__=='__main__':
    if os.name=='nt':raise SystemExit('Actual coordinator signals are Linux-only')
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Launch))
    (WORKSPACE/'docs/evidence/launch-publication-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'Linux kernel guard, actual self-KILL at launch boundaries; synthetic executor identity','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
