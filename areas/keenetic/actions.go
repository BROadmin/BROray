package keenetic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	kn "github.com/BROadmin/broray/internal/keenetic"
)

// Timing of lib/interface-manage.sh (variables for tests).
var (
	settleDelay         = 5 * time.Second // before every "system configuration save"
	convergenceAttempts = 10
	convergenceDelay    = time.Second
	postActionDelay     = time.Second // keenetic-page.sh: sleep 1 before the final status
)

// actionError carries the 3.x stderr/stdout lines of a failed action.
type actionError struct {
	expectedUnavailable bool // rc 2 → 409 KEENETIC_EXPECTED_STATE_UNAVAILABLE
	lines               []string
	stage               string // the failed stage of run()
	unsupported         bool   // KeeneticOS answered "unknown command" (udpcap.go)
}

func (e *actionError) Error() string { return strings.Join(e.lines, "\n") }

func failLines(lines ...string) error { return &actionError{lines: lines} }

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ---------- staged commands (interface-core.sh:67-179) ----------

// stageAllowed is broray_interface_stage_command_allowed.
func stageAllowed(name, stage, c string) bool {
	switch strings.TrimPrefix(strings.TrimPrefix(stage, "rollback-"), "cleanup-") {
	case "create-interface":
		return c == "interface "+name
	case "protocol":
		return c == "interface "+name+" proxy protocol socks5"
	case "socks5-udp":
		if stage == "rollback-socks5-udp" && c == "interface "+name+" no proxy socks5-udp" {
			return true
		}
		return c == "interface "+name+" proxy socks5-udp"
	case "upstream":
		f := strings.Fields(c)
		if len(f) != 6 || f[0] != "interface" || f[1] != name || f[2] != "proxy" || f[3] != "upstream" || !validIPv4(f[4]) {
			return false
		}
		p, err := strconv.Atoi(f[5])
		return err == nil && validPort(p) && c == "interface "+name+" proxy upstream "+f[4]+" "+f[5]
	case "connect":
		return c == "interface "+name+" proxy connect"
	case "description":
		if stage == "rollback-description" && (c == "interface "+name+" no description" || c == "interface "+name+" description null") {
			return true
		}
		prefix := "interface " + name + " description "
		if !strings.HasPrefix(c, prefix) {
			return false
		}
		d := strings.TrimPrefix(c, prefix)
		if d == "BROray" {
			return true
		}
		if !strings.HasPrefix(d, `"BROray - `) || !strings.HasSuffix(d, `"`) {
			return false
		}
		return validDescription(d[1 : len(d)-1])
	case "security-level":
		return c == "interface "+name+" security-level public"
	case "admin-up":
		return c == "interface "+name+" up"
	case "save":
		return c == "system configuration save"
	case "delete-interface", "delete":
		return c == "no interface "+name
	}
	return false
}

func descriptionCommand(name, d string) (string, bool) {
	if !validDescription(d) {
		return "", false
	}
	if d == "BROray" {
		return "interface " + name + " description BROray", true
	}
	return "interface " + name + ` description "` + d + `"`, true
}

type stageCmd struct{ stage, cmd string }

// run dispatches staged commands in one RCI batch after the stage grammar
// and the shared allowlist; every command records evidence
// (run/interface-last-command.json); a failure is copied to
// run/interface-last-failure.json.
func (s *service) run(ctx context.Context, name string, cmds ...stageCmd) error {
	list := make([]string, 0, len(cmds))
	for _, c := range cmds {
		area := kn.AreaInterface
		if c.cmd == "system configuration save" {
			area = kn.AreaSystem
		}
		if !stageAllowed(name, c.stage, c.cmd) || kn.Allow(area, c.cmd) != nil {
			return failLines("BRORAY_PROXY_ERROR:PROXY_COMMAND_NOT_AUTHORIZED:" + c.stage)
		}
		list = append(list, c.cmd)
	}
	res, err := s.d.App.T.Parse(ctx, list)
	for i, c := range cmds {
		rc, stdout, stderr := 0, "", ""
		switch {
		case err != nil:
			rc, stderr = 1, err.Error()
		case i >= len(res):
			rc, stderr = 1, "no result"
		default:
			var msgs []string
			for _, st := range res[i].Status {
				msgs = append(msgs, st.Message)
				if st.Status == "error" {
					stderr = st.Message
				}
			}
			stdout = strings.Join(msgs, "\n")
			if !res[i].OK {
				rc = 1
				if stderr == "" {
					stderr = "rejected"
				}
			}
		}
		ev := map[string]any{"schemaVersion": 1, "contract": "r14c01-proxy-ndmc-stage-evidence/1", "stage": c.stage, "command": c.cmd,
			"rc": rc, "stdout": stdout, "stderr": stderr, "observedAt": compat.Now()}
		_ = compat.WriteJSON(s.d.RunPath("interface-last-command.json"), ev)
		if rc != 0 {
			_ = compat.WriteJSON(s.d.RunPath("interface-last-failure.json"), ev)
			lines := []string{fmt.Sprintf("BRORAY_PROXY_STAGE_ERROR:stage=%s rc=%d", c.stage, rc)}
			if stdout != "" {
				lines = append(lines, "--- ndmc stdout ---", stdout)
			}
			if stderr != "" {
				lines = append(lines, "--- ndmc stderr ---", stderr)
			}
			ae := &actionError{lines: lines, stage: c.stage}
			if err == nil && i < len(res) {
				ae.unsupported = unsupportedCommand(res[i].Status)
			}
			return ae
		}
	}
	return nil
}

