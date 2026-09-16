"""Route admission policy: isolated files/synthetic owners, no router access."""
import json, os, shutil, subprocess, unittest, uuid
from pathlib import Path
from test_operations import Operations, APP, BB, WORKSPACE


class RouteAdmission(unittest.TestCase):
    def setUp(self):
        self.ops=Operations('test_normal_finish_and_next_begin'); self.ops.setUp()
        self.app=self.ops.temp/'app'
        shutil.copytree(APP/'lib',self.app/'lib')
        self.progress=self.app/'routes/operations'; self.progress.mkdir(parents=True)
        self.ops.env['BRORAY_ROOT']=str(self.app)

    def tearDown(self): self.ops.tearDown()

    def call(self,*args,expected=0):
        # Operations.call runs the checked-in coordinator with this private app.
        return self.ops.call(*args,expected=expected)

    def pending(self,bundle='fixture',running=False):
        p=self.progress/(bundle+'.json')
        p.write_text(json.dumps({'schemaVersion':2,'kind':'routes','bundleId':bundle,
            'operation':'install','running':running,'resumable':not running,
            'phase':'failed_resumable','current':1,'total':3}))
        return p

    def begin(self,action='resume',bundle='fixture',expected=0,ack=True):
        a=self.call('begin','routes',action,bundle,'USER','900001','protected',uuid.uuid4().hex,expected=expected)
        if expected==0 and ack:self.call('ack',a['operationId'],a['token'],'900001')
        return a

    def test_same_bundle_resume_and_its_preflight_are_admitted(self):
        p=self.pending(); before=p.read_bytes()
        for action in ['preflight:resume','resume']:
            a=self.begin(action)
            self.call('finish',a['operationId'],a['token'],'completed','')
            self.assertEqual(p.read_bytes(),before)

    def test_other_bundle_and_unrelated_actions_stay_blocked(self):
        p=self.pending(); before=p.read_bytes()
        for action,bundle in [('resume','other'),('preflight:resume','other'),('download','fixture'),('delete','fixture'),('export','fixture')]:
            self.assertEqual(self.begin(action,bundle,expected=2)['errorCode'],'DOMAIN_OPERATION_BUSY')
        self.assertEqual(p.read_bytes(),before)

    def test_persisted_running_progress_is_not_proof_of_absence(self):
        self.pending(running=True)
        self.assertEqual(self.begin(expected=2)['errorCode'],'DOMAIN_OPERATION_BUSY')

    def test_resume_does_not_bypass_live_global_owner(self):
        a=self.ops.begin(); self.pending()
        self.assertEqual(self.begin(expected=2)['errorCode'],'OPERATION_BUSY')
        self.assertTrue(self.ops.opfile(a,'state.json').exists())

    def test_resume_does_not_bypass_unknown_resource_generation(self):
        self.pending(); lock=self.app/'routes/locks/operation.lock'; lock.mkdir(parents=True)
        (lock/'unknown').write_text('KEEP')
        self.assertEqual(self.begin(expected=2)['errorCode'],'DOMAIN_OPERATION_BUSY')
        self.assertEqual((lock/'unknown').read_text(),'KEEP')

    def test_resume_does_not_bypass_an_updater_transaction(self):
        self.pending(); (self.ops.temp/'updater/request.lock').mkdir(parents=True)
        self.assertEqual(self.begin(expected=2)['errorCode'],'DOMAIN_OPERATION_BUSY')


if __name__=='__main__':
    if os.name=='nt':raise SystemExit('Run in isolated Linux guest')
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(RouteAdmission))
    (WORKSPACE/'docs/evidence/route-admission-tests.json').write_text(json.dumps({
        'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,
        'environment':'Linux native guard, synthetic owners, private route state',
        'routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
