package routes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/routepolicy"
)

type premiumListChange struct {
	Before *nativeDomainRecord `json:"before,omitempty"`
	Next   nativeDomainRecord  `json:"next"`
}

type premiumListSpec struct {
	id, name, pending, operation string
	isIP                         bool
}

var premiumDomainList = premiumListSpec{premiumID, premiumName, "premium-domain-pending.json", "routes:premium-domain", false}
var premiumIPList = premiumListSpec{premiumIPID, premiumIPName, "premium-ip-pending.json", "routes:premium-ip", true}

func (e *engine) premiumChangePath() string {
	return e.p(premiumDomainList.pending)
}

func (e *engine) addPremiumDomain(user, token string) (any, error) {
	return e.addPremiumTarget(user, token, premiumDomainList)
}

func (e *engine) addPremiumTarget(user, token string, list premiumListSpec) (result any, resultErr error) {
	stale := cgi.E(409, "PREMIUM_CHECK_EXPIRED", "Повторите проверку перед добавлением.")
	release, ok := e.d.Lock.TryAcquire("routes", list.operation, list.id)
	if !ok {
		return nil, cgi.E(409, "POLICY_BUSY", "Дождитесь завершения текущей операции.")
	}
	defer release()
	proof, exists := e.premiumProofs[token]
	identity := e.d.Svc.ServersPremiumVPNIdentity
	if !exists || proof.isIP != list.isIP || proof.user != user || !proof.expires.After(time.Now()) || identity == nil || identity() != proof.identity {
		return nil, stale
	}
	delete(e.premiumProofs, token)
	ctx, cancel := e.jobCtx()
	defer cancel()
	ctx = compat.WithFence(ctx)
	var network string
	if list.isIP {
		addr, valid := premiumIP(proof.domain)
		if !valid {
			return nil, stale
		}
		network = premiumIPNetwork(addr)
	} else {
		_, network = premiumAddresses(ctx, proof.domain)
	}
	if network != "" || identity() != proof.identity {
		return nil, stale
	}
	rc := e.nativeDomainRCI()
	if rc == nil {
		return nil, cgi.E(409, "PREMIUM_ROUTER_UNAVAILABLE", "Маршрутизация по доменам Keenetic недоступна.")
	}
	target, err := e.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, e.interfaceDisplay())
	if err != nil || target.Name != e.managedInterface() || !keenetic.ValidProxyName(target.Name) {
		return nil, cgi.E(409, "NATIVE_DOMAIN_INTERFACE_UNCONFIRMED", "Не удалось подтвердить интерфейс BROray.")
	}
	op := e.d.Queue.StartOperation("", "route_policy", list.operation, "USER", "protected", "")
	defer func() { op.Finish(resultErr) }()
	op.Phase("applying")
	verify := func(ctx context.Context) bool {
		after, err := e.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, e.interfaceDisplay())
		return err == nil && after == target && identity() == proof.identity
	}
	applied, duplicate, err := e.applyPremiumTargetList(ctx, rc, target.Name, proof.domain, verify, list)
	if err != nil {
		// Keep domain names and device responses out of persistent operation logs.
		if errors.Is(err, errPremiumUncertain) {
			return nil, cgi.E(409, "PREMIUM_RECOVERY_REQUIRED", "Восстановите предыдущий список перед добавлением.")
		}
		if errors.Is(err, errPremiumIPConflict) {
			return nil, cgi.E(409, "PREMIUM_IP_ROUTE_CONFLICT", "Для этого IP уже действует маршрут Keenetic с другим назначением.")
		}
		return nil, cgi.E(409, "PREMIUM_APPLY_FAILED", "Запись не добавлена. Проверьте состояние списка и повторите проверку.")
	}
	return map[string]any{"domain": proof.domain, "list": list.name, "count": len(applied.Preview.Domains), "alreadyAdded": duplicate}, nil
}

var errPremiumUncertain = errors.New("premium domain rollback unconfirmed")
var errPremiumIPConflict = errors.New("conflicting active IP route")

func (e *engine) recoverPremium() (any, error) {
	return e.recoverPremiumTarget(premiumDomainList)
}

