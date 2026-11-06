package dns

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/keenetic"
)

// Timing of the convergence polls (RD:1308-1348); variables for tests.
var (
	convergenceAttempts = 10
	convergenceDelay    = time.Second
)

// planEntry is one entry of a transaction plan: a receipt, a catalog entry or a live record.
type planEntry struct {
	liveRec
	ID      string `json:"id,omitempty"`
	Receipt bool   `json:"receipt,omitempty"` // receiptSchemaVersion==1: re-add check also compares portRaw/portState
}

type txPlan struct {
	Kind              string      `json:"kind"`
	Remove            []planEntry `json:"remove"`
	Add               []planEntry `json:"add"`
	TrackedDesiredIDs []string    `json:"trackedDesiredIds,omitempty"`
	ConflictIDs       []string    `json:"conflictIds"`
	AfterTotal        int         `json:"afterTotal"`
	DohCount          int         `json:"dohCount"`
}

func sameSemantic(a, b liveRec) bool {
	return a.Address == b.Address && a.EffectivePort == b.EffectivePort && a.SNI == b.SNI && a.SPKI == b.SPKI &&
		a.Interface == b.Interface && a.Domain == b.Domain
}

// ---------- Keenetic commands (frozen R14C01 DoT grammar, RD:910-959) ----------

func safeValue(v string) bool { return v != "" && !strings.ContainsAny(v, ";\"\\") }

func ipv4(v string) bool {
	p := strings.Split(v, ".")
	if len(p) != 4 {
		return false
	}
	for _, x := range p {
		if !reDigits.MatchString(x) {
			return false
		}
		n, err := strconv.Atoi(x)
		if err != nil || n > 255 {
			return false
		}
	}
	return true
}

func portOK(v string) bool {
	if !reDigits.MatchString(v) {
		return false
	}
	n, err := strconv.Atoi(v)
	return err == nil && n >= 1 && n <= 65535
}

// commandAllowed is broray_dot_write_command_allowed.
func commandAllowed(c string) bool {
	if c == "system configuration save" {
		return true
	}
	if strings.ContainsAny(c, "\n\r") {
		return false
	}
	f := strings.Fields(c)
	n := len(f)
	if n == 6 && f[0] == "no" && f[1] == "dns-proxy" && f[2] == "tls" && f[3] == "upstream" && ipv4(f[4]) && portOK(f[5]) &&
		c == "no dns-proxy tls upstream "+f[4]+" "+f[5] {
		return true
	}
	if (n == 7 || n == 9 || n == 11 || n == 13) && f[0] == "dns-proxy" && f[1] == "tls" && f[2] == "upstream" && ipv4(f[3]) && portOK(f[4]) &&
		f[5] == "sni" && safeValue(f[6]) {
		exp := "dns-proxy tls upstream " + f[3] + " " + f[4] + " sni " + f[6]
		ok := true
		if n >= 9 {
			ok = ok && f[7] == "spki" && safeValue(f[8])
			exp += " spki " + f[8]
		}
		if n >= 11 {
			ok = ok && f[9] == "on" && safeValue(f[10])
			exp += " on " + f[10]
		}
		if n >= 13 {
			ok = ok && f[11] == "domain" && safeValue(f[12])
			exp += " domain " + f[12]
		}
		return ok && c == exp
	}
	return false
}

// command dispatches one write after the grammar check (broray_dot_command).
func (s *service) command(ctx context.Context, c string) error {
	area := keenetic.AreaDoT
	if c == "system configuration save" {
		area = keenetic.AreaSystem
	}
	if !commandAllowed(c) || keenetic.Allow(area, c) != nil {
		return dotErr("DOT_COMMAND_NOT_AUTHORIZED", "Команда не входит в замороженный R14C01 DoT grammar.", "")
	}
	res, err := s.d.App.T.Parse(ctx, []string{c})
	if err != nil {
		return err
	}
	if len(res) != 1 || !res[0].OK {
		return errors.New("rejected")
	}
	return nil
}

func addCommand(e liveRec) (string, bool) {
	for _, v := range []string{e.Address, strconv.Itoa(e.EffectivePort), e.SNI, e.SPKI, e.Interface, e.Domain} {
		if strings.ContainsAny(v, " \t\n\r\v\f;\"\\") {
			return "", false
		}
	}
	c := "dns-proxy tls upstream " + e.Address + " " + strconv.Itoa(e.EffectivePort) + " sni " + e.SNI
	if e.SPKI != "" {
		c += " spki " + e.SPKI
	}
	if e.Interface != "" {
		c += " on " + e.Interface
	}
	if e.Domain != "" {
		c += " domain " + e.Domain
	}
	return c, true
}

