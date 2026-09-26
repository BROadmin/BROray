"""Reproductions from the physical r01c14 audit, using isolated Linux inputs."""
import base64,json,os,shutil,subprocess,tempfile,unittest
from pathlib import Path
from test_subscription_vless_pipeline import Pipeline,CORE_RESULTS
from test_subscription_vless_compat import profile

ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))

class LiveAudit(unittest.TestCase):
    setUp=Pipeline.setUp
    tearDown=Pipeline.tearDown
    shell=Pipeline.shell
    extract=Pipeline.extract
    generate=Pipeline.generate

    def test_redundant_xhttp_mode_requires_identical_effective_value(self):
        for mode,extra,accepted in [('auto',{'mode':'auto'},True),('auto',{'mode':''},True),
                ('auto',{'mode':'stream-one'},False),('auto',{'madeUp':True},False),
                ('auto',{'xPaddingBytes':'broken'},False)]:
            with self.subTest(mode=mode,extra=extra):
                data=json.dumps(dict(network='xhttp',xhttp=dict(mode=mode,extra=extra))).encode()
                r=subprocess.run(['jq','-es','--arg','validate_model','yes','--argjson','max_nodes','1','-f',str(self.app/'lib/subscription-xray-json.jq')],input=data,capture_output=True)
                self.assertEqual(r.returncode==0,accepted,r.stderr)

    def test_four_bom_subscription_forms_preserve_semantics_and_xray_config(self):
        uri=b'vless://11111111-2222-4333-8444-555555555555@example.invalid:443?type=tcp&security=tls&sni=example.invalid'
        for content in [uri,json.dumps(profile()).encode()]:
            for encoded in [False,True]:
                with self.subTest(kind='uri' if content==uri else 'json',base64=encoded):
                    payload=b'\xef\xbb\xbf'+content
                    result,nodes=self.extract(base64.b64encode(payload) if encoded else payload)
                    self.assertEqual((result['rc'],result['accepted'],result['rejected']),(0,1,0),result)
                    self.generate(nodes[0])

    def test_report_streams_large_journal_without_loss(self):
        events=[dict(sequence=i,event='phase_changed',message='x'*160) for i in range(1600)]
        journal=dict(complete=True,truncated=False,errors=[],events=events)
        raw=json.dumps(journal);self.assertGreater(len(raw),131072)
        (self.app/'large-journal.json').write_text(raw)
        (self.app/'lib/operation-report-facts.sh').write_text('''
ops_report_automation(){ echo '{"complete":true,"errors":[],"autoSwitch":false,"serverCheck":false,"subscriptionUpdate":false}'; }
ops_report_services(){ echo '[]'; }
ops_report_xray(){ echo '{"complete":true,"state":"unknown"}'; }
ops_report_updater(){ echo '{"complete":true}'; }
''')
        r=self.shell('''OPS_APP="$BRORAY_ROOT"; OPS_CODE="$OPS_APP"; OPS_PROC=/proc; OPS_UPDATER="$OPS_APP/updater"
ops_status(){ echo '{"complete":true,"operations":[],"errors":[],"automationPaused":true,"globalFence":"absent"}'; }
ops_journal_snapshot(){ cat "$OPS_APP/large-journal.json"; }
ops_file_safe(){ return 1; }
ops_pending_domain(){ return 1; }
ops_now(){ echo 2026-09-26T00:00:00Z; }
. "$OPS_APP/lib/operation-report.sh"
ops_report
''')
        self.assertEqual(r.returncode,0,r.stderr)
        report=json.loads(r.stdout);self.assertEqual(report['events'],events)
        self.assertTrue(report['journal']['complete']);self.assertFalse(report['journal']['truncated'])

    def test_overview_uses_observed_presence_instead_of_old_registry(self):
        (self.app/'routes/installed').mkdir(parents=True)
        (self.app/'routes/installed/routes.json').write_text(json.dumps({'routes':[{'owners':['user-old','wikipedia']}]}))
        snapshot=dict(schemaVersion=1,generatedAt='2026-09-26T00:00:00Z',managedInterface='Proxy0',managedInterfaceDisplay='BROray',globalOperation={},operation={},
            health=dict(severity='warning',actionRequired=True),bundles=[
                dict(id='user-old',installedVersion={'version':'old'},verifiedInstalled=False,attention=True,routeCount=131),
                dict(id='wikipedia',verifiedInstalled=True,attention=False,routeCount=12)])
        (self.app/'snapshot.json').write_text(json.dumps(snapshot))
        r=self.shell('''. "$BRORAY_ROOT/lib/routes-page-summary.sh"
broray_routes_page_summary(){ [ "$1" = all ] && cp "$BRORAY_ROOT/snapshot.json" "$2"; }
broray_routes_page_overview "$BRORAY_ROOT/overview.json" && cat "$BRORAY_ROOT/overview.json"
''')
        self.assertEqual(r.returncode,0,r.stderr);result=json.loads(r.stdout)
        self.assertEqual(result['custom']['installed'],0);self.assertEqual(result['custom']['attention'],1)
        self.assertEqual(result['catalog']['installed'],1);self.assertEqual(result['health'],snapshot['health'])
        self.assertEqual(result['sharedRoutes'],1)

    def preview(self,uri):
        api=self.app/'web-new/api';(api/'servers').mkdir(parents=True,exist_ok=True)
        (api/'auth-common.sh').write_text('''broray_api_require_method(){ [ "$REQUEST_METHOD" = "$1" ] || exit 90; }
broray_api_require_session(){ :; }
broray_api_success(){ jq -nc --argjson data "$1" '{success:true,data:$data}'; exit 0; }
broray_api_error(){ jq -nc --arg code "$2" --arg message "$3" '{success:false,error:{code:$code,message:$message}}'; exit 0; }
''')
        shutil.copyfile(ROOT/'runtime/app/web-new/api/servers/common.sh',api/'servers/common.sh')
        script=(ROOT/'runtime/app/web-new/api/servers/import.cgi').read_text().replace('/opt/broray',str(self.app))
        target=api/'servers/import.cgi';target.write_text(script)
        payload=json.dumps(dict(uri=uri,preview=True)).encode()
        before={p.name:p.read_bytes() for p in (self.app/'servers').glob('*.json')}
        scratch_before=set(Path('/tmp').glob('broray-import-preview.*'))
        r=subprocess.run(['/bin/ash',str(target)],input=payload,env=self.env|{'REQUEST_METHOD':'POST','CONTENT_LENGTH':str(len(payload))},capture_output=True,timeout=30)
        self.assertEqual(r.returncode,0,r.stderr)
        self.assertEqual(before,{p.name:p.read_bytes() for p in (self.app/'servers').glob('*.json')})
        self.assertEqual(scratch_before,set(Path('/tmp').glob('broray-import-preview.*')))
        return json.loads(r.stdout)

    def test_preview_uses_installed_parser_without_saving_credentials(self):
        result=self.preview('hysteria2://private%2Bsecret@example.invalid?sni=example.invalid')
        self.assertTrue(result['success'],result);self.assertEqual(result['data']['port'],443)
        self.assertRegex(result['data']['canonical'],r'^[0-9a-f]{64}$')
        self.assertNotIn('private',json.dumps(result))

    def test_preview_identity_preserves_distinct_credentials(self):
        first=self.preview('vless://11111111-2222-4333-8444-555555555555@example.invalid:443?security=tls')
        second=self.preview('vless://22222222-2222-4333-8444-555555555555@example.invalid:443?security=tls')
        self.assertTrue(first['success'],first);self.assertTrue(second['success'],second)
        self.assertNotEqual(first['data']['canonical'],second['data']['canonical'])

    def test_preview_rejects_malformed_percent_without_live_mutation(self):
        result=self.preview('vless://bad%ZZ@example.invalid:443?security=tls')
        self.assertFalse(result['success']);self.assertEqual(result['error']['code'],'SERVER_PREVIEW_FAILED')

if __name__=='__main__':unittest.main(verbosity=2)
