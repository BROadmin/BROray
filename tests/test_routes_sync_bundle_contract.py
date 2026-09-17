#!/usr/bin/env python3
"""Regression of the sync bundle/admission boundary, NOT full router integration.

Runs real functions extracted from routes-router-sync.sh and the unchanged jq
admission predicate/exit mapping extracted from the resource controller. Process
identity, native guard, coordinator, plan computation, cleanup and persistence
are explicit test doubles. No network, ndmc, router, service or production path
is used. In a checkout defaults to its root; the delivery package can supply
BRORAY_STAGE02_SOURCE_ROOT pointing to the documented source excerpts.

Python 3.9+, jq, and BusyBox ash or dash; no pip packages required.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

SOURCE = Path(os.environ.get("BRORAY_STAGE02_SOURCE_ROOT", Path(__file__).resolve().parents[1]))
LIB = SOURCE / "runtime/app/lib"
SHELL = os.environ.get("BRORAY_STAGE02_TEST_SHELL", shutil.which("busybox") or "/bin/dash")
SHELL_CMD = [SHELL, "ash"] if Path(SHELL).name == "busybox" else [SHELL]
FUNCTION_NAMES = (
    "broray_routes_sync_id_valid", "broray_routes_sync_lock_acquire",
    "broray_routes_sync_lock_release", "broray_routes_sync_plan",
)


def extract_one(text, pattern, description):
    matches = re.findall(pattern, text, flags=re.MULTILINE | re.DOTALL)
    if len(matches) != 1:
        raise ValueError("Expected one unchanged extraction boundary: " + description)
    return matches[0] + "\n"


def source_functions():
    sync = (LIB / "routes-router-sync.sh").read_text(encoding="utf-8")
    functions = "\n".join(extract_one(sync, r"^" + re.escape(name) + r"\(\)\n\{\n.*?^\}", name)
                            for name in FUNCTION_NAMES)
    control = (LIB / "routes-resource-control.sh").read_text(encoding="utf-8")
    policy = extract_one(control, r'^    if \[ "\$job" != null \]; then\n.*?^    fi$', "bundle policy")
    resource = (LIB / "routes-resource-lock.sh").read_text(encoding="utf-8")
    mapping = extract_one(resource, r'^    case "\$rc" in 0\) return 0 ;; 2\|75\) return 2 ;; \*\) return 1 ;; esac$', "exit mapping")
    return functions, policy, mapping


DOUBLES = r'''
broray_contract_map_rc()
{
    local rc
    rc="$1"
    . "$CONTRACT_MAPPING"
}
broray_route_resource_acquire()
{
    local rc job operation bundle
    printf 'acquire|%s|%s\n' "$2" "${3:-}" >>"$CONTRACT_EVENTS"
    rc=0
    (
        job="$CONTRACT_JOB"; operation="$2"; bundle="${3:-}"
        . "$CONTRACT_POLICY"
    ) || rc=$?
    printf 'policy_result|%s\n' "$rc" >>"$CONTRACT_EVENTS"
    if [ "$rc" = 0 ] && [ -n "$CONTRACT_ACQUIRE_RC" ]; then rc="$CONTRACT_ACQUIRE_RC"; fi
    broray_contract_map_rc "$rc" || return $?
    BRORAY_ROUTE_RESOURCE_TOKEN=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    printf '%s\n' "$BRORAY_ROUTE_RESOURCE_TOKEN" >"$CONTRACT_TOKEN_RECORD"
}
broray_route_resource_release()
{
    printf 'release|%s\n' "$2" >>"$CONTRACT_EVENTS"
    broray_contract_map_rc "$CONTRACT_RELEASE_RC" || return $?
    [ "$(cat "$CONTRACT_TOKEN_RECORD")" = "$2" ] || return 2
    rm "$CONTRACT_TOKEN_RECORD"
}
broray_routes_sync_ensure_global_registry()
{
    printf 'registry\n' >>"$CONTRACT_EVENTS"
    return "$CONTRACT_REGISTRY_RC"
}
broray_routes_sync_build_plan_core()
{
    printf 'plan_core|%s\n' "$1" >>"$CONTRACT_EVENTS"
    [ "$CONTRACT_PLAN_RC" = 0 ] || return "$CONTRACT_PLAN_RC"
    jq -n --arg bundle "$1" '{bundleId:$bundle,contractFixture:true}' >"$2"
}
broray_routes_sync_cleanup()
{
    printf 'cleanup_double\n' >>"$CONTRACT_EVENTS"
    broray_routes_sync_lock_release
}
broray_routes_sync_abort()
{
    # The production abort/rollback implementation is deliberately not run.
    printf 'abort_double\n' >>"$CONTRACT_EVENTS"
    printf '%s\n' "$*" >&2
    exit 1
}
'''


class SyncBundleContract(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if not shutil.which(SHELL) or not shutil.which("jq"):
            raise RuntimeError("Required shell and jq must be installed; no skipped PASS")
        cls.functions, cls.policy, cls.mapping = source_functions()

    def run_case(self, body, *, bundle="telegram", action="verify", job=True,
                 acquire_rc="", release_rc="0", registry_rc="0", plan_rc="0", marker=None):
        with tempfile.TemporaryDirectory(prefix="broray-stage02-contract-") as td:
            t = Path(td)
            (t / "routes/tmp").mkdir(parents=True)
            (t / "events").write_text("")
            (t / "policy.sh").write_text(self.policy, encoding="utf-8")
            (t / "mapping.sh").write_text(self.mapping, encoding="utf-8")
            if marker == "file":
                (t / "rollback-required").write_text("DO NOT CLEAR")
            elif marker == "dangling-symlink":
                (t / "rollback-required").symlink_to(t / "does-not-exist")
            # Do not inherit router credentials or BROray runtime paths.
            env = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"), "LC_ALL": "C.UTF-8", "HOME": td,
                   "BRORAY_SYNC_ROUTES": str(t / "routes"),
                   "BRORAY_SYNC_LOCK": str(t / "routes/locks/operation.lock"),
                   "BRORAY_SYNC_ROLLBACK_MARKER": str(t / "rollback-required"),
                   "BRORAY_SYNC_LOCK_HELD": "false", "BRORAY_SYNC_LOCK_TOKEN": "",
                   "CONTRACT_EVENTS": str(t / "events"), "CONTRACT_TOKEN_RECORD": str(t / "lease-double"),
                   "CONTRACT_POLICY": str(t / "policy.sh"), "CONTRACT_MAPPING": str(t / "mapping.sh"),
                   "CONTRACT_JOB": json.dumps({"bundleId": bundle, "action": action}) if job else "null",
                   "CONTRACT_ACQUIRE_RC": str(acquire_rc), "CONTRACT_RELEASE_RC": str(release_rc),
                   "CONTRACT_REGISTRY_RC": str(registry_rc), "CONTRACT_PLAN_RC": str(plan_rc),
                   "CASE_BUNDLE": bundle}
            p = subprocess.run(SHELL_CMD + ["-c", "set -u\n" + self.functions + DOUBLES + "\n" + body],
                               env=env, capture_output=True, text=True, encoding="utf-8", timeout=15)
            return {"returncode": p.returncode, "stdout": p.stdout, "stderr": p.stderr,
                    "events": (t / "events").read_text().splitlines(),
                    "markerPresent": os.path.lexists(t / "rollback-required"),
                    "fixtureLeasePresent": (t / "lease-double").exists()}

    def assert_ok(self, r):
        self.assertEqual(r["returncode"], 0, r)

    def assert_refused(self, r, text):
        self.assertEqual(r["returncode"], 1, r)
        self.assertIn(text, r["stderr"])
        self.assertFalse(any(e.startswith("plan_core|") for e in r["events"]), r)

    def test_ready_verify_plan_forwards_bundle(self):
        r = self.run_case('broray_routes_sync_plan "$CASE_BUNDLE"')
        self.assert_ok(r)
        self.assertIn("acquire|sync|telegram", r["events"])
        self.assertEqual(json.loads(r["stdout"])["bundleId"], "telegram")
        self.assertFalse(r["fixtureLeasePresent"])

    def test_custom_export_preflight_plan_forwards_bundle(self):
        name = "user-d5d259cc84688805"
        r = self.run_case('broray_routes_sync_plan "$CASE_BUNDLE"', bundle=name, action="preflight:export")
        self.assert_ok(r)
        self.assertIn("acquire|sync|" + name, r["events"])
        self.assertEqual(json.loads(r["stdout"])["bundleId"], name)

    def test_direct_plan_context_forwards_bundle(self):
        r = self.run_case('broray_routes_sync_plan "$CASE_BUNDLE"', action="plan")
        self.assert_ok(r)

    def test_resume_preflight_context_forwards_bundle(self):
        r = self.run_case('broray_routes_sync_plan "$CASE_BUNDLE"', action="preflight:resume")
        self.assert_ok(r)

    def test_apply_lock_boundary_forwards_current_bundle(self):
        r = self.run_case('BRORAY_SYNC_BUNDLE="$CASE_BUNDLE"\nbroray_routes_sync_lock_acquire', action="export")
        self.assert_ok(r)
        self.assertEqual(r["events"][0], "acquire|sync|telegram")

    def test_resume_lock_boundary_forwards_current_bundle(self):
        r = self.run_case('BRORAY_SYNC_BUNDLE="$CASE_BUNDLE"\nbroray_routes_sync_lock_acquire', action="resume")
        self.assert_ok(r)
        self.assertEqual(r["events"][0], "acquire|sync|telegram")

    def test_wrong_bundle_remains_rejected(self):
        r = self.run_case('BRORAY_SYNC_BUNDLE=another-bundle\nbroray_routes_sync_lock_acquire')
        self.assertEqual(r["returncode"], 1, r)
        self.assertIn("policy_result|73", r["events"])
        self.assertFalse(r["fixtureLeasePresent"])

    def test_missing_bundle_in_protected_context_remains_rejected(self):
        r = self.run_case('unset BRORAY_SYNC_BUNDLE\nbroray_routes_sync_lock_acquire')
        self.assertEqual(r["returncode"], 1, r)
        self.assertIn("policy_result|73", r["events"])

    def test_standalone_no_bundle_compatibility(self):
        r = self.run_case('unset BRORAY_SYNC_BUNDLE\nbroray_routes_sync_lock_acquire || exit $?\nbroray_routes_sync_lock_release', job=False)
        self.assert_ok(r)
        self.assertIn("acquire|sync|", r["events"])
        self.assertFalse(r["fixtureLeasePresent"])

    def test_success_binds_and_clears_local_token(self):
        r = self.run_case('''BRORAY_SYNC_BUNDLE="$CASE_BUNDLE"
broray_routes_sync_lock_acquire || exit $?
[ "$BRORAY_SYNC_LOCK_HELD" = true ] || exit 95
[ "$BRORAY_SYNC_LOCK_TOKEN" = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ] || exit 95
broray_routes_sync_lock_release || exit $?
[ "$BRORAY_SYNC_LOCK_HELD" = false ] || exit 95
[ -z "$BRORAY_SYNC_LOCK_TOKEN" ] || exit 95''')
        self.assert_ok(r)

    def test_failed_release_preserves_local_owner_state(self):
        r = self.run_case('''BRORAY_SYNC_BUNDLE="$CASE_BUNDLE"
broray_routes_sync_lock_acquire || exit $?
rc=0; broray_routes_sync_lock_release || rc=$?
[ "$rc" = 2 ] || exit 95
[ "$BRORAY_SYNC_LOCK_HELD" = true ] || exit 95
[ -n "$BRORAY_SYNC_LOCK_TOKEN" ] || exit 95''', release_rc="75")
        self.assert_ok(r)
        self.assertTrue(r["fixtureLeasePresent"])

    def test_acquire_contention_return_two_stays_busy(self):
        r = self.run_case('broray_routes_sync_plan "$CASE_BUNDLE"', acquire_rc="2")
        self.assert_refused(r, "Другая операция с маршрутами уже выполняется.")
        self.assertFalse(r["fixtureLeasePresent"])

    def test_acquire_guard_timeout_stays_busy(self):
        r = self.run_case('broray_routes_sync_plan "$CASE_BUNDLE"', acquire_rc="75")
        self.assert_refused(r, "Другая операция с маршрутами уже выполняется.")

    def test_acquire_membership_rejection_stays_error(self):
        r = self.run_case('broray_routes_sync_plan "$CASE_BUNDLE"', acquire_rc="73")
        self.assert_refused(r, "Не удалось установить блокировку операции.")

    def test_acquire_storage_failure_stays_error(self):
        r = self.run_case('broray_routes_sync_plan "$CASE_BUNDLE"', acquire_rc="74")
        self.assert_refused(r, "Не удалось установить блокировку операции.")

    def test_acquire_failure_does_not_set_held_or_token(self):
        r = self.run_case('''BRORAY_SYNC_BUNDLE="$CASE_BUNDLE"
rc=0; broray_routes_sync_lock_acquire || rc=$?
[ "$rc" = 2 ] || exit 95
[ "$BRORAY_SYNC_LOCK_HELD" = false ] || exit 95
[ -z "$BRORAY_SYNC_LOCK_TOKEN" ] || exit 95''', acquire_rc="2")
        self.assert_ok(r)

    def test_stale_bundle_variable_is_replaced_by_plan_argument(self):
        r = self.run_case('BRORAY_SYNC_BUNDLE=old\nbroray_routes_sync_plan "$CASE_BUNDLE"')
        self.assert_ok(r)
        self.assertIn("acquire|sync|telegram", r["events"])

    def test_sequential_plans_use_their_own_bundle(self):
        r = self.run_case('broray_routes_sync_plan telegram || exit $?\nbroray_routes_sync_plan user-second', job=False)
        self.assert_ok(r)
        self.assertEqual([e for e in r["events"] if e.startswith("acquire|")],
                         ["acquire|sync|telegram", "acquire|sync|user-second"])
        self.assertFalse(r["fixtureLeasePresent"])

    def test_rollback_marker_still_blocks_plan(self):
        r = self.run_case('broray_routes_sync_plan "$CASE_BUNDLE"', marker="file")
        self.assert_refused(r, "Предыдущий откат маршрутов не подтверждён")
        self.assertTrue(r["markerPresent"])

    def test_dangling_rollback_marker_still_blocks_plan(self):
        r = self.run_case('broray_routes_sync_plan "$CASE_BUNDLE"', marker="dangling-symlink")
        self.assert_refused(r, "Предыдущий откат маршрутов не подтверждён")
        self.assertTrue(r["markerPresent"])

    def test_invalid_bundle_refused_before_acquire(self):
        r = self.run_case('broray_routes_sync_plan ../escape')
        self.assert_refused(r, "Некорректный идентификатор набора.")
        self.assertFalse(any(e.startswith("acquire|") for e in r["events"]))

    def test_missing_bundle_refused_before_acquire(self):
        r = self.run_case('broray_routes_sync_plan')
        self.assert_refused(r, "Некорректный идентификатор набора.")
        self.assertFalse(any(e.startswith("acquire|") for e in r["events"]))

    def test_long_bundle_refused_before_acquire(self):
        r = self.run_case('broray_routes_sync_plan "$CASE_BUNDLE"', bundle="a" * 64)
        self.assert_refused(r, "Некорректный идентификатор набора.")
        self.assertFalse(any(e.startswith("acquire|") for e in r["events"]))

    def test_63_character_bundle_allowed_by_existing_validation(self):
        r = self.run_case('broray_routes_sync_plan "$CASE_BUNDLE"', bundle="a" * 63)
        self.assert_ok(r)

    def test_registry_failure_is_not_successful_plan(self):
        r = self.run_case('broray_routes_sync_plan "$CASE_BUNDLE"', registry_rc="1")
        self.assert_refused(r, "ROUTES_PLAN_REGISTRY_INVALID")

    def test_plan_core_failure_is_not_successful_plan(self):
        r = self.run_case('broray_routes_sync_plan "$CASE_BUNDLE"', plan_rc="1")
        self.assertEqual(r["returncode"], 1, r)
        self.assertIn("Не удалось построить безопасный план установки.", r["stderr"])
        self.assertEqual(r["stdout"], "")


if __name__ == "__main__":
    unittest.main(verbosity=2)