func deleteCommand(e liveRec) (string, bool) {
	if strings.ContainsAny(e.Address, " \t\n\r\v\f;\"\\:") {
		return "", false
	}
	return "no dns-proxy tls upstream " + e.Address + " " + strconv.Itoa(e.EffectivePort), true
}

// entryPresent is broray_dot_transaction_entry_present.
func entryPresent(obs observed, e planEntry) bool {
	exact, sel, full := 0, 0, false
	for _, r := range obs.Dot {
		if r.clean() && sameSemantic(r, e.liveRec) {
			exact++
		}
		if r.Address == e.Address && r.EffectivePort == e.EffectivePort {
			sel++
		}
		if sameSemantic(r, e.liveRec) && r.PortRaw == e.PortRaw && r.PortState == e.PortState {
			full = true
		}
	}
	return exact == 1 && sel == 1 && (!e.Receipt || full)
}

func semanticKeys(items []liveRec) []string {
	out := make([]string, 0, len(items))
	for _, r := range items {
		out = append(out, strings.Join([]string{r.Address, strconv.Itoa(r.EffectivePort), r.SNI, r.SPKI, r.Interface, r.Domain}, "\x00"))
	}
	sort.Strings(out)
	return out
}

func fullKeys(items []liveRec) []string {
	out := make([]string, 0, len(items))
	for _, r := range items {
		out = append(out, strings.Join([]string{r.Address, r.PortRaw, strconv.Itoa(r.EffectivePort), r.PortState, r.SNI, r.SPKI, r.Interface, r.Domain}, "\x00"))
	}
	sort.Strings(out)
	return out
}

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

// waitPlanConvergence is broray_dot_wait_for_plan_convergence.
func (s *service) waitPlanConvergence(ctx context.Context, before observed, p txPlan) (observed, bool) {
	var kept []liveRec
	for _, o := range before.Dot {
		removed := false
		for _, r := range p.Remove {
			if sameSemantic(o, r.liveRec) {
				removed = true
			}
		}
		if !removed {
			kept = append(kept, o)
		}
	}
	expected := append([]liveRec(nil), kept...)
	for _, a := range p.Add {
		expected = append(expected, a.liveRec)
	}
	for attempt := 1; attempt <= convergenceAttempts; attempt++ {
		obs, err := s.fetchObserved(ctx)
		if err == nil && obs.Determinate && obs.RuntimeReconciled && equalStrings(semanticKeys(obs.Dot), semanticKeys(expected)) &&
			obs.DohCount == before.DohCount && obs.TotalSecure == before.TotalSecure-len(p.Remove)+len(p.Add) {
			ok := true
			for _, k := range kept {
				found := false
				for _, o := range obs.Dot {
					if sameSemantic(o, k) && o.PortRaw == k.PortRaw && o.PortState == k.PortState {
						found = true
					}
				}
				ok = ok && found
			}
			if ok {
				return obs, true
			}
		}
		if attempt < convergenceAttempts {
			if sleepCtx(ctx, convergenceDelay) != nil {
				break
			}
		}
	}
	return observed{}, false
}

// ---------- transaction marker (RD:1237-1563) ----------

type transaction struct {
	Kind          string
	PlanPath      string
	RunningBackup string
	ConfigBackup  string
	StateBackup   string
}

func (s *service) txPaths(kind string) transaction {
	pid := strconv.Itoa(os.Getpid())
	tmp := s.d.Root.Tmp()
	t := transaction{Kind: kind, ConfigBackup: filepath.Join(tmp, "dot-transaction-config."+pid+".json"),
		StateBackup: filepath.Join(tmp, "dot-transaction-state."+pid+".json")}
	if kind != "test" {
		t.PlanPath = filepath.Join(tmp, "dot-transaction-plan."+pid+".json")
		t.RunningBackup = filepath.Join(tmp, "dot-transaction-running."+pid+".json")
	}
	return t
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writeBytes(dst, b)
}

