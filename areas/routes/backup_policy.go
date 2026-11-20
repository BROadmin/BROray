package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/geodata"
	"github.com/BROadmin/broray/modules/routepolicy"
)

type policyBackup struct {
	State      routepolicy.State `json:"state"`
	AutoUpdate bool              `json:"autoUpdate"`
	Geodata    json.RawMessage   `json:"geodata"`
}

func (e *engine) backupPolicy(ctx context.Context) (*policyBackup, error) {
	pending, err := routepolicy.RecoveryPending(ctx, e.d.Root)
	if err != nil {
		return nil, err
	}
	if pending {
		return nil, backupInvalid("восстановление правил ещё не завершено")
	}
	state, err := routepolicy.ReadState(ctx, e.root)
	if err != nil {
		return nil, err
	}
	schedule, err := routepolicy.ReadGeoSchedule(ctx, e.root)
	if err != nil {
		return nil, err
	}
	out := &policyBackup{State: state, AutoUpdate: schedule.Enabled, Geodata: json.RawMessage("null")}
	store, err := geodata.Open(filepath.Join(e.d.Root.Runtime(), geodata.AssetDirectory))
	if errors.Is(err, os.ErrNotExist) && state.GeoRevision == "" {
		if err = checkBackupPolicy(state, geodata.Snapshot{}); err != nil {
			return nil, err
		}
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer store.Close()
	snapshot, err := store.Load(ctx)
	if err != nil {
		return nil, err
	}
	if snapshot.Revision != state.GeoRevision {
		return nil, backupInvalid("ревизия геоданных не соответствует правилам")
	}
	if err = checkBackupPolicy(state, snapshot); err != nil {
		return nil, err
	}
	out.Geodata, err = store.ExportArchive(ctx)
	return out, err
}

// Preserve the legacy routes envelope. Only the new optional field receives
// its strict schema; a missing field means keep the current saved policy.
func policyBackupInput(raw []byte) (*policyBackup, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return nil, backupInvalid("раздел маршрутов повреждён")
	}
	var found *policyBackup
	seen := false
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, err
		}
		name, ok := key.(string)
		if !ok {
			return nil, backupInvalid("раздел маршрутов повреждён")
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return nil, err
		}
		if !strings.EqualFold(name, "policy") {
			continue
		}
		if seen || name != "policy" {
			return nil, backupInvalid("неоднозначные правила маршрутизации")
		}
		seen = true
		found, err = decodePolicyBackup(value)
		if err != nil {
			return nil, err
		}
	}
	if end, err := d.Token(); err != nil || end != json.Delim('}') {
		return nil, backupInvalid("раздел маршрутов повреждён")
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, backupInvalid("лишние данные раздела маршрутов")
	}
	return found, nil
}

func decodePolicyBackup(raw []byte) (*policyBackup, error) {
	if len(raw) > 4<<20 {
		return nil, backupInvalid("правила и геоданные слишком велики")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return nil, backupInvalid("правила маршрутизации повреждены")
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, err
		}
		name, ok := key.(string)
		if !ok || fields[name] != nil || name != "state" && name != "autoUpdate" && name != "geodata" {
			return nil, backupInvalid("неоднозначные поля правил маршрутизации")
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return nil, err
		}
		fields[name] = value
	}
	if end, err := d.Token(); err != nil || end != json.Delim('}') || len(fields) != 3 {
		return nil, backupInvalid("неполные правила маршрутизации")
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, backupInvalid("лишние данные правил маршрутизации")
	}
	state, err := routepolicy.DecodeState(fields["state"])
	if err != nil {
		return nil, err
	}
	enabled := bytes.TrimSpace(fields["autoUpdate"])
	if !bytes.Equal(enabled, []byte("true")) && !bytes.Equal(enabled, []byte("false")) {
		return nil, backupInvalid("автообновление геоданных повреждено")
	}
	return &policyBackup{State: state, AutoUpdate: bytes.Equal(enabled, []byte("true")), Geodata: fields["geodata"]}, nil
}

func checkBackupPolicy(state routepolicy.State, snapshot geodata.Snapshot) error {
	rules := append([]routepolicy.Rule(nil), state.Rules...)
	for i := range rules {
		if len(rules[i].CountryCodes)+len(rules[i].GeoSiteTags) > 0 {
			rules[i].Enabled = true
		}
	}
	_, err := routepolicy.Compile(rules, snapshot, routepolicy.OutboundTags{VPN: "proxy", Direct: "broray-direct", Deny: "broray-deny"}, []string{"socks"})
	return err
}

