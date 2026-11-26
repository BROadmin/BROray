package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/routepolicy"
)

const dnsDraftPrefix = "native-dns-draft-"

func (e *engine) dnsDraftPath(id string) string {
	return e.p(dnsDraftPrefix + id + ".json")
}

func readDNSDraft(path, id string) (*nativeDomainPreview, error) {
	data, err := platform.ReadRegularNoFollow(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var draft nativeDomainPreview
	if len(data) > 2<<20 || json.Unmarshal(data, &draft) != nil || draft.ID != id ||
		draft.Revision != "own" || len(draft.Domains) == 0 || draft.Hash != dnsDraftHash(draft.Name, draft.Domains) {
		return nil, errors.New("invalid saved DNS list")
	}
	return &draft, nil
}

func dnsDraftHash(name string, targets []string) string {
	return premiumListHash(append([]string{name}, targets...))
}

func parseDNSDraft(text string) ([]string, error) {
	var targets []string
	for index, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		target, valid := keenetic.DNSRouteTarget(strings.TrimPrefix(line, "domain:"))
		if !valid {
			return nil, cgi.E(400, "NATIVE_DNS_TARGET_INVALID", fmt.Sprintf("Строка %d. Укажите домен, IPv4 или IPv6.", index+1))
		}
		targets = append(targets, target)
	}
	slices.Sort(targets)
	targets = slices.Compact(targets)
	if len(targets) == 0 || len(targets) > 4096 {
		return nil, cgi.E(400, "NATIVE_DNS_SIZE_INVALID", "В списке должно быть от 1 до 4096 записей.")
	}
	return targets, nil
}

func hNativeDNSDraft(e *engine, c *cgi.Req) (any, error) {
	if err := requestGuard(c); err != nil {
		return nil, err
	}
	raw, err := strictjson.Decode(c.Body())
	fields, ok := raw.(map[string]any)
	if err != nil || !ok {
		return nil, cgi.E(400, "NATIVE_DNS_DRAFT_INVALID", "Проверьте данные списка.")
	}
	id, _ := fields["id"].(string)
	action, _ := fields["action"].(string)
	expected, expectedOK := fields["expectedHash"].(string)
	revision, revisionOK := fields["policyRevision"].(string)
	if !nativeOwnID.MatchString(id) || id == premiumID || id == premiumIPID ||
		!expectedOK || !revisionOK || !strings.HasPrefix(id, "own-") ||
		(action != "save" && action != "delete") ||
		(action == "save" && len(fields) != 6) || (action == "delete" && len(fields) != 4) {
		return nil, cgi.E(400, "NATIVE_DNS_DRAFT_INVALID", "Проверьте данные списка.")
	}
	release, ok := e.d.Lock.TryAcquire("routes", "routes:native-dns-draft", id)
	if !ok {
		return nil, cgi.E(409, "POLICY_BUSY", "Дождитесь завершения текущей операции.")
	}
	defer release()
	ctx := compat.WithFence(c.R.Context())
	pending, err := routepolicy.RecoveryPending(ctx, e.d.Root)
	if pending || err != nil {
		return nil, e.policyUncertain(errors.Join(errors.New("previous policy transaction requires recovery"), err))
	}
	state, err := routepolicy.ReadState(ctx, e.root)
	if err != nil {
		return nil, err
	}
	actual, err := routepolicy.StateRevision(state)
	if err != nil {
		return nil, err
	}
	for _, rule := range state.Rules {
		if rule.ID == id && (rule.Enabled || len(rule.Domains) == 0 || len(rule.GeoSiteTags) != 0 || len(rule.CountryCodes) != 0) {
			return nil, cgi.E(409, "NATIVE_DOMAIN_LEGACY_ACTIVE", "Перед изменением отключите прежний маршрут списка.")
		}
	}
	path := e.dnsDraftPath(id)
	draft, err := readDNSDraft(path, id)
	if err != nil {
		return nil, err
	}
	if actual != revision || (draft == nil && expected != "") || (draft != nil && draft.Hash != expected) {
		return nil, cgi.E(409, "NATIVE_DNS_DRAFT_CHANGED", "Список изменился. Обновите страницу и повторите действие.")
	}
	record, err := readNativeDomainRecord(nativeDomainRecordPath(e.root, id), id)
	if err != nil {
		return nil, err
	}
	if record != nil {
		return nil, cgi.E(409, "NATIVE_DOMAIN_IN_USE", "Сначала отключите список в Keenetic.")
	}
	if action == "delete" {
		if draft == nil {
			return nil, cgi.E(409, "NATIVE_DNS_DRAFT_CHANGED", "Список уже удалён.")
		}
		return map[string]bool{"deleted": true}, platform.RemoveRegularNoFollow(path)
	}
	name, _ := fields["name"].(string)
	content, contentOK := fields["targets"].(string)
	name = strings.TrimSpace(name)
	if !contentOK || name == "" || len([]rune(name)) > 128 || strings.IndexFunc(name, unicode.IsControl) >= 0 ||
		strings.EqualFold(name, premiumName) || strings.EqualFold(name, premiumIPName) {
		return nil, cgi.E(400, "NATIVE_DNS_DRAFT_INVALID", "Укажите название и содержимое списка. Названия Premium зарезервированы.")
	}
	targets, err := parseDNSDraft(content)
	if err != nil {
		return nil, err
	}
	next := nativeDomainPreview{ID: id, Name: name, Revision: "own", Hash: dnsDraftHash(name, targets),
		nativeDomainContent: nativeDomainContent{Domains: targets, Skipped: []skippedDomainMatcher{}}}
	data, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	return next, platform.WriteFileAtomicNoFollow(path, data, 0600)
}

func (e *engine) dnsDrafts(ctx context.Context) (map[string]*nativeDomainPreview, error) {
	entries, err := os.ReadDir(e.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	drafts := make(map[string]*nativeDomainPreview)
	for _, entry := range entries {
		id, found := strings.CutPrefix(entry.Name(), dnsDraftPrefix)
		if !found {
			continue
		}
		id, found = strings.CutSuffix(id, ".json")
		if !found || !nativeOwnID.MatchString(id) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		draft, err := readDNSDraft(e.dnsDraftPath(id), id)
		if err != nil {
			return nil, err
		}
		if draft != nil {
			drafts[id] = draft
		}
	}
	return drafts, nil
}