func writeBytes(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".part"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// arm is broray_dot_transaction_arm: back up local files, persist the plan
// and the pre-state, then write the marker.
func (s *service) arm(t transaction, p *txPlan, before *observed) error {
	if exists(s.markerPath()) {
		return dotErr("DOT_RECOVERY_REQUIRED", "Предыдущая DNS-over-TLS транзакция требует проверки и восстановления.", "")
	}
	if t.Kind != "test" {
		pb, err1 := json.Marshal(p)
		bb, err2 := json.Marshal(before)
		if err1 != nil || err2 != nil || writeBytes(t.PlanPath, pb) != nil || writeBytes(t.RunningBackup, bb) != nil {
			return errors.New("transaction arm failed")
		}
	}
	if err := copyFile(s.configPath(), t.ConfigBackup); err != nil {
		return err
	}
	if err := copyFile(s.statePath(), t.StateBackup); err != nil {
		_ = os.Remove(t.ConfigBackup)
		return err
	}
	marker := map[string]any{"schemaVersion": 1, "operation": t.Kind, "pid": os.Getpid(), "state": "active", "startedAt": stamp(s.now()),
		"recovery": map[string]any{"plan": nullable(t.PlanPath), "managed": nil, "runningBackup": nullable(t.RunningBackup),
			"configBackup": t.ConfigBackup, "stateBackup": t.StateBackup}}
	if err := writeMap(s.markerPath(), marker); err != nil {
		_ = os.Remove(t.ConfigBackup)
		_ = os.Remove(t.StateBackup)
		return err
	}
	return nil
}

func (s *service) cleanupTx(t transaction) {
	for _, p := range []string{t.ConfigBackup, t.StateBackup, t.PlanPath, t.RunningBackup} {
		if p != "" {
			_ = os.Remove(p)
		}
	}
}

// disarm is broray_dot_transaction_disarm.
func (s *service) disarm(t transaction) error {
	if err := os.Remove(s.markerPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	if exists(s.markerPath()) {
		return errors.New("marker still present")
	}
	s.cleanupTx(t)
	return nil
}

// rollback is broray_dot_transaction_rollback: reconcile the live object to
// the pre-state, restore local files and retire the marker. Returns true
// when everything was proven.
func (s *service) rollback(ctx context.Context, t transaction) bool {
	ok := true
	var p txPlan
	var before observed
	var actual observed
	if t.Kind != "test" {
		pb, err1 := os.ReadFile(t.PlanPath)
		bb, err2 := os.ReadFile(t.RunningBackup)
		if err1 != nil || err2 != nil || json.Unmarshal(pb, &p) != nil || json.Unmarshal(bb, &before) != nil {
			ok = false
		}
		if ok {
			var err error
			actual, err = s.fetchObserved(ctx)
			ok = err == nil
		}
	}
	if ok {
		switch t.Kind {
		case "apply":
			for _, e := range p.Add {
				if entryPresent(actual, e) {
					c, good := deleteCommand(e.liveRec)
					if !good || s.command(ctx, c) != nil {
						ok = false
					}
				}
			}
			for _, e := range p.Remove {
				if !entryPresent(actual, e) {
					c, good := addCommand(e.liveRec)
					if !good || s.command(ctx, c) != nil {
						ok = false
					}
				}
			}
		case "delete":
			for _, e := range p.Remove {
				if !entryPresent(actual, e) {
					c, good := addCommand(e.liveRec)
					if !good || s.command(ctx, c) != nil {
						ok = false
					}
				}
			}
		case "test":
		default:
			ok = false
		}
	}
	if t.Kind != "test" {
		if ok && s.command(ctx, "system configuration save") != nil {
			ok = false
		}
		converged := false
		for poll := 1; ok && poll <= convergenceAttempts; poll++ {
			v, err := s.fetchObserved(ctx)
			if err == nil && v.Determinate && equalStrings(fullKeys(v.Dot), fullKeys(before.Dot)) && v.DohCount == before.DohCount &&
				v.TotalSecure == before.TotalSecure {
				converged = true
				break
			}
			if poll < convergenceAttempts && sleepCtx(ctx, convergenceDelay) != nil {
				break
			}
		}
		if !converged {
			ok = false
		}
	}
	if copyFile(t.ConfigBackup, s.configPath()) != nil || copyFile(t.StateBackup, s.statePath()) != nil {
		ok = false
	}
	if ok {
		if err := os.Remove(s.markerPath()); err != nil && !os.IsNotExist(err) {
			return false
		}
		s.cleanupTx(t)
		return true
	}
	return false
}

// recoverMarker retries the rollback of a transaction left by a daemon that
// stopped mid-way (3.x: signal handler; 3.5.0: also at daemon start).
func (s *service) recoverMarker(ctx context.Context) {
	m, err := readMap(s.markerPath())
	if err != nil {
		return
	}
	rec, _ := m["recovery"].(map[string]any)
	t := transaction{Kind: strOr(m["operation"], ""), PlanPath: strOr(rec["plan"], ""), RunningBackup: strOr(rec["runningBackup"], ""),
		ConfigBackup: strOr(rec["configBackup"], ""), StateBackup: strOr(rec["stateBackup"], "")}
	if t.ConfigBackup == "" || t.StateBackup == "" {
		return
	}
	if s.rollback(ctx, t) {
		s.d.Log.Printf("dns: recovered interrupted DNS-over-TLS %s transaction", t.Kind)
	} else {
		s.d.Log.Printf("dns: interrupted DNS-over-TLS %s transaction could not be rolled back; recovery marker kept", t.Kind)
	}
}

// ---------- test (RD:707-850) ----------

func (s *service) runTest(ctx context.Context, req map[string]any) (map[string]any, error) {
	if exists(s.markerPath()) {
		return nil, dotErr("DOT_RECOVERY_REQUIRED", "Предыдущая DNS-over-TLS транзакция требует проверки и восстановления.", "")
	}
	if _, err := s.ensureFiles(); err != nil {
		return nil, err
	}
	ids, err := validateRequest(req)
	if err != nil {
		return nil, err
	}
	tx := s.txPaths("test")
	if err := s.arm(tx, nil, nil); err != nil {
		var de *dotError
		if errors.As(err, &de) {
			return nil, err
		}
		return nil, dotErr("DOT_TEST_FAILED", "Проверка DNS-over-TLS прервана; локальное состояние восстановлено.", "arm")
	}
	testedAtT := s.now()
	testedAt, testedEpoch := stamp(testedAtT), testedAtT.Unix()
	var tests []any
	allOK := true
	for _, id := range ids {
		c, _ := findCatalog(id)
		pr := s.probe(ctx, c)
		row := map[string]any{"id": c.ID, "address": c.Address, "effectivePort": 853, "sni": c.SNI, "spki": "", "interface": "",
			"domain": "", "ok": pr.OK, "status": pr.Status, "latencyMs": pr.LatencyMs, "message": pr.Message,
			"testedAt": testedAt, "testedEpoch": testedEpoch}
		allOK = allOK && pr.OK
		tests = append(tests, row)
	}
	if tests == nil {
		tests = []any{}
	}
	fail := ""
	s.fileMu.Lock()
	f, ferr := s.ensureFiles()
	if ferr != nil {
		fail = "config-commit"
	}
	updatedAt := stamp(s.now())
	if fail == "" {
		cfg := f.config
		cfg["requestedIds"], cfg["selectedIds"] = strSlice(ids), strSlice(ids)
		cfg["migrationState"], cfg["updatedAt"] = "none", updatedAt
		if writeMap(s.configPath(), cfg) != nil {
			fail = "config-commit"
		}
	}
	if fail == "" {
		st := f.state
		st["tests"], st["lastTestedAt"], st["lastTestedEpoch"], st["lastError"] = tests, testedAt, testedEpoch, nil
		st["lastOperation"] = map[string]any{"type": "test", "success": allOK, "selectedIds": strSlice(ids), "completedAt": updatedAt}
		st["updatedAt"] = updatedAt
		if writeMap(s.statePath(), st) != nil {
			fail = "state-commit"
		}
	}
	s.fileMu.Unlock()
	if fail != "" {
		if s.rollback(ctx, tx) {
			return nil, dotErr("DOT_TEST_FAILED", "Проверка DNS-over-TLS прервана; локальное состояние восстановлено.", fail)
		}
		return nil, dotErr("DOT_TEST_FAILED", "Проверка DNS-over-TLS прервана; требуется восстановление по recovery-marker.", fail)
	}
	if err := s.disarm(tx); err != nil {
		return nil, dotErr("DOT_RECOVERY_REQUIRED", "Проверка завершена, но транзакционный marker не удалось безопасно закрыть.", "")
	}
	return s.status(ctx)
}

// ---------- apply (RD:1565-1760) ----------

func (s *service) runApply(ctx context.Context, req map[string]any) (map[string]any, error) {
	if exists(s.markerPath()) {
		return nil, dotErr("DOT_RECOVERY_REQUIRED", "Предыдущая DNS-over-TLS транзакция требует проверки и восстановления.", "")
	}
	f, err := s.ensureFiles()
	if err != nil {
		return nil, err
	}
	ids, err := validateRequest(req)
	if err != nil {
		return nil, err
	}
	if !testsFreshAndOK(f.state, ids, s.now().Unix()) {
		return nil, dotErr("DOT_TEST_REQUIRED", "Сначала успешно проверьте все выбранные DNS-over-TLS серверы.", "")
	}
	current, err := s.fetchObserved(ctx)
	if err != nil {
		return nil, err
	}
	if !current.Determinate || !current.RuntimeReconciled {
		return nil, dotErr("DOT_OBSERVATION_UNDERDETERMINED", "DoT-записи running-config и runtime не сведены однозначно; изменение запрещено.", "")
	}
	desired := make([]catEntry, 0, len(ids))
	for _, id := range ids {
		c, _ := findCatalog(id)
		desired = append(desired, c)
	}
	p := txPlan{Kind: "apply", Remove: []planEntry{}, Add: []planEntry{}, ConflictIDs: []string{}}
	semCount := func(e liveRec) int {
		n := 0
		for _, r := range current.Dot {
			if sameSemantic(e, r) {
				n++
			}
		}
		return n
	}
	for _, d := range desired {
		e := d.endpoint()
		if selectorCount(current.Dot, e.Address, e.EffectivePort) > 0 && semCount(e) != 1 {
			p.ConflictIDs = append(p.ConflictIDs, d.ID)
		}
	}
	for _, rc := range f.receipts {
		if contains(ids, rc.ID) {
			continue
		}
		n := 0
		for _, r := range current.Dot {
			if sameFull(rc.rec(), r) {
				n++
			}
		}
		if n == 1 {
			p.Remove = append(p.Remove, planEntry{liveRec: rc.rec(), ID: rc.ID, Receipt: true})
		}
	}
	for _, d := range desired {
		if semCount(d.endpoint()) == 0 {
			p.Add = append(p.Add, planEntry{liveRec: d.endpoint(), ID: d.ID})
		}
	}
	p.TrackedDesiredIDs = uniqueSorted(ids)
	p.AfterTotal = current.TotalSecure - len(p.Remove) + len(p.Add)
	p.DohCount = current.DohCount
	if len(p.ConflictIDs) > 0 {
		b, _ := json.Marshal(p.ConflictIDs)
		return nil, dotErr("DOT_SELECTOR_CONFLICT", "Адрес и порт выбранного DoT уже заняты иной или неоднозначной записью; внешние записи сохранены.", string(b))
	}
	if p.AfterTotal > maxServers {
		return nil, dotErr("DOT_LIMIT_EXCEEDED", "После экспорта будет "+strconv.Itoa(p.AfterTotal)+" из "+strconv.Itoa(maxServers)+" DoT/DoH-серверов.", "")
	}
	tx := s.txPaths("apply")
	if err := s.arm(tx, &p, &current); err != nil {
		var de *dotError
		if errors.As(err, &de) {
			return nil, err
		}
		return nil, dotErr("DOT_APPLY_FAILED", "Экспорт DNS-over-TLS не завершён. Выполнен и проверен откат.", "arm")
	}
	failure := ""
	for _, e := range p.Remove {
		c, ok := deleteCommand(e.liveRec)
		if !ok {
			failure = "remove-command"
			break
		}
		if s.command(ctx, c) != nil {
			failure = "remove-command-rejected"
			break
		}
	}
	if failure == "" {
		for _, e := range p.Add {
			c, ok := addCommand(e.liveRec)
			if !ok {
				failure = "add-command"
				break
			}
			if s.command(ctx, c) != nil {
				failure = "add-command-rejected"
				break
			}
		}
	}
	if failure == "" && s.command(ctx, "system configuration save") != nil {
		failure = "save"
	}
	var verify observed
	if failure == "" {
		var ok bool
		if verify, ok = s.waitPlanConvergence(ctx, current, p); !ok {
			failure = "verify-timeout"
		}
	}
	at := stamp(s.now())
	var managed []any
	if failure == "" {
		for _, id := range p.TrackedDesiredIDs {
			c, _ := findCatalog(id)
			want := c.endpoint()
			var matches []liveRec
			for _, r := range verify.Dot {
				if r.clean() && sameSemantic(want, r) {
					matches = append(matches, r)
				}
			}
			if len(matches) != 1 || selectorCount(verify.Dot, want.Address, want.EffectivePort) != 1 {
				failure = "local-managed-build"
				break
			}
			managed = append(managed, receiptObject(c.ID, c.Provider, matches[0], at))
		}
	}
	if failure == "" {
		s.fileMu.Lock()
		cfg, err := readMap(s.configPath())
		if err != nil {
			failure = "local-config-commit"
		} else {
			var eff []string
			for _, m := range managed {
				eff = append(eff, m.(map[string]any)["id"].(string))
			}
			cfg["schemaVersion"], cfg["requestedIds"], cfg["selectedIds"] = 3, strSlice(ids), strSlice(ids)
			cfg["effectiveIds"], cfg["migrationState"], cfg["managed"], cfg["updatedAt"] = strSlice(uniqueSorted(eff)), "none", nonNil(managed), at
			if writeMap(s.configPath(), cfg) != nil {
				failure = "local-config-commit"
			}
		}
		if failure == "" {
			st, err := readMap(s.statePath())
			if err != nil {
				failure = "local-state-commit"
			} else {
				st["lastAppliedAt"], st["lastError"], st["updatedAt"] = at, nil, at
				st["lastOperation"] = map[string]any{"type": "apply", "success": true, "rolledBack": false, "selectedIds": strSlice(ids), "completedAt": at}
				if writeMap(s.statePath(), st) != nil {
					failure = "local-state-commit"
				}
			}
		}
		s.fileMu.Unlock()
	}
	if failure != "" {
		rolled := s.rollback(ctx, tx)
		msg := "Экспорт DNS-over-TLS не завершён."
		switch {
		case strings.HasPrefix(failure, "add-"):
			msg = "Keenetic отклонил добавление DNS-over-TLS."
		case strings.HasPrefix(failure, "remove-"):
			msg = "Keenetic отклонил удаление авторитетной записи BROray."
		case failure == "save":
			msg = "Keenetic не сохранил конфигурацию DNS-over-TLS."
		case failure == "verify-timeout":
			msg = "Keenetic не достиг точного состояния DNS-over-TLS за ограниченное время."
		case strings.HasPrefix(failure, "local-"):
			msg = "Не удалось атомарно зафиксировать локальный DoT receipt."
		}
		if rolled {
			msg += " Выполнен и проверен откат."
		} else {
			msg += " Откат не подтверждён; дальнейшие изменения заблокированы recovery-marker."
		}
		return nil, dotErr("DOT_APPLY_FAILED", msg, failure)
	}
	if err := s.disarm(tx); err != nil {
		return nil, dotErr("DOT_RECOVERY_REQUIRED", "DNS-over-TLS применён, но транзакционный marker не удалось безопасно закрыть.", "")
	}
	return s.status(ctx)
}

func nonNil(a []any) []any {
	if a == nil {
		return []any{}
	}
	return a
}

// ---------- delete preview / delete (RD:1762-2003) ----------

// fingerprint is broray_dot_delete_fingerprint over the jq -cS canonical form.
func fingerprint(ids []string, entries []map[string]any) (string, bool) {
	if len(ids) == 0 || len(ids) > maxServers || !equalStrings(ids, uniqueSorted(ids)) || len(entries) != len(ids) {
		return "", false
	}
	list := make([]any, 0, len(entries))
	for i, e := range entries {
		if e["id"] != ids[i] {
			return "", false
		}
		if a, _ := e["address"].(string); a == "" {
			return "", false
		}
		if p, ok := e["effectivePort"].(int); !ok || p != 853 {
			return "", false
		}
		if sn, _ := e["sni"].(string); sn == "" {
			return "", false
		}
		list = append(list, e)
	}
	sum, err := compat.CanonicalSHA256(map[string]any{"schemaVersion": 1, "serverIds": strSlice(ids), "entries": list})
	if err != nil {
		return "", false
	}
	return sum, true
}

func entryObject(id string, r liveRec) map[string]any {
	return map[string]any{"id": id, "address": r.Address, "effectivePort": r.EffectivePort, "sni": r.SNI, "spki": r.SPKI,
		"interface": r.Interface, "domain": r.Domain}
}

func (s *service) runDeletePreview(ctx context.Context) (map[string]any, error) {
	if exists(s.markerPath()) {
		return nil, dotErr("DOT_RECOVERY_REQUIRED", "Предыдущая DNS-over-TLS транзакция требует проверки и восстановления.", "")
	}
	if _, err := s.ensureFiles(); err != nil {
		return nil, err
	}
	st, err := s.status(ctx)
	if err != nil {
		return nil, err
	}
	sel, _ := st["selectedIds"].([]any)
	if st["deleteEligible"] != true || len(sel) == 0 {
		reason := "delete-not-eligible"
		if r, ok := st["deleteBlockedReason"].(string); ok {
			reason = r
		}
		return nil, dotErr("DOT_DELETE_AUTHORITY_REFUSED", "Удаление недоступно: текущий набор DoT нельзя однозначно подтвердить.", reason)
	}
	ids, _ := stringList(sel)
	ids = sortedIDs(ids)
	actual, _ := st["actual"].(map[string]any)
	cls, _ := actual["dot"].([]classified)
	var entries []map[string]any
	for _, id := range ids {
		var matches []classified
		for _, x := range cls {
			if x.DeleteEligible && contains(x.CatalogMatchIDs, id) {
				matches = append(matches, x)
			}
		}
		if len(matches) == 1 {
			entries = append(entries, entryObject(id, matches[0].liveRec))
		}
	}
	if len(entries) == 0 {
		return nil, dotErr("DOT_DELETE_AUTHORITY_REFUSED", "Удаление недоступно: подтверждённые записи не найдены.", "")
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i]["id"].(string) < entries[j]["id"].(string) })
	fp, ok := fingerprint(ids, entries)
	if !ok {
		return nil, dotErr("DEPENDENCY_MISSING", "Не удалось вычислить контрольную сумму подтверждения удаления.", "")
	}
	list := make([]any, 0, len(entries))
	for _, e := range entries {
		list = append(list, e)
	}
	return map[string]any{"schemaVersion": 1, "expectedFingerprint": fp, "serverIds": strSlice(ids), "entries": list}, nil
}