// save is broray_interface_save: 5 s settle, then "system configuration save".
func (s *service) save(ctx context.Context, name, stage string) error {
	if err := sleepCtx(ctx, settleDelay); err != nil {
		return err
	}
	return s.run(ctx, name, stageCmd{stage, "system configuration save"})
}

// runtimeReady is broray_interface_runtime_ready (show interface).
func (s *service) runtimeReady(ctx context.Context, name, desc string) bool {
	lines, err := s.showText(ctx, "show interface "+name)
	if err != nil {
		return false
	}
	kv := keyValues(lines)
	return kv["type"] == "Proxy" && kv["link"] == "up" && (desc == "" || kv["description"] == desc) && kv["connected"] == "yes" && kv["state"] == "up"
}

// UDP requirement of waitPresent.
const (
	udpAny = iota
	udpOff
	udpOn
)

// waitPresent is broray_interface_wait_present (10 × 1 s); 3.5.0 also
// checks the SOCKS5 UDP mode in running and startup configuration.
func (s *service) waitPresent(ctx context.Context, name, host string, port int, desc string, udp int) bool {
	udpOK := func(sn snapshots) bool {
		r, st := snapshotUDP(sn.running, name), snapshotUDP(sn.startup, name)
		switch udp {
		case udpOn:
			return r && st
		case udpOff:
			return !r && !st
		}
		return r == st
	}
	for attempt := 1; attempt <= convergenceAttempts; attempt++ {
		sn := s.capture(ctx)
		if sn.err == nil && sourceExact(sn.running, name, host, port, desc, "") && sourceExact(sn.startup, name, host, port, desc, "") &&
			udpOK(sn) && s.runtimeReady(ctx, name, desc) {
			return true
		}
		if attempt < convergenceAttempts && sleepCtx(ctx, convergenceDelay) != nil {
			return false
		}
	}
	return false
}

func (s *service) existsName(ctx context.Context, name string) (bool, error) {
	lines, err := s.captureConfig(ctx, "running")
	if err != nil {
		return false, err
	}
	return declarationCount(lines, name) > 0, nil
}

// ---------- create / delete (interface-manage.sh) ----------