func (e *engine) recoverPremiumTarget(list premiumListSpec) (result any, resultErr error) {
	release, ok := e.d.Lock.TryAcquire("routes", list.operation, list.id)
	if !ok {
		return nil, cgi.E(409, "POLICY_BUSY", "Дождитесь завершения текущей операции.")
	}
	defer release()
	ctx, cancel := e.jobCtx()
	defer cancel()
	ctx = compat.WithFence(ctx)
	rc := e.nativeDomainRCI()
	target, err := e.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, e.interfaceDisplay())
	if rc == nil || err != nil || target.Name != e.managedInterface() || !keenetic.ValidProxyName(target.Name) {
		return nil, cgi.E(409, "NATIVE_DOMAIN_INTERFACE_UNCONFIRMED", "Не удалось подтвердить интерфейс BROray.")
	}
	op := e.d.Queue.StartOperation("", "route_policy", list.operation, "USER", "protected", "")
	defer func() { op.Finish(resultErr) }()
	op.Phase("recovering")
	if err := e.finishPremiumRecovery(ctx, rc, target.Name, list); err != nil {
		return nil, cgi.E(409, "PREMIUM_RECOVERY_REQUIRED", "Предыдущий список не удалось подтвердить. Проверьте состояние маршрутов Keenetic.")
	}
	return map[string]bool{"recoveryRequired": false}, nil
}

func (e *engine) finishPremiumRecovery(ctx context.Context, rc *keenetic.RCI, iface string, list premiumListSpec) error {
	raw, err := platform.ReadRegularNoFollow(e.p(list.pending))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var pending premiumListChange
	if err != nil || len(raw) > 1<<20 || json.Unmarshal(raw, &pending) != nil || !validPremiumChange(pending, iface, list) {
		return errPremiumUncertain
	}
	return e.restorePremiumList(ctx, rc, nativeDomainRecordPath(e.root, list.id), pending, list)
}

func (e *engine) applyPremiumTargetList(ctx context.Context, rc *keenetic.RCI, iface, domain string, verify func(context.Context) bool, list premiumListSpec) (nativeDomainRecord, bool, error) {
	var empty nativeDomainRecord
	path := nativeDomainRecordPath(e.root, list.id)
	pendingPath := e.p(list.pending)
	if err := e.finishPremiumRecovery(ctx, rc, iface, list); err != nil {
		return empty, false, errPremiumUncertain
	}
	state, err := routepolicy.ReadState(ctx, e.root)
	if err != nil {
		return empty, false, err
	}
	for _, rule := range state.Rules {
		if rule.ID == list.id || strings.EqualFold(rule.Name, list.name) {
			return empty, false, errors.New("premium list name conflict")
		}
	}
	before, err := readNativeDomainRecord(path, list.id)
	if err != nil {
		return empty, false, err
	}
	var key [10]byte
	if _, err := rand.Read(key[:]); err != nil {
		return empty, false, err
	}
	next := nativeDomainRecord{Group: "broray-" + hex.EncodeToString(key[:]), Interface: iface,
		Description: nativeOwnDescription(list.name), Phase: "enabled",
		Preview: nativeDomainPreview{ID: list.id, Name: list.name, Revision: "own",
			nativeDomainContent: nativeDomainContent{Skipped: []skippedDomainMatcher{}}}}
	snapshot, err := rc.ReadDomainRoutes(ctx)
	if err != nil {
		return empty, false, err
	}
	if list.isIP {
		addr, valid := premiumIP(domain)
		if !valid || addr.String() != domain {
			return empty, false, errors.New("invalid numeric premium destination")
		}
		if premiumIPConflict(snapshot, iface, addr) {
			return empty, false, errPremiumIPConflict
		}
	}
	if before != nil {
		if !validPremiumRecord(*before, iface, list) ||
			!snapshot.ConfirmsDomainRoute(before.Description, before.Preview.Domains, *before.Binding) {
			return empty, false, errPremiumUncertain
		}
		if slices.Contains(before.Preview.Domains, domain) {
			return *before, true, nil
		}
		if len(before.Preview.Domains) >= 256 {
			return empty, false, errors.New("premium list too large")
		}
		next.Preview.Domains = slices.Clone(before.Preview.Domains)
	}
	if _, occupied := snapshot.Lists[next.Group]; occupied {
		return empty, false, errors.New("premium domain group already exists")
	}
	for _, binding := range snapshot.Bindings {
		if binding.Group == next.Group {
			return empty, false, errors.New("premium domain binding already exists")
		}
	}
	next.Preview.Domains = append(next.Preview.Domains, domain)
	slices.Sort(next.Preview.Domains)
	next.Preview.Hash = premiumListHash(next.Preview.Domains)
	pending := premiumListChange{Before: before, Next: next}
	data, err := json.Marshal(pending)
	if err != nil {
		return empty, false, err
	}
	if err := platform.WriteFileAtomicNoFollow(pendingPath, data, 0600); err != nil {
		return empty, false, err
	}
	abort := func() (nativeDomainRecord, bool, error) {
		recovery, stop := context.WithTimeout(context.WithoutCancel(ctx), 65*time.Second)
		defer stop()
		if err := e.restorePremiumList(recovery, rc, path, pending, list); err != nil {
			return empty, false, errPremiumUncertain
		}
		return empty, false, errors.New("premium list change rolled back")
	}
	binding, err := rc.ChangeDomainRoute(ctx, next.Group, next.Description, iface, next.Preview.Domains, nil)
	if err != nil || !verify(ctx) {
		return abort()
	}
	next.Binding = &binding
	// The current domains keep an active VPN route while its superset is
	// installed. If the router rejects overlap, the previous list stays live.
	if before != nil {
		if err := rc.RemoveJournaledDomainRoute(ctx, before.Group, before.Description, iface, before.Preview.Domains, before.Binding); err != nil {
			return abort()
		}
		if !verify(ctx) {
			return abort()
		}
	}
	if err := saveNativeDomainRecord(path, &next); err != nil {
		return abort()
	}
	if err := platform.RemoveRegularNoFollow(pendingPath); err != nil {
		return empty, false, errPremiumUncertain
	}
	return next, false, nil
}

