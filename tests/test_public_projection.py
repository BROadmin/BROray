"""Secret canaries test the shared public projection, not a collection of regex masks."""
import json,os,subprocess,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
JQ=str(ROOT/'.local/bin/jq.exe') if os.name=='nt' else '/usr/bin/jq'
LIB=ROOT/'implementation/runtime/app/lib'
CANARIES=['token_abc-secret','927e780f-d115-47ef-9972-26dcc5bc0149','PASSWORD_secret',
          'https://user:secret@private.example/subscription/SECRET?token=abc',
          'PRIVATE_KEY_secret','Authorization: Bearer SECRET','Cookie: session=SECRET','REALITY_SECRET']

class Public(unittest.TestCase):
    def project(self,value,kind):
        code=(LIB/'operation-public.jq').read_text(encoding='utf-8')+'\n'+kind if os.name=='nt' else 'include "operation-public"; '+kind
        p=subprocess.run([JQ,'-c','-L','implementation/runtime/app/lib',code],cwd=ROOT,input=json.dumps(value).encode(),capture_output=True,timeout=10)
        self.assertEqual(p.returncode,0,p.stderr)
        return json.loads(p.stdout)
    def test_every_public_operation_text_field_rejects_secret_canaries(self):
        keys=['operationId','type','source','state','phase','cancelability','startedAt','updatedAt','finishedAt','errorCode','ownerStatus','ownerReason']
        for canary in CANARIES:
            data=self.project({k:canary for k in keys},'operation_public')
            self.assertNotIn(canary,json.dumps(data))
    def test_event_message_is_catalogue_text(self):
        for canary in CANARIES:
            data=self.project({'event':'started','message':canary,'password':canary,'token':canary,'subscriptionURL':canary},'event_public')
            self.assertEqual(data['message'],'Операция запущена')
            self.assertNotIn(canary,json.dumps(data))
    def test_unknown_event_cannot_become_raw_message(self):
        data=self.project({'event':CANARIES[3],'operationType':CANARIES[3],'errorCode':CANARIES[0]},'event_public')
        self.assertEqual(data['event'],'unknown');self.assertIsNone(data['errorCode'])
    def test_nested_extra_values_are_dropped(self):
        data=self.project({'owner':{'token':CANARIES[0]},'rawLog':CANARIES,'event':'completed'},'event_public')
        self.assertNotIn('owner',data);self.assertNotIn('rawLog',data)
    def test_operation_id_is_not_private_nonce(self):
        good='op-20260915123456-900001-deadbeef1234'
        self.assertEqual(self.project({'operationId':good},'operation_public')['operationId'],good)
        self.assertIsNone(self.project({'operationId':CANARIES[1]},'operation_public')['operationId'])
    def test_unsafe_pid_and_bad_types_are_not_exposed(self):
        for value in [-1,1,2**40,'900001',[],{}]:
            self.assertIsNone(self.project({'pid':value},'event_public')['pid'])
    def test_internal_action_maps_to_public_type(self):
        self.assertEqual(self.project({'type':'subscriptions:scheduler'},'operation_public')['type'],'subscription_update')
        self.assertEqual(self.project({'type':'auto-switch'},'operation_public')['type'],'auto_switch')

if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Public))
    report={'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'jq public allowlist projection; synthetic secret canaries','routerAccessed':False}
    (ROOT/'docs/evidence/public-projection-tests.json').write_text(json.dumps(report,indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
