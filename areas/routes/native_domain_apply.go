package routes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/routepolicy"
)

type nativeDomainRecord struct {
	Group       string                       `json:"group"`
	Description string                       `json:"description"`
	Interface   string                       `json:"interface"`
	Phase       string                       `json:"phase"`
	Preview     nativeDomainPreview          `json:"preview"`
	Binding     *keenetic.DomainRouteBinding `json:"binding,omitempty"`
}

func nativeDomainRecordPath(root, id string) string {
	return filepath.Join(root, "native-domain-"+id+".json")
}

func nativeDomainRecordIDs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		name, ok := strings.CutPrefix(entry.Name(), "native-domain-")
		if !ok {
			continue
		}
		id, ok := strings.CutSuffix(name, ".json")
		if ok && nativeOwnID.MatchString(id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func nativeOwnDescription(name string) string {
	description := []rune("BROray - " + strings.ReplaceAll(name, "\"", "'"))
	if len(description) > 128 {
		description = description[:128]
	}
	return string(description)
}

func readNativeDomainRecord(path, id string) (*nativeDomainRecord, error) {
	raw, err := platform.ReadRegularNoFollow(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record nativeDomainRecord
	if len(raw) > 4<<20 || json.Unmarshal(raw, &record) != nil || record.Preview.ID != id ||
		len(record.Preview.Domains) == 0 || record.Group == "" || !keenetic.ValidProxyName(record.Interface) ||
		(record.Phase != "creating" && record.Phase != "enabled" && record.Phase != "removing") ||
		(record.Phase != "creating" && record.Binding == nil) {
		return nil, errors.New("invalid native domain operation record")
	}
	return &record, nil
}

func saveNativeDomainRecord(path string, v *nativeDomainRecord) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return cgi.E(400, "NATIVE_DOMAIN_TOO_LARGE", "Список слишком велик для сохранения и безопасного отката.")
	}
	return platform.WriteFileAtomicNoFollow(path, data, 0600)
}

func (e *engine) nativeDomainRCI() *keenetic.RCI {
	switch t := e.T.(type) {
	case *keenetic.RCI:
		return t
	case *keenetic.Auto:
		r, _ := t.Primary.(*keenetic.RCI)
		return r
	}
	return nil
}

