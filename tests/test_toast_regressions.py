"""Run prior browser suites with the patched shared component; all APIs are fixtures."""
from pathlib import Path
import hashlib,json,os,shutil,subprocess,tempfile
ROOT=Path(os.environ.get('BRORAY_STAGE_ROOT',Path(__file__).resolve().parents[2]))
PREPARED=Path(os.environ.get('BRORAY_TEST_ROOT',ROOT/'files'))
BASELINE=Path(os.environ.get('BRORAY_BASELINE_ROOT',ROOT/'baseline'))
EVIDENCE=Path(os.environ.get('STAGE12_EVIDENCE',ROOT/'evidence/regressions'))
EVIDENCE.mkdir(parents=True,exist_ok=True)
SUITES={'test_stage04_browser.mjs':11,'test_dot_auto_browser.mjs':10,'test_subscription_metadata_browser.mjs':7,'test_subscription_device_browser.mjs':9}
def source(rel):
 for root in [PREPARED,BASELINE]:
  p=root/rel
  if p.is_file():return p
 raise FileNotFoundError(rel)
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
results=[]
with tempfile.TemporaryDirectory(prefix='broray-toast-regression-') as folder:
 fixture=Path(folder)
 # Only static browser assets are copied. No runtime service or shell is executed.
 for root in [BASELINE,PREPARED]:
  web=root/'runtime/app/web-new'
  if web.exists():shutil.copytree(web,fixture/'files/runtime/app/web-new',dirs_exist_ok=True)
 shutil.copytree(fixture/'files/runtime/app/web-new',fixture/'baseline/runtime/app/web-new')
 (fixture/'tests').mkdir();(fixture/'evidence').mkdir()
 for name,expected in SUITES.items():
  src=source('tests/'+name);test=fixture/'tests'/name;test.write_bytes(src.read_bytes())
  env={**os.environ,'BRORAY_STAGE_ROOT':str(fixture)}
  p=subprocess.run(['node',str(test)],env=env,capture_output=True,timeout=120)
  target=EVIDENCE/name.removesuffix('.mjs');target.mkdir(exist_ok=True)
  (target/'runner.log').write_bytes(p.stdout+p.stderr)
  for f in (fixture/'evidence').iterdir():
   if f.is_file():shutil.copyfile(f,target/f.name);f.unlink()
  summaries=[]
  for line in p.stdout.decode('utf-8',errors='replace').splitlines():
   try:value=json.loads(line)
   except ValueError:continue
   if isinstance(value,dict) and 'passed' in value:summaries.append(value)
  good=p.returncode==0 and bool(summaries) and summaries[-1]['passed']==expected and summaries[-1].get('failed',0)==0
  results.append({'suite':name,'sourceSha256':sha(src),'returncode':p.returncode,'passed':summaries[-1]['passed'] if summaries else None,'expected':expected,'status':'PASS' if good else 'FAIL'})
  (EVIDENCE/'PROGRESS.json').write_text(json.dumps(results,indent=2),encoding='utf-8')
  if not good:raise RuntimeError('Regression failed: '+name+'; see '+str(target/'runner.log'))
report={'status':'PASS','suites':results,'totalPassed':sum(s['passed'] for s in results),'componentSha256':{n:sha(source(n)) for n in ['runtime/app/web-new/assets/js/common.js','runtime/app/web-new/assets/css/allpage.css']},'routerAccessed':False,'http':'intercepted synthetic API','nativeShell':False,'note':'DNS suites load actual scripts. Subscription suites substitute the common BROrayUI adapter; they check CSS/forms, not shared toast execution.'}
(EVIDENCE/'REGRESSION-RESULT.json').write_bytes((json.dumps(report,indent=2)+'\n').encode())
print(json.dumps(report))