func (s *service) runDelete(ctx context.Context, req map[string]any) (map[string]any, error) {
	if exists(s.markerPath()) {
		return nil, dotErr("DOT_RECOVERY_REQUIRED", "Предыдущая DNS-over-TLS транзакция требует проверки и восстановления.", "")
	}
	stale := dotErr("DOT_DELETE_CONFIRMATION_STALE", "Состояние DNS-over-TLS изменилось. Обновите список и подтвердите удаление повторно.", "")
	cfg0, err1 := readMap(s.configPath())
	st0, err2 := readMap(s.statePath())
	if err1 != nil || err2 != nil || !numIs(cfg0["schemaVersion"], 3) || !numIs(st0["schemaVersion"], 1) {
		return nil, stale
	}
	f, err := s.ensureFiles()
	if err != nil {
		return nil, err
	}
	if fp, ok := req["expectedFingerprint"].(string); !ok || !numIs(req["schemaVersion"], 1) {
		_ = fp
		return nil, dotErr("REQUEST_INVALID", "Подтверждение удаления имеет неподдерживаемый формат.", "")
	}
	ids, err := validateRequest(req)
	if err != nil {
		return nil, err
	}
	expected := req["expectedFingerprint"].(string)
	if expected == "" {
		return nil, dotErr("REQUEST_INVALID", "Запрос удаления не содержит контрольную сумму подтверждённого набора.", "")
	}
	for _, ch := range expected {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return nil, dotErr("REQUEST_INVALID", "Контрольная сумма подтверждения удаления некорректна.", "")
		}
	}
	if len(expected) != 64 {
		return nil, dotErr("REQUEST_INVALID", "Контрольная сумма подтверждения удаления некорректна.", "")
	}
	if !equalStrings(sortedIDs(f.requested()), sortedIDs(ids)) {
		return nil, dotErr("DOT_DELETE_CONFIRMATION_STALE", "Выбранный набор DNS-over-TLS изменился после подтверждения. Обновите список и подтвердите удаление повторно.", "")
	}
	current, err := s.fetchObserved(ctx)
	if err != nil {
		return nil, err
	}
	p := txPlan{Kind: "delete", Remove: []planEntry{}, Add: []planEntry{}, ConflictIDs: []string{}}
	for _, id := range ids {
		c, _ := findCatalog(id)
		want := c.endpoint()
		var matches []liveRec
		for _, r := range current.Dot {
			if r.clean() && sameSemantic(want, r) {
				matches = append(matches, r)
			}
		}
		sc := selectorCount(current.Dot, want.Address, want.EffectivePort)
		if len(matches) == 1 && sc == 1 {
			p.Remove = append(p.Remove, planEntry{liveRec: matches[0], ID: id})
		}
		if sc > 0 && (sc != 1 || len(matches) != 1) {
			p.ConflictIDs = append(p.ConflictIDs, id)
		}
	}
	sorted := sortedIDs(ids)
	var entries []map[string]any
	for _, id := range sorted {
		c, _ := findCatalog(id)
		want := c.endpoint()
		for _, r := range current.Dot {
			if r.clean() && sameSemantic(want, r) {
				entries = append(entries, entryObject(id, r))
				break
			}
		}
	}
	actualFP, ok := fingerprint(sorted, entries)
	if !ok {
		return nil, dotErr("DOT_DELETE_CONFIRMATION_STALE", "Подтверждённый набор DNS-over-TLS больше не совпадает с текущим. Ничего не удалено.", "")
	}
	if actualFP != expected {
		return nil, dotErr("DOT_DELETE_CONFIRMATION_STALE", "Список DNS-over-TLS изменился после подтверждения. Ничего не удалено; обновите список и подтвердите операцию повторно.", "")
	}
	if !current.Determinate || !current.RuntimeReconciled || len(p.ConflictIDs) > 0 {
		b, _ := json.Marshal(p.ConflictIDs)
		return nil, dotErr("DOT_SELECTOR_CONFLICT", "Удаление запрещено: выбранная DoT-запись не совпадает с единственным точным selector address+port.", string(b))
	}
	tx := s.txPaths("delete")
	if err := s.arm(tx, &p, &current); err != nil {
		var de *dotError
		if errors.As(err, &de) {
			return nil, err
		}
		return nil, dotErr("DOT_DELETE_FAILED", "Удаление DNS-over-TLS не завершено. Выполнен и проверен откат.", "arm")
	}
	failure := ""
	for _, e := range p.Remove {
		c, ok := deleteCommand(e.liveRec)
		if !ok {
			failure = "remove-command"
			break
		}
		if s.command(ctx, c) != nil {
			failure = "remove-command-rejected"
			break
		}
	}
	if failure == "" && s.command(ctx, "system configuration save") != nil {
		failure = "save"
	}
	if failure == "" {
		if _, ok := s.waitPlanConvergence(ctx, current, p); !ok {
			failure = "verify-timeout"
		}
	}
	at := stamp(s.now())
	if failure == "" {
		s.fileMu.Lock()
		cfg, err := readMap(s.configPath())
		if err != nil {
			failure = "local-config-commit"
		} else {
			cfg["requestedIds"], cfg["selectedIds"], cfg["managed"], cfg["effectiveIds"], cfg["updatedAt"] = []any{}, []any{}, []any{}, []any{}, at
			if writeMap(s.configPath(), cfg) != nil {
				failure = "local-config-commit"
			}
		}
		if failure == "" {
			st, err := readMap(s.statePath())
			if err != nil {
				failure = "local-state-commit"
			} else {
				st["lastDeletedAt"], st["lastError"], st["updatedAt"] = at, nil, at
				st["lastOperation"] = map[string]any{"type": "delete", "success": true, "rolledBack": false, "completedAt": at}
				if writeMap(s.statePath(), st) != nil {
					failure = "local-state-commit"
				}
			}
		}
		s.fileMu.Unlock()
	}
	if failure != "" {
		msg := "Удаление DNS-over-TLS не завершено. Откат не подтверждён; дальнейшие изменения заблокированы recovery-marker."
		if s.rollback(ctx, tx) {
			msg = "Удаление DNS-over-TLS не завершено. Выполнен и проверен откат."
		}
		return nil, dotErr("DOT_DELETE_FAILED", msg, failure)
	}
	if err := s.disarm(tx); err != nil {
		return nil, dotErr("DOT_RECOVERY_REQUIRED", "DNS-over-TLS удалён, но транзакционный marker не удалось безопасно закрыть.", "")
	}
	return s.status(ctx)
}