// createUnchecked is broray_interface_create_unchecked.
func (s *service) createUnchecked(ctx context.Context, name, host string, port int, desc string) error {
	if ex, err := s.existsName(ctx, name); err != nil || ex {
		return failLines("ОШИБКА: интерфейс " + name + " уже существует")
	}
	dc, ok := descriptionCommand(name, desc)
	if !ok {
		return failLines("ОШИБКА: описание не установлено")
	}
	if err := s.run(ctx, name, stageCmd{"create-interface", "interface " + name}); err != nil {
		return failLines("ОШИБКА: интерфейс не создан")
	}
	if ex, _ := s.existsName(ctx, name); !ex {
		return failLines("ОШИБКА: интерфейс отсутствует после создания")
	}
	if err := s.run(ctx, name, stageCmd{"protocol", "interface " + name + " proxy protocol socks5"}); err != nil {
		return err
	}
	// SOCKS5 UDP: KeeneticOS requires the protocol first. Its own batch, so
	// a firmware without it (before 4.1) can be told from a real failure.
	want := udpOn
	if s.udpUnsupported(ctx) {
		want = udpOff
	} else if err := s.run(ctx, name, stageCmd{"socks5-udp", "interface " + name + " proxy socks5-udp"}); err != nil {
		if !udpUnsupportedErr(err) {
			return err
		}
		s.recordUDP(ctx, false)
		want = udpOff
	} else {
		s.recordUDP(ctx, true)
	}
	if err := s.run(ctx, name,
		stageCmd{"upstream", "interface " + name + " proxy upstream " + host + " " + strconv.Itoa(port)},
		stageCmd{"connect", "interface " + name + " proxy connect"},
		stageCmd{"description", dc},
		stageCmd{"security-level", "interface " + name + " security-level public"},
		stageCmd{"admin-up", "interface " + name + " up"}); err != nil {
		return err
	}
	if err := s.save(ctx, name, "save"); err != nil {
		return failLines("ОШИБКА: конфигурация KeeneticOS не сохранена")
	}
	if ex, _ := s.existsName(ctx, name); !ex {
		return failLines("ОШИБКА: интерфейс отсутствует после сохранения")
	}
	if !s.waitPresent(ctx, name, host, port, desc, want) {
		return failLines("BRORAY_PROXY_ERROR:PROXY_CREATE_CONVERGENCE_TIMEOUT:running/startup/runtime не сошлись после сохранения")
	}
	return nil
}

// deleteUnchecked is broray_interface_delete_unchecked.
func (s *service) deleteUnchecked(ctx context.Context, name string) (string, error) {
	ex, err := s.existsName(ctx, name)
	if err != nil {
		return "", failLines("ОШИБКА: running-config недоступен")
	}
	if !ex {
		return "Интерфейс " + name + " уже отсутствует", nil
	}
	if err := s.run(ctx, name, stageCmd{"delete-interface", "no interface " + name}); err != nil {
		return "", failLines("ОШИБКА: KeeneticOS отклонила удаление")
	}
	if err := s.save(ctx, name, "save"); err != nil {
		return "", failLines("ОШИБКА: конфигурация KeeneticOS не сохранена")
	}
	if ex, _ := s.existsName(ctx, name); ex {
		return "", failLines("ОШИБКА: интерфейс остался после удаления")
	}
	return "Интерфейс " + name + " удалён", nil
}

func (s *service) create(ctx context.Context, host string, port int, desc string) error {
	name, err := s.selectForAction(ctx, host, port)
	if err != nil {
		return failLines("BRORAY_PROXY_ERROR:PROXY_SAFE_SELECTION_FAILED:не удалось доказать owned или свободный ProxyN")
	}
	if ex, err := s.existsName(ctx, name); err != nil || ex {
		sn := s.capture(ctx)
		if _, owned := s.ownerValid(sn, name); owned {
			return failLines("ОШИБКА: интерфейс "+name+" уже существует", "Используйте repair")
		}
		return failLines("ОШИБКА: интерфейс "+name+" занят и не принадлежит BROray", "Чужой прокси-интерфейс оставлен без изменений")
	}
	if err := s.createUnchecked(ctx, name, host, port, desc); err != nil {
		if ex, _ := s.existsName(ctx, name); ex {
			_, _ = s.deleteUnchecked(ctx, name)
		}
		return err
	}
	if err := s.ownerWrite(ctx, name, "created", host, port, desc); err != nil {
		_, _ = s.deleteUnchecked(ctx, name)
		return failLines("ОШИБКА: не удалось сохранить локальную запись интерфейса BROray")
	}
	if err := s.syncRoutePolicy(name); err != nil {
		return failLines("ОШИБКА: " + err.Error())
	}
	return nil
}

// ---------- repair ----------

