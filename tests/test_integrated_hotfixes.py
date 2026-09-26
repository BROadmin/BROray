import unittest,subprocess,os,re
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
class IntegratedHotfixes(unittest.TestCase):
 def test_changed_shell_syntax(self):
  names=['runtime/app/lib/operation-coordinator.sh', 'runtime/app/lib/operation-platform-generation.sh', 'runtime/app/lib/operations-origin.sh', 'runtime/app/web-new/api/broray/updater-api-common.sh', 'runtime/app/web-new/api/servers/summary.cgi']
  for n in names:
   with self.subTest(path=n):
    r=subprocess.run(['/bin/ash','-n',str(ROOT/n)],capture_output=True,text=True)
    self.assertEqual(r.returncode,0,r.stderr)
 def test_preferred_failover_selection_and_cancellation(self):
  s=(ROOT/'runtime/app/bin/broray-server-auto-switch').read_text()
  functions=''
  for name,nextname in [('rating_rank','load_config'),('build_candidates','activate_candidate')]:
   functions+=s[s.index(name+'()\n'):s.index(nextname+'()\n')]
  r=subprocess.run(['/bin/ash','-s'],input='set -u\nexport PATH=/opt/bin:/opt/sbin:/usr/bin:/bin:/sbin\numask 077\nROOT="$(mktemp -d /tmp/broray-audit-f10.XXXXXX)" || exit 70\ncase "$ROOT" in /tmp/broray-audit-f10.*) ;; *) exit 70;; esac\ntrap \'rm -rf "$ROOT"\' EXIT\nmkdir "$ROOT/tmp"\nTEST_LOG="$ROOT/checks"\nCFG_MINIMUM=good\nCFG_RULE=preferred\nCFG_PREFERRED=preferred\nMODE=healthy\nbroray_server_check() {\n printf \'%s\\n\' "$1" >> "$TEST_LOG"\n if [ "$MODE" = cancelled ]; then return 130; fi\n if [ "$1" = preferred ] && [ "$MODE" = unavailable ]; then printf \'{"success":false}\\n\';return 1;fi\n quality=excellent\n if [ "$1" = preferred ] && [ "$MODE" = poor ]; then quality=poor;fi\n printf \'{"success":true,"quality":{"rating":"%s","ping":10,"jitter":1}}\\n\' "$quality"\n}\nprintf \'{"servers":[{"id":"other-a"},{"id":"other-b"},{"id":"preferred"},{"id":"current"}]}\\n\' > "$ROOT/summary.json"\n'+functions+'\nset -e\npass=0\nfor spec in \'preferred healthy preferred preferred preferred\' \'preferred unavailable preferred other-a preferred,other-a,other-b\' \'preferred poor preferred other-a preferred,other-a,other-b\' \'best-quality healthy preferred other-a other-a,other-b,preferred\' \'lowest-ping healthy preferred other-a other-a,other-b,preferred\' \'preferred healthy current other-a other-a,other-b,preferred\' \'preferred healthy missing other-a other-a,other-b,preferred\';do\n set -- $spec;CFG_RULE="$1";MODE="$2";CFG_PREFERRED="$3";expected="$4";order="$5";: > "$TEST_LOG"\n build_candidates "$ROOT/summary.json" current "$ROOT/result.json"\n select_candidate "$ROOT/result.json"\n chosen="$(printf \'%s\' "$SELECTED_JSON" | jq -r .id)"\n [ "$chosen" = "$expected" ] || { echo "WRONG_CHOICE $spec: $chosen";exit 1; }\n actual="$(tr \'\\n\' \',\' < "$TEST_LOG" | sed \'s/,$//\')"\n [ "$actual" = "$order" ] || { echo "WRONG_ORDER $spec: $actual";exit 1; }\n pass=$((pass+1));echo "PASS case $pass $spec"\ndone\nMODE=cancelled;CFG_RULE=preferred;CFG_PREFERRED=preferred;: > "$TEST_LOG";rc=0\nbuild_candidates "$ROOT/summary.json" current "$ROOT/result.json" || rc=$?\n[ "$rc" = 130 ] && [ "$(cat "$TEST_LOG")" = preferred ] || exit 1\npass=$((pass+1));echo "PASS case $pass cancellation propagates without a second candidate"\nprintf \'TESTS_TOTAL=%s TESTS_PASS=%s\\n\' "$pass" "$pass"\n',capture_output=True,text=True,timeout=30)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertIn('TESTS_TOTAL=8 TESTS_PASS=8',r.stdout)
if __name__=='__main__':unittest.main()