func premiumListHash(domains []string) string {
	sum := sha256.Sum256([]byte(strings.Join(domains, "\n")))
	return hex.EncodeToString(sum[:])
}

func validPremiumRecord(record nativeDomainRecord, iface string, list premiumListSpec) bool {
	return record.Binding != nil && record.Phase == "enabled" && record.Interface == iface &&
		record.Preview.ID == list.id && record.Preview.Name == list.name &&
		record.Description == nativeOwnDescription(list.name) && record.Preview.Revision == "own" &&
		len(record.Preview.Domains) > 0 && len(record.Preview.Domains) <= 256 &&
		record.Preview.Hash == premiumListHash(record.Preview.Domains)
}

func validPremiumChange(change premiumListChange, iface string, list premiumListSpec) bool {
	next := change.Next
	next.Binding = &keenetic.DomainRouteBinding{}
	if !validPremiumRecord(next, iface, list) {
		return false
	}
	if change.Before == nil {
		return len(next.Preview.Domains) == 1
	}
	before := *change.Before
	if !validPremiumRecord(before, iface, list) || before.Group == next.Group ||
		len(before.Preview.Domains)+1 != len(next.Preview.Domains) {
		return false
	}
	for _, domain := range before.Preview.Domains {
		if !slices.Contains(next.Preview.Domains, domain) {
			return false
		}
	}
	return true
}

// Restore the last confirmed list. A router object with a third image is
// rejected by the ownership checks in the native domain route helpers.
func (e *engine) restorePremiumList(ctx context.Context, rc *keenetic.RCI, path string, change premiumListChange, list premiumListSpec) error {
	before, next := change.Before, change.Next
	snapshot, err := rc.ReadDomainRoutes(ctx)
	if err != nil {
		return err
	}
	if before != nil && !snapshot.ConfirmsDomainRoute(before.Description, before.Preview.Domains, *before.Binding) {
		binding, err := rc.CompleteJournaledDomainRoute(ctx, before.Group, before.Description, before.Interface, before.Preview.Domains)
		if err != nil {
			return err
		}
		before.Binding = &binding
	}
	if err := rc.RemoveJournaledDomainRoute(ctx, next.Group, next.Description, next.Interface, next.Preview.Domains, nil); err != nil {
		return err
	}
	if before == nil {
		if err := platform.RemoveRegularNoFollow(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if err := saveNativeDomainRecord(path, before); err != nil {
		return err
	}
	return platform.RemoveRegularNoFollow(e.p(list.pending))
}