// restoreOwned is broray_interface_restore_owned. udpTouched=false: the
// failed change never enabled SOCKS5 UDP, so a TCP-only interface gets no
// "no proxy socks5-udp" (unknown to KeeneticOS before 4.1).
func (s *service) restoreOwned(ctx context.Context, name, host string, port int, desc string, udp, udpTouched bool) bool {
	dc, ok := descriptionCommand(name, desc)
	if !ok {
		return false
	}
	ex, err := s.existsName(ctx, name)
	if err != nil {
		return false
	}
	if !ex && s.run(ctx, name, stageCmd{"rollback-create-interface", "interface " + name}) != nil {
		return false
	}
	udpCmd := stageCmd{"rollback-socks5-udp", "interface " + name + " no proxy socks5-udp"}
	want := udpOff
	if udp {
		udpCmd, want = stageCmd{"rollback-socks5-udp", "interface " + name + " proxy socks5-udp"}, udpOn
	}
	cmds := []stageCmd{{"rollback-protocol", "interface " + name + " proxy protocol socks5"}}
	if udp || udpTouched {
		cmds = append(cmds, udpCmd)
	}
	cmds = append(cmds,
		stageCmd{"rollback-upstream", "interface " + name + " proxy upstream " + host + " " + strconv.Itoa(port)},
		stageCmd{"rollback-connect", "interface " + name + " proxy connect"},
		stageCmd{"rollback-description", dc},
		stageCmd{"rollback-security-level", "interface " + name + " security-level public"},
		stageCmd{"rollback-admin-up", "interface " + name + " up"})
	if s.run(ctx, name, cmds...) != nil {
		return false
	}
	if s.save(ctx, name, "rollback-save") != nil {
		return false
	}
	return s.waitPresent(ctx, name, host, port, desc, want)
}

// migrateLegacy is broray_interface_migrate_legacy_owner (schema-1 receipts).
func (s *service) migrateLegacy(ctx context.Context, name string, r receipt, desired string) error {
	dc, ok := descriptionCommand(name, desired)
	if !ok {
		return failLines("ОШИБКА: описание не установлено")
	}
	sn := s.capture(ctx)
	block, _ := extractBlock(sn.running, name)
	oldNull := len(block) > 1 && block[1] == "    description null"
	failure := ""
	if s.run(ctx, name, stageCmd{"description", dc}) != nil {
		failure = "description"
	} else if s.run(ctx, name, stageCmd{"connect", "interface " + name + " proxy connect"}) != nil {
		failure = "connect"
	} else if s.run(ctx, name, stageCmd{"admin-up", "interface " + name + " up"}) != nil {
		failure = "admin-up"
	} else if s.save(ctx, name, "save") != nil {
		failure = "save"
	} else if !s.waitPresent(ctx, name, r.Upstream.Host, r.Upstream.Port, desired, udpAny) {
		failure = "convergence-timeout"
	}
	if failure != "" {
		rb := "interface " + name + " no description"
		if oldNull {
			rb = "interface " + name + " description null"
		}
		_ = s.run(ctx, name, stageCmd{"rollback-description", rb})
		_ = s.save(ctx, name, "rollback-save")
		if legacyMigratable(s.capture(ctx), name, r.Upstream.Host, r.Upstream.Port) {
			return failLines("BRORAY_PROXY_LEGACY_MIGRATION_ROLLBACK=PASS failedStage=" + failure)
		}
		return failLines("BRORAY_PROXY_ERROR:PROXY_RECOVERY_REQUIRED:legacy owner migration rollback не доказан failedStage=" + failure)
	}
	return s.ownerWrite(ctx, name, "legacy-schema1-migrated", r.Upstream.Host, r.Upstream.Port, desired)
}

