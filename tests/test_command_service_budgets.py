"""Approved command/start budgets: offline ash fixtures, no router/network.

Virtual /proc/uptime advances deterministically; wall-clock test timeouts stay
short. These tests prove budget/error behavior, not slow-router performance.
"""
import os, re, shutil, subprocess, tempfile, unittest
from pathlib import Path
import test_network_multilan as lan

ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))

class Budgets(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.app=Path(self.temp.name)
        for d in ['bin','lib','tmp','logs']:(self.app/d).mkdir()
        self.clock=self.app/'uptime';self.clock.write_text('0.00 0.00\n')
        self.env=os.environ|{'BRORAY_ROOT':str(self.app),'BRORAY_BASE':str(self.app),
            'CLOCK':str(self.clock),'PATH':str(self.app/'bin')+':/usr/bin:/bin'}
    def script(self,name,body):
        p=self.app/'bin'/name;p.write_text('#!/bin/ash\n'+body+'\n');p.chmod(0o755);return p
    def shell(self,code):
        return subprocess.run(['/bin/ash','-c',code],env=self.env,text=True,capture_output=True,timeout=20)
    def source(self,path):return (ROOT/path).read_text()
    def test_delayed_running_snapshot_keeps_valid_lan(self):
        f=lan.Lan();f.setUp()
        try:
            cmd=f.app/'bin/ndmc';cmd.write_text(cmd.read_text().replace('cat "$BRORAY_ROOT/running"','sleep 9\ncat "$BRORAY_ROOT/running"'))
            self.assertEqual(f.detect(),'192.168.2.1')
        finally:f.doCleanups()
    def test_network_read_dispatches_separate_budgets_and_preserves_failure(self):
        self.script('broray-timeout','printf "%s\\n" "$*" >>"$BRORAY_ROOT/limits"; exit 47')
        self.env['BRORAY_NETWORK_ROOT']=str(self.app)
        q=self.app/'lib/network.sh';q.write_text(self.source('runtime/app/lib/network.sh'))
        for args in ['"$BRORAY_ROOT/bin/broray-system-ndmc" -c "show running-config"','ip -4 addr show']:
            p=self.shell('. "$BRORAY_ROOT/lib/network.sh"; broray_network_read '+args)
            self.assertEqual(p.returncode,47)
        rows=(self.app/'limits').read_text().splitlines()
        self.assertIn('-k 2 120 ',rows[0]);self.assertEqual(rows[1],'-k 2 30 ip -4 addr show')
    def service(self,ready_at=180,step=60,probe_cost=0):
        self.env.update(READY_AT=str(ready_at),STEP=str(step),PROBE_COST=str(probe_cost))
        source=self.source('runtime/app/bin/broray-service').replace('/proc/uptime',str(self.clock))
        (self.app/'bin/broray-service').write_text(source)
        (self.app/'lib/service-lifecycle.sh').write_text('''
broray_service_setup() { SVC_NAME=test; SVC_APP="$BRORAY_ROOT"; SVC_ASH=/bin/ash; SVC_DAEMON="$BRORAY_ROOT/daemon"; SVC_LOG="$BRORAY_ROOT/logs/service"; }
broray_service_status_json() {
 read -r t unused <"$CLOCK"; t=${t%%.*}; t=$((t+PROBE_COST)); echo "$t.00 0.00" >"$CLOCK"
 ready=false; [ "$READY_AT" -ge 0 ] && [ "$t" -ge "$READY_AT" ] && ready=true
 printf '{"complete":true,"running":true,"ready":%s}\\n' "$ready"
}
''')
        self.script('sleep','read -r t unused <"$CLOCK"; echo "$(( ${t%%.*}+STEP )).00 0.00" >"$CLOCK"; echo sleep >>"$BRORAY_ROOT/waits"')
        return self.shell('/bin/ash "$BRORAY_ROOT/bin/broray-service" test start')
    def test_service_ready_at_180_second_boundary(self):
        p=self.service();self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual((self.app/'waits').read_text().splitlines(),['sleep']*3)
    def test_service_timeout_counts_probe_time(self):
        p=self.service(ready_at=-1,step=60,probe_cost=30)
        self.assertEqual(p.returncode,75,p.stderr)
        self.assertEqual((self.app/'waits').read_text().splitlines(),['sleep']*2)
    def test_service_immediate_ready_does_not_sleep(self):
        p=self.service(ready_at=0);self.assertEqual(p.returncode,0,p.stderr)
        self.assertFalse((self.app/'waits').exists())
    def xray(self,ready_at,selected=False):
        (self.app/'lib/xray-process.sh').write_text('')
        self.env['READY_AT']=str(ready_at)
        checks='''
broray_xray_is_running() { read -r t unused <"$CLOCK"; [ "$READY_AT" -ge 0 ] && [ "${t%%.*}" -ge "$READY_AT" ]; }
broray_xray_socks_address() { echo 127.0.0.1; }
broray_xray_socks_port() { echo 2080; }
broray_xray_socks_active() { return 0; }
'''
        self.script('sleep','read -r t unused <"$CLOCK"; echo "$(( ${t%%.*}+60 )).00 0.00" >"$CLOCK"; echo sleep >>"$BRORAY_ROOT/waits"')
        path='runtime/app/lib/xray-releases.sh' if selected else 'runtime/app/lib/xray-control.sh'
        source=self.source(path).replace('/proc/uptime',str(self.clock))
        (self.app/'lib/test.sh').write_text(source)
        (self.app/'lib/xray.sh').write_text(checks)
        call='broray_xray_selected_runtime_ready' if selected else 'broray_xray_wait_running'
        return self.shell('. "$BRORAY_ROOT/lib/test.sh"; '+checks+'\n'+call)
    def test_xray_running_boundary(self):
        p=self.xray(180);self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual(len((self.app/'waits').read_text().splitlines()),3)
    def test_xray_running_timeout(self):
        p=self.xray(-1);self.assertNotEqual(p.returncode,0,p.stderr)
        self.assertEqual(len((self.app/'waits').read_text().splitlines()),3)
    def test_xray_selected_boundary(self):
        p=self.xray(180,True);self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual(len((self.app/'waits').read_text().splitlines()),3)
    def test_xray_selected_timeout(self):
        p=self.xray(-1,True);self.assertNotEqual(p.returncode,0,p.stderr)
        self.assertEqual(len((self.app/'waits').read_text().splitlines()),3)
    def test_webui_probe_total_budget_and_success_boundary(self):
        source=self.source('runtime/app/lib/package-setup.sh')
        function=source[source.index('validate_webui()'):source.index('validate_runtime_contracts()')].replace('/proc/uptime',str(self.clock))
        script='''
BRORAY_SETUP_SKIP_SERVICES=0
broray_setup_web_lan_ip() { echo 192.168.2.1; }
fail() { exit 1; }
broray_setup_local_http_probe() {
 echo "$2" >>"$BRORAY_ROOT/limits"
 read -r t unused <"$CLOCK"; t=$(( ${t%%.*}+$2 )); echo "$t.00 0.00" >"$CLOCK"
 [ "$t" -ge 180 ]
}
sleep() { read -r t unused <"$CLOCK"; echo "$(( ${t%%.*}+1 )).00 0.00" >"$CLOCK"; }
'''+function+'\nvalidate_webui'
        p=self.shell(script);self.assertEqual(p.returncode,0,p.stderr)
        limits=[int(x) for x in (self.app/'limits').read_text().splitlines()]
        self.assertEqual(limits,[30,30,30,30,30,25])
    def test_native_start_deadlines_match_approved_contract(self):
        for path in ['native/broray-platform-launch.h','native/broray-updater-service-cycle.h']:
            text=self.source(path)
            self.assertRegex(text,r'until\s*=\s*wait_begin\s*\+\s*180000\b',path)

if __name__=='__main__':unittest.main()