func (e *engine) stageBackupPolicy(ctx context.Context, in *policyBackup, st *compat.BackupStage, plan *compat.BackupPlan) error {
	if in == nil {
		return nil
	}
	if !compat.FenceHeld(ctx) {
		return backupInvalid("для восстановления правил требуется блокировка операции")
	}
	pending, err := routepolicy.RecoveryPending(ctx, e.d.Root)
	if err != nil {
		return err
	}
	if pending {
		return backupInvalid("восстановление правил ещё не завершено")
	}
	current, err := e.backupPolicy(ctx)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(current, in) {
		return nil
	}
	owned, err := nativeDomainRecordIDs(e.root)
	if err != nil {
		return err
	}
	if len(owned) > 0 {
		previous := make(map[string]routepolicy.Rule, len(current.State.Rules))
		restored := make(map[string]routepolicy.Rule, len(in.State.Rules))
		for _, rule := range current.State.Rules {
			previous[rule.ID] = rule
		}
		for _, rule := range in.State.Rules {
			restored[rule.ID] = rule
		}
		for _, id := range owned {
			if _, preset := nativeDomainPresetByID(id); !preset && !reflect.DeepEqual(previous[id], restored[id]) {
				return backupInvalid("сначала удалите из Keenetic изменяемые пользовательские доменные маршруты")
			}
		}
	}
	next := in.State
	if err = st.Take(compat.DataRel("routes", routepolicy.StateFile)); err != nil {
		return err
	}
	if err = st.Take(compat.DataRel("routes", routepolicy.GeoScheduleFile)); err != nil {
		return err
	}
	if err = platform.MkdirAllDurable(st.St.Root.Routes(), 0700); err != nil {
		return err
	}
	if err = platform.MkdirAllDurable(st.St.Root.Runtime(), 0700); err != nil {
		return err
	}
	hasArchive := !bytes.Equal(bytes.TrimSpace(in.Geodata), []byte("null"))
	var snapshot geodata.Snapshot
	var stagedStore *geodata.Store
	archive := in.Geodata
	if !hasArchive && current.State.GeoRevision != "" {
		// An empty imported policy clears the pointer through a fresh empty
		// generation, retaining the old generation for undo and running readers.
		archive = []byte(`{"schemaVersion":1,"snapshot":{"revision":"","previousRevision":"","generation":"","selection":{"countryCodes":null,"geoSiteTags":null},"assets":null},"files":{}}`)
		hasArchive = true
	}
	if hasArchive {
		if err = st.Take("runtime/" + geodata.AssetDirectory); err != nil {
			return err
		}
		se := newEngine(st.St)
		store, err := se.openManagedGeodata()
		if err != nil {
			return err
		}
		stagedStore = store
		defer store.Close()
		previous, err := store.Load(ctx)
		if err != nil {
			return err
		}
		var archived struct {
			Snapshot geodata.Snapshot `json:"snapshot"`
		}
		if err = json.Unmarshal(archive, &archived); err != nil {
			return err
		}
		if archived.Snapshot.Revision != next.GeoRevision {
			return backupInvalid("ревизия архива геоданных не соответствует правилам")
		}
		validate := func(ctx context.Context, snap geodata.Snapshot) error {
			if err := checkBackupPolicy(next, snap); err != nil {
				return err
			}
			// Test all archived tags, including those selected by disabled rules.
			state := routepolicy.State{SchemaVersion: 1, Rules: []routepolicy.Rule{{ID: "backup-validation", Enabled: true, Action: routepolicy.Deny, CountryCodes: snap.Selection.CountryCodes, GeoSiteTags: snap.Selection.GeoSiteTags}}}
			if len(snap.Assets) == 0 {
				state.Rules = nil
			}
			return e.validateBackupPolicy(ctx, state, snap, st.St.Root.Runtime())
		}
		candidate, err := store.PrepareArchive(ctx, archive, validate)
		if err != nil {
			return err
		}
		defer store.Abort(context.WithoutCancel(ctx), candidate)
		snapshot, err = store.Commit(ctx, candidate, previous.Revision)
		if err != nil {
			return err
		}
		next.GeoRevision = snapshot.Revision
	} else if next.GeoRevision != "" {
		return backupInvalid("в архиве отсутствуют геоданные правил")
	}
	if err = checkBackupPolicy(next, snapshot); err != nil {
		return err
	}
	if len(next.Rules) > 0 {
		if err = e.validateBackupPolicy(ctx, next, snapshot, st.St.Root.Runtime()); err != nil {
			return err
		}
	}
	base, err := e.d.Svc.PreviewBackupBaseConfig(ctx, st)
	if err != nil {
		return err
	}
	if base != nil {
		cfg, err := e.renderRouting(base, next, snapshot)
		if err != nil {
			return err
		}
		dir, err := platform.OpenDirNoFollow(st.St.Root.Runtime())
		if err != nil {
			return err
		}
		err = e.d.Svc.ValidateXrayStagedConfig(ctx, cfg, dir)
		dir.Close()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(st.St.Root.Dir, st.St.Root.XrayConfig())
		if err != nil {
			return err
		}
		if err = st.Take(filepath.ToSlash(rel)); err != nil {
			return err
		}
		if e.d.Svc.IsXrayRunning() {
			if info, err := os.Lstat(e.d.Root.XrayConfig()); err != nil || !info.Mode().IsRegular() {
				return backupInvalid("исходная конфигурация работающего Xray недоступна")
			}
		}
		if err = platform.MkdirAllDurable(filepath.Dir(st.St.Root.XrayConfig()), 0700); err != nil {
			return err
		}
		if err = platform.WriteFileAtomicNoFollow(st.St.Root.XrayConfig(), cfg, 0600); err != nil {
			return err
		}
	}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if _, err = routepolicy.DecodeState(data); err != nil {
		return err
	}
	if err = platform.WriteFileAtomicNoFollow(filepath.Join(st.St.Root.Routes(), routepolicy.StateFile), data, 0600); err != nil {
		return err
	}
	schedule, err := routepolicy.ReadGeoSchedule(ctx, st.St.Root.Routes())
	if err != nil {
		return err
	}
	if _, err = routepolicy.SaveGeoSchedule(ctx, st.St.Root.Routes(), routepolicy.GeoScheduleRevision(schedule), in.AutoUpdate); err != nil {
		return err
	}
	if stagedStore != nil {
		// Later config checks must not invalidate PrepareArchive's earlier proof.
		// Load retains the original root identity and verifies every payload again.
		verified, err := stagedStore.Load(ctx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(verified, snapshot) {
			return backupInvalid("подготовленные геоданные изменились")
		}
	}
	plan.Add("setting", "routing-policy", "Правила маршрутизации и геоданные", "replace", "")
	return nil
}

func (e *engine) validateBackupPolicy(ctx context.Context, state routepolicy.State, snapshot geodata.Snapshot, assetsPath string) error {
	base := []byte(`{"inbounds":[{"tag":"socks","protocol":"socks","listen":"127.0.0.1","port":1080}],"outbounds":[{"tag":"proxy","protocol":"blackhole"}]}`)
	cfg, err := e.renderRouting(base, state, snapshot)
	if err != nil {
		return err
	}
	dir, err := platform.OpenDirNoFollow(assetsPath)
	if err != nil {
		return err
	}
	defer dir.Close()
	return e.d.Svc.ValidateXrayStagedConfig(ctx, cfg, dir)
}

func (e *engine) backupPolicyAfter(ctx context.Context, before any) compat.BackupPlan {
	var plan compat.BackupPlan
	if !compat.BackupOwnsXrayConfig(ctx) {
		return plan
	}
	running, _ := before.(bool)
	if !running {
		plan.Notes = append(plan.Notes, "Правила и конфигурация восстановлены. Xray остаётся остановленным.")
		return plan
	}
	if err := e.d.Svc.ReapplyActiveServer(ctx, "backup-policy"); err != nil {
		e.log.Printf("routes: backup policy runtime reapply failed: %v", err)
		plan.FollowUps = append(plan.FollowUps, "Настройки восстановлены, но подключение с восстановленными правилами не подтверждено. Переподключите активный сервер на странице «Серверы».")
		return plan
	}
	plan.Notes = append(plan.Notes, "Восстановленные правила применены к активному серверу.")
	return plan
}