// repairOwned is broray_interface_repair_unchecked for an existing owned
// interface: move it to the current SOCKS endpoint and expected
// description, with byte-exact rollback.
func (s *service) repairOwned(ctx context.Context, name, host string, port int, desired string) error {
	sn := s.capture(ctx)
	r, owned := s.ownerValid(sn, name)
	if !owned {
		return failLines("BRORAY_PROXY_ERROR:PROXY_DELETE_AUTHORITY_REFUSED:" + name + " не имеет полного policy-bound receipt/live/startup equality")
	}
	if r.SchemaVersion == 1 {
		if err := s.migrateLegacy(ctx, name, r, desired); err != nil {
			return err
		}
		r, _ = s.loadReceipt()
	}
	oldHost, oldPort, oldDesc := r.Upstream.Host, r.Upstream.Port, r.Description
	sn = s.capture(ctx)
	oldUDP := snapshotUDP(sn.running, name) && snapshotUDP(sn.startup, name)
	noUDP := !oldUDP && s.udpUnsupported(ctx) // firmware without SOCKS5 UDP: stay TCP-only
	if oldHost == host && oldPort == port && oldDesc == desired && (oldUDP || noUDP) && s.runtimeReady(ctx, name, desired) {
		return nil // "Исправление не требуется"
	}
	dc, ok := descriptionCommand(name, desired)
	if !ok {
		return failLines("ОШИБКА: описание не установлено")
	}
	changed, failure, udpTouched := false, "", false
	want := udpOn
	if oldHost != host || oldPort != port {
		if s.run(ctx, name, stageCmd{"upstream", "interface " + name + " proxy upstream " + host + " " + strconv.Itoa(port)}) != nil {
			failure = "upstream"
		} else {
			changed = true
		}
	}
	if failure == "" && oldDesc != desired {
		if s.run(ctx, name, stageCmd{"description", dc}) != nil {
			failure = "description"
		} else {
			changed = true
		}
	}
	// 3.5.0: interfaces created by 3.x / dev1–dev3 are TCP-only; enable
	// SOCKS5 UDP in place (no delete/recreate).
	if failure == "" && noUDP {
		want = udpOff
	} else if failure == "" && !oldUDP {
		if err := s.run(ctx, name, stageCmd{"socks5-udp", "interface " + name + " proxy socks5-udp"}); err == nil {
			changed, udpTouched = true, true
			s.recordUDP(ctx, true)
		} else if udpUnsupportedErr(err) {
			s.recordUDP(ctx, false) // nothing was enabled: continue TCP-only
			want = udpOff
		} else {
			failure, udpTouched = "socks5-udp", true
		}
	}
	if failure == "" && changed && s.save(ctx, name, "save") != nil {
		failure = "save"
	}
	if failure == "" && !s.waitPresent(ctx, name, host, port, desired, want) {
		failure = "convergence-timeout"
	}
	if failure != "" {
		if s.restoreOwned(ctx, name, oldHost, oldPort, oldDesc, oldUDP, udpTouched) {
			return failLines("BRORAY_PROXY_REPAIR_ROLLBACK=PASS failedStage=" + failure)
		}
		return failLines("BRORAY_PROXY_ERROR:PROXY_RECOVERY_REQUIRED:repair rollback не доказан failedStage=" + failure)
	}
	if err := s.ownerWrite(ctx, name, "repaired", host, port, desired); err != nil {
		_ = s.restoreOwned(ctx, name, oldHost, oldPort, oldDesc, oldUDP, udpTouched)
		return failLines("ОШИБКА: не удалось сохранить локальную запись интерфейса BROray")
	}
	_ = os.Remove(s.d.RunPath("interface-last-failure.json"))
	return nil
}

// repair is broray_interface_repair.
func (s *service) repair(ctx context.Context, host string, port int, desc string) error {
	name, err := s.selectForAction(ctx, host, port)
	if err != nil {
		return failLines("BRORAY_PROXY_ERROR:PROXY_SAFE_SELECTION_FAILED:не удалось доказать owned или свободный ProxyN")
	}
	ex, err := s.existsName(ctx, name)
	if err != nil {
		return failLines("ОШИБКА: running-config недоступен")
	}
	if ex {
		if err := s.repairOwned(ctx, name, host, port, desc); err != nil {
			return err
		}
	} else {
		if err := s.createUnchecked(ctx, name, host, port, desc); err != nil {
			if ex, _ := s.existsName(ctx, name); ex {
				_, _ = s.deleteUnchecked(ctx, name)
			}
			return err
		}
	}
	if err := s.ownerWrite(ctx, name, "repaired", host, port, desc); err != nil {
		return failLines("ОШИБКА: не удалось сохранить локальную запись интерфейса BROray")
	}
	if err := s.syncRoutePolicy(name); err != nil {
		return failLines("ОШИБКА: " + err.Error())
	}
	return nil
}

// ---------- sync description (interface-sync.sh:151-255) ----------