func hNativeDomainApply(e *engine, c *cgi.Req) (result any, resultErr error) {
	if c.R.Method == http.MethodGet {
		states := make(map[string]any)
		var snap keenetic.DomainRouteSnapshot
		var attempted, observed bool
		ids, err := nativeDomainRecordIDs(e.root)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			record, err := readNativeDomainRecord(nativeDomainRecordPath(e.root, id), id)
			if err != nil {
				return nil, err
			}
			if record != nil {
				phase := record.Phase
				if phase == "enabled" {
					if !attempted {
						attempted = true
						rc := e.nativeDomainRCI()
						if rc != nil {
							snap, err = rc.ReadDomainRoutes(c.R.Context())
							observed = err == nil
						}
					}
					if !observed || !snap.ConfirmsDomainRoute(record.Description, record.Preview.Domains, *record.Binding) {
						phase = "unconfirmed"
					}
				}
				states[id] = map[string]any{"phase": phase, "preview": record.Preview}
			}
		}
		drafts, err := e.dnsDrafts(c.R.Context())
		if err != nil {
			return nil, err
		}
		return map[string]any{"states": states, "drafts": drafts}, nil
	}
	if err := requestGuard(c); err != nil {
		return nil, err
	}
	value, err := strictjson.Decode(c.Body())
	fields, ok := value.(map[string]any)
	if err != nil || !ok {
		return nil, cgi.E(400, "NATIVE_DOMAIN_REQUEST_INVALID", "Проверьте параметры доменного списка.")
	}
	id, _ := fields["id"].(string)
	action, _ := fields["action"].(string)
	preset, isPreset := nativeDomainPresetByID(id)
	if !nativeOwnID.MatchString(id) || (action != "enable" && action != "disable" && action != "recover") ||
		(action == "enable" && len(fields) != 4) || (action != "enable" && len(fields) != 2) {
		return nil, cgi.E(400, "NATIVE_DOMAIN_REQUEST_INVALID", "Проверьте параметры доменного списка.")
	}
	rc := e.nativeDomainRCI()
	if rc == nil {
		return nil, cgi.E(501, "NATIVE_DOMAIN_RCI_REQUIRED", "Для штатных списков требуется подключение RCI к Keenetic.")
	}
	ctx, cancel := e.jobCtx()
	defer cancel()
	revision, _ := fields["sourceRevision"].(string)
	hash, _ := fields["sourceSHA256"].(string)
	var preview nativeDomainPreview
	if action == "enable" && isPreset {
		preview, err = e.nativePresetPreview(ctx, id, revision)
		if err != nil {
			return nil, err
		}
		if len(preview.Domains) == 0 || preview.Hash != hash {
			return nil, cgi.E(409, "NATIVE_DOMAIN_PREVIEW_CHANGED", "Состав списка изменился. Повторите предпросмотр.")
		}
	}
	release, ok := e.d.Lock.TryAcquire("routes", "routes:native-domain", id)
	if !ok {
		return nil, cgi.E(409, "POLICY_BUSY", "Дождитесь завершения текущей операции.")
	}
	defer release()
	ctx = compat.WithFence(ctx)
	for _, list := range []premiumListSpec{premiumDomainList, premiumIPList} {
		if id == list.id {
			if _, err := os.Lstat(e.p(list.pending)); !errors.Is(err, os.ErrNotExist) {
				return nil, cgi.E(409, "PREMIUM_RECOVERY_REQUIRED", "Сначала восстановите список через раздел Premium.")
			}
		}
	}
	op := e.d.Queue.StartOperation("", "route_policy", "routes:native-domain", "USER", "protected", "")
	defer func() { op.Finish(resultErr) }()
	path := nativeDomainRecordPath(e.root, id)
	record, err := readNativeDomainRecord(path, id)
	if err != nil {
		return nil, err
	}
	if action == "enable" {
		if record != nil {
			return nil, cgi.E(409, "NATIVE_DOMAIN_ALREADY_TRACKED", "Список уже включён или требует восстановления.")
		}
		description := "BROray - " + preset.Name
		if !isPreset {
			pending, err := routepolicy.RecoveryPending(ctx, e.d.Root)
			if err != nil || pending {
				return nil, e.policyUncertain(errors.Join(errors.New("previous policy transaction requires recovery"), err))
			}
			var legacyEnabled bool
			preview, legacyEnabled, err = e.nativeOwnPreview(ctx, id)
			if err != nil {
				return nil, err
			}
			if legacyEnabled {
				return nil, cgi.E(409, "NATIVE_DOMAIN_LEGACY_ACTIVE", "Сначала отключите прежний маршрут этого списка в BROray.")
			}
			if len(preview.Domains) == 0 || revision != preview.Revision || hash != preview.Hash {
				return nil, cgi.E(409, "NATIVE_DOMAIN_PREVIEW_CHANGED", "Состав списка изменился. Повторите предпросмотр.")
			}
			description = nativeOwnDescription(preview.Name)
		}
		iface := e.managedInterface()
		target, err := e.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, e.interfaceDisplay())
		if err != nil || target.Name != iface || !keenetic.ValidProxyName(iface) {
			return nil, cgi.E(409, "NATIVE_DOMAIN_INTERFACE_UNCONFIRMED", "Не удалось подтвердить принадлежность интерфейса BROray.")
		}
		var key [10]byte
		if _, err := rand.Read(key[:]); err != nil {
			return nil, err
		}
		group := "broray-" + hex.EncodeToString(key[:])
		before, err := rc.ReadDomainRoutes(ctx)
		if err != nil {
			return nil, err
		}
		if _, exists := before.Lists[group]; exists {
			return nil, errors.New("generated domain group already exists")
		}
		for _, b := range before.Bindings {
			if b.Group == group {
				return nil, errors.New("generated domain group already has a binding")
			}
		}
		record = &nativeDomainRecord{Group: group, Description: description, Interface: iface, Phase: "creating", Preview: preview}
		if err := saveNativeDomainRecord(path, record); err != nil {
			return nil, err
		}
		op.Phase("applying")
		binding, err := rc.ChangeDomainRoute(ctx, group, record.Description, iface, preview.Domains, nil)
		if err == nil {
			after, verifyErr := e.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, e.interfaceDisplay())
			if verifyErr != nil || after != target {
				err = errors.New("owned Keenetic interface changed during domain route installation")
			}
		}
		if err != nil {
			recovery, stop := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
			defer stop()
			if rollback := rc.RemoveJournaledDomainRoute(recovery, group, record.Description, iface, preview.Domains, nil); rollback != nil {
				return nil, errors.Join(err, rollback)
			}
			return nil, errors.Join(err, platform.RemoveRegularNoFollow(path))
		}
		record.Binding, record.Phase = &binding, "enabled"
		if err := saveNativeDomainRecord(path, record); err != nil {
			return nil, err
		}
		return map[string]any{"phase": "enabled", "preview": preview}, nil
	}
	if record == nil {
		return map[string]any{"phase": "disabled"}, nil
	}
	if action == "disable" && record.Phase != "enabled" || action == "recover" && record.Phase == "enabled" {
		return nil, cgi.E(409, "NATIVE_DOMAIN_PHASE", "Обновите состояние списка перед изменением.")
	}
	if action == "recover" && record.Phase == "creating" {
		target, err := e.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, e.interfaceDisplay())
		if err != nil || target.Name != record.Interface || record.Interface != e.managedInterface() {
			return nil, cgi.E(409, "NATIVE_DOMAIN_INTERFACE_UNCONFIRMED", "Не удалось подтвердить принадлежность интерфейса BROray.")
		}
		op.Phase("applying")
		binding, err := rc.CompleteJournaledDomainRoute(ctx, record.Group, record.Description, record.Interface, record.Preview.Domains)
		if err != nil {
			return nil, err
		}
		after, err := e.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, e.interfaceDisplay())
		if err != nil || after != target {
			return nil, cgi.E(409, "NATIVE_DOMAIN_INTERFACE_UNCONFIRMED", "При завершении операции изменился интерфейс BROray.")
		}
		record.Binding, record.Phase = &binding, "enabled"
		if err := saveNativeDomainRecord(path, record); err != nil {
			return nil, err
		}
		return map[string]any{"phase": "enabled", "preview": record.Preview}, nil
	}
	if action == "disable" {
		record.Phase = "removing"
		if err := saveNativeDomainRecord(path, record); err != nil {
			return nil, err
		}
	}
	op.Phase("recovering")
	if err := rc.RemoveJournaledDomainRoute(ctx, record.Group, record.Description, record.Interface, record.Preview.Domains, record.Binding); err != nil {
		return nil, err
	}
	if err := platform.RemoveRegularNoFollow(path); err != nil {
		return nil, err
	}
	return map[string]any{"phase": "disabled"}, nil
}