// uninstall removes the DoT upstreams BROray owns by its receipts (uninstall
// stage "dot"): each receipt whose exact record is present once is removed,
// then the configuration is saved and the absence verified. Foreign and
// changed records are never touched. Nothing owned → nil.
func (s *service) uninstall(ctx context.Context) error {
	f, err := s.ensureFiles()
	if err != nil {
		return err
	}
	if len(f.receipts) == 0 {
		return nil
	}
	current, err := s.fetchObserved(ctx)
	if err != nil {
		return err
	}
	var remove []liveRec
	for _, rc := range f.receipts {
		n := 0
		for _, r := range current.Dot {
			if sameFull(rc.rec(), r) {
				n++
			}
		}
		if n == 1 && selectorCount(current.Dot, rc.Address, rc.Port) == 1 {
			remove = append(remove, rc.rec())
		}
	}
	for _, r := range remove {
		c, ok := deleteCommand(r)
		if !ok {
			return errors.New("DOT_DELETE_FAILED: invalid owned record")
		}
		if err := s.command(ctx, c); err != nil {
			return errors.New("DOT_DELETE_FAILED: Keenetic отклонил удаление DNS-over-TLS.")
		}
	}
	if len(remove) > 0 {
		if err := s.command(ctx, "system configuration save"); err != nil {
			return errors.New("DOT_DELETE_FAILED: Keenetic не сохранил конфигурацию DNS-over-TLS.")
		}
		gone := false
		for attempt := 1; attempt <= convergenceAttempts && !gone; attempt++ {
			if v, err := s.fetchObserved(ctx); err == nil {
				gone = true
				for _, r := range remove {
					if selectorCount(v.Dot, r.Address, r.EffectivePort) != 0 {
						gone = false
					}
				}
			}
			if !gone && attempt < convergenceAttempts && sleepCtx(ctx, convergenceDelay) != nil {
				break
			}
		}
		if !gone {
			return errors.New("DOT_DELETE_FAILED: Keenetic не подтвердил удаление DNS-over-TLS.")
		}
	}
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	cfg, err := readMap(s.configPath())
	if err != nil {
		return err
	}
	cfg["managed"], cfg["effectiveIds"], cfg["updatedAt"] = []any{}, []any{}, stamp(s.now())
	return writeMap(s.configPath(), cfg)
}