func (s *service) syncDescription(ctx context.Context) error {
	expected, ok := s.expectedDescription()
	if !ok {
		return failLines("ОШИБКА: описание не установлено")
	}
	sn := s.capture(ctx)
	if sn.err != nil {
		return failLines("ОШИБКА: running-config недоступен")
	}
	r, present := s.loadReceipt()
	name := s.ownerName(sn)
	var oldDesc string
	if name != "" {
		oldDesc = r.Description
	} else {
		if !present {
			return failLines("BRORAY_PROXY_ERROR:PROXY_DELETE_AUTHORITY_REFUSED:description sync требует полный R14C01 receipt")
		}
		name = r.InterfaceName
		if !validName(name) {
			return failLines("BRORAY_PROXY_ERROR:PROXY_DELETE_AUTHORITY_REFUSED:interfaceName receipt недостоверен")
		}
		// drift admissible: the receipt already names the expected
		// description and only the live description differs.
		admissible := r.SchemaVersion == 2 && acceptedContracts[r.Contract] && r.Owner == "BROray" && r.Protocol == "socks5" &&
			r.Description == expected && r.WriteProtocolSHA256 == writePolicySHA256 && len(r.RunningBlockSHA256) == 64 &&
			r.RunningBlockSHA256 == r.StartupBlockSHA256
		if admissible {
			block, _ := extractBlock(sn.running, name)
			live, ok := blockDescription(block)
			admissible = ok && validDescription(live) && live != expected &&
				sourceExact(sn.running, name, r.Upstream.Host, r.Upstream.Port, live, "") &&
				sourceExact(sn.startup, name, r.Upstream.Host, r.Upstream.Port, live, "") && s.runtimeReady(ctx, name, "")
			oldDesc = live
		}
		if !admissible {
			return failLines("BRORAY_PROXY_ERROR:PROXY_DELETE_AUTHORITY_REFUSED:расхождение не ограничено одним именем owned ProxyN")
		}
	}
	host, port := r.Upstream.Host, r.Upstream.Port
	if oldDesc == expected {
		return nil // "Синхронизация не требуется"
	}
	dc, _ := descriptionCommand(name, expected)
	failure := ""
	if s.run(ctx, name, stageCmd{"description", dc}) != nil {
		failure = "description"
	} else if s.save(ctx, name, "save") != nil {
		failure = "save"
	} else if !s.waitPresent(ctx, name, host, port, expected, udpAny) {
		failure = "convergence-timeout"
	}
	if failure != "" {
		rb, _ := descriptionCommand(name, oldDesc)
		if s.run(ctx, name, stageCmd{"rollback-description", rb}) == nil && s.run(ctx, name, stageCmd{"rollback-save", "system configuration save"}) == nil &&
			s.waitPresent(ctx, name, host, port, oldDesc, udpAny) {
			return failLines("BRORAY_PROXY_SYNC_ROLLBACK=PASS failedStage=" + failure)
		}
		return failLines("BRORAY_PROXY_ERROR:PROXY_RECOVERY_REQUIRED:description rollback не доказан failedStage=" + failure)
	}
	if err := s.ownerWrite(ctx, name, "description-synced", host, port, expected); err != nil {
		return failLines("ОШИБКА: не удалось сохранить локальную запись интерфейса BROray")
	}
	return nil
}

// ---------- delete ----------

func (s *service) deleteOwned(ctx context.Context) (string, error) {
	sn := s.capture(ctx)
	if sn.err != nil {
		return "", failLines("ОШИБКА: running-config недоступен")
	}
	if name := s.ownerName(sn); name != "" {
		return s.deleteUnchecked(ctx, name)
	}
	selected := s.selectedName(sn)
	if _, err := os.Lstat(s.receiptPath()); os.IsNotExist(err) && nameAbsent(sn.running, selected) && nameAbsent(sn.startup, selected) {
		return "Интерфейс " + selected + " уже отсутствует; delete mutation не выполнялась", nil
	}
	return "", failLines("BRORAY_PROXY_ERROR:PROXY_DELETE_AUTHORITY_REFUSED:полный R14C01 ownership receipt отсутствует")
}

// ---------- page actions (keenetic-page.sh:309-372, action-common.sh) ----------

// runAction executes one K2–K6 action under s.mu and returns the status.
func (s *service) runAction(ctx context.Context, action string) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch action {
	case "check-upstream":
		return s.status(ctx)
	case "create", "repair":
		sn := s.capture(ctx)
		if sn.err != nil {
			return nil, failLines("ОШИБКА: running-config недоступен")
		}
		exp := s.readExpected(ctx, s.selectedName(sn))
		if !exp.Ready {
			return nil, &actionError{expectedUnavailable: true,
				lines: []string{"Невозможно настроить " + exp.InterfaceName + " без SOCKS Xray и активного сервера."}}
		}
		desc, ok := s.expectedDescription()
		if !ok {
			return nil, failLines("ОШИБКА: описание не установлено")
		}
		var err error
		if action == "create" {
			err = s.create(ctx, exp.Listen, exp.Port, desc)
		} else {
			err = s.repair(ctx, exp.Listen, exp.Port, desc)
		}
		if err != nil {
			return nil, err
		}
		if err := s.syncDescription(ctx); err != nil {
			return nil, err
		}
	case "sync-description":
		if err := s.syncDescription(ctx); err != nil {
			return nil, err
		}
	case "delete":
		if _, err := s.deleteOwned(ctx); err != nil {
			return nil, err
		}
	default:
		return nil, &actionError{expectedUnavailable: true, lines: []string{"Неизвестное действие Keenetic: " + action}}
	}
	_ = sleepCtx(ctx, postActionDelay)
	return s.status(ctx)
}

func lastN(lines []string, n int) string {
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// handleAction is K2–K6: global lock keenetic:<action>, then the action.
func (s *service) handleAction(c *cgi.Req, action string) (any, error) {
	release, ok := s.d.Lock.TryAcquire("keenetic", "keenetic:"+action, "keenetic")
	if !ok {
		return nil, cgi.E(409, "OPERATION_BUSY", "Другая конфликтующая операция BROray уже выполняется.")
	}
	defer release()
	op := s.d.Queue.StartOperation("", "router_operation", "keenetic:"+action, "USER", "protected", "")
	op.Phase("working")
	data, err := s.runAction(context.WithoutCancel(c.R.Context()), action)
	if err != nil {
		var ae *actionError
		if errors.As(err, &ae) && ae.expectedUnavailable {
			op.Finish(errors.New("KEENETIC_EXPECTED_STATE_UNAVAILABLE: " + lastN(ae.lines, 1)))
			return nil, cgi.Ed(409, "KEENETIC_EXPECTED_STATE_UNAVAILABLE", "Операция недоступна без параметров SOCKS Xray и активного сервера.", lastN(ae.lines, 30))
		}
		details := err.Error()
		if errors.As(err, &ae) {
			details = lastN(ae.lines, 30)
		}
		op.Finish(errors.New("KEENETIC_ACTION_FAILED: " + details))
		return nil, cgi.Ed(500, "KEENETIC_ACTION_FAILED", "Не удалось выполнить операцию с управляемым ProxyN.", details)
	}
	op.Finish(nil)
	return data, nil
}

func (s *service) handleStatus(c *cgi.Req) (any, error) {
	var data map[string]any
	var err error
	if strings.Contains("&"+c.RawQuery+"&", "&force=1&") {
		data, err = s.status(c.R.Context())
	} else {
		data, err = s.statusCached(c.R.Context())
	}
	if err != nil {
		return nil, cgi.Ed(500, "KEENETIC_STATUS_FAILED", "Не удалось получить состояние управляемого ProxyN.", lastN(strings.Split(err.Error(), "\n"), 30))
	}
	return data, nil
}

// ---------- services for other areas ----------

// EnsureInterface implements Services.KeeneticEnsureInterface: create or
// repair the owned ProxyN for the current SOCKS inbound and active server,
// then synchronise the description (the caller holds the global fence).
func (s *service) EnsureInterface(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sn := s.capture(ctx)
	if sn.err != nil {
		return fmt.Errorf("KEENETIC_UNAVAILABLE: %v", sn.err)
	}
	host, port, ok := s.socksTarget(ctx)
	if !ok {
		return errors.New("KEENETIC_EXPECTED_STATE_UNAVAILABLE: Операция недоступна без параметров SOCKS Xray и активного сервера.")
	}
	desc, ok := s.expectedDescription()
	if !ok {
		return errors.New("KEENETIC_ACTION_FAILED: Описание интерфейса недопустимо.")
	}
	if err := s.repair(ctx, host, port, desc); err != nil {
		return fmt.Errorf("KEENETIC_ACTION_FAILED: %s", lastN(strings.Split(err.Error(), "\n"), 3))
	}
	if err := s.syncDescription(ctx); err != nil {
		return fmt.Errorf("KEENETIC_ACTION_FAILED: %s", lastN(strings.Split(err.Error(), "\n"), 3))
	}
	_, _ = s.status(ctx)
	return nil
}

// uninstallInterface removes the owned ProxyN by its receipt (uninstall
// stage "keenetic"). Nothing owned → nil; the receipt is removed afterwards.
func (s *service) uninstallInterface(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sn := s.capture(ctx)
	if sn.err != nil {
		return fmt.Errorf("KEENETIC_UNAVAILABLE: %v", sn.err)
	}
	// A temporary UDP check route left by a crash goes first. If its
	// ownership or removal is uncertain, keep ProxyN and its receipt intact.
	if err := s.recoverUDPRoute(ctx); err != nil {
		return fmt.Errorf("UDP_ROUTE_RECOVERY_REQUIRED: %w", err)
	}
	if name := s.ownerName(sn); name != "" {
		if _, err := s.deleteUnchecked(ctx, name); err != nil {
			return fmt.Errorf("KEENETIC_ACTION_FAILED: %s", err.Error())
		}
	}
	return nil
}

// ---------- startup reconciliation (bin/broray-interface-reconcile) ----------

var (
	reconcileAttempts = 10
	reconcileDelay    = 3 * time.Second
	xrayWaitAttempts  = 40
)

func (s *service) writeReconcileStatus(state string, attempt int, method string) {
	_ = os.MkdirAll(s.d.Root.Run(), 0o700)
	_ = os.WriteFile(s.d.RunPath("interface-reconcile.status"), []byte("state="+state+"\nattempt="+strconv.Itoa(attempt)+"\nmethod="+method+"\n"), 0o600)
}

// reconcile runs once at daemon start: reconnect the owned ProxyN to the
// Xray SOCKS upstream (S24broray POSTCMD), then check / sync / repair it.
func (s *service) reconcile(ctx context.Context) {
	r, ok := s.loadReceipt()
	if !ok || r.Owner != "BROray" || (r.SchemaVersion != 1 && r.SchemaVersion != 2) || !validName(r.InterfaceName) {
		return
	}
	// 3.x ran this after Xray had started.
	for i := 0; i < xrayWaitAttempts && !s.d.Svc.IsXrayRunning(); i++ {
		if sleepCtx(ctx, reconcileDelay) != nil {
			return
		}
	}
	if !s.d.Svc.IsXrayRunning() {
		s.writeReconcileStatus("deferred", 0, "xray-not-running")
		return
	}
	for attempt := 1; attempt <= reconcileAttempts; attempt++ {
		if sleepCtx(ctx, reconcileDelay) != nil {
			return
		}
		release, ok := s.d.Lock.TryAcquire("keenetic", "keenetic:reconcile", "keenetic")
		if !ok {
			s.writeReconcileStatus("deferred", attempt, "admission")
			continue
		}
		op := s.d.Queue.StartOperation("", "router_operation", "keenetic:reconcile", "SYSTEM_RECOVERY", "cooperative", "")
		op.Phase("checking")
		state, method, err := s.reconcileOnce(ctx, op)
		op.Finish(err)
		release()
		s.writeReconcileStatus(state, attempt, method)
		return
	}
	s.writeReconcileStatus("deferred", reconcileAttempts, "admission")
}

func (s *service) reconcileOnce(ctx context.Context, op *compat.Operation) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sn := s.capture(ctx)
	name := s.ownerName(sn)
	if sn.err != nil || name == "" {
		return "error", "ownership-check", errors.New("KEENETIC_OWNERSHIP_UNCONFIRMED: ownership-check")
	}
	_ = s.run(ctx, name, stageCmd{"connect", "interface " + name + " proxy connect"}, stageCmd{"admin-up", "interface " + name + " up"})
	op.Phase("committing")
	r, _ := s.loadReceipt()
	check := func() bool {
		sn := s.capture(ctx)
		_, ok := s.ownerValid(sn, name)
		r, _ := s.loadReceipt()
		// 3.5.0: a TCP-only ProxyN is not reconciled (SOCKS5 UDP required),
		// unless this KeeneticOS has no SOCKS5 UDP (no repair loop then)
		udp := snapshotUDP(sn.running, name) && snapshotUDP(sn.startup, name)
		return ok && (udp || s.udpUnsupported(ctx)) && s.runtimeReady(ctx, name, r.Description)
	}
	if check() && s.syncDescription(ctx) == nil && check() {
		return "success", "reconcile", nil
	}
	if host, port, ok := s.socksTarget(ctx); ok {
		desc, _ := s.expectedDescription()
		if s.repair(ctx, host, port, desc) == nil && check() {
			return "success", "reconcile", nil
		}
	}
	_ = r
	if _, ok := s.ownerValid(s.capture(ctx), name); ok {
		return "error", "configuration-confirmed", errors.New("KEENETIC_RECONCILE_FAILED: configuration-confirmed")
	}
	return "error", "recovery-required", errors.New("KEENETIC_RECONCILE_FAILED: recovery-required")
}
