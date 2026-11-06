package keenetic

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

// Write policy constants (lib/keenetic-write-policy.sh): the staged policy
// is enabled in this build.
const (
	writePolicySHA256 = "dc8501856b27260738828e9959e10512496410ff44adfceaa8564056bed1ddfa"
	ownerContract     = "r14c38-proxy-owned-interface/1"
)

var acceptedContracts = map[string]bool{
	"r14c34-proxy-owned-interface/1": true, "r14c35-proxy-owned-interface/1": true, "r14c36-proxy-owned-interface/1": true,
	"r14c37-proxy-owned-interface/1": true, "r14c38-proxy-owned-interface/1": true,
}

// receipt is config/interface.json (schema 2, or legacy schema 1).
type receipt struct {
	SchemaVersion int    `json:"schemaVersion"`
	Contract      string `json:"contract,omitempty"`
	Owner         string `json:"owner"`
	InterfaceName string `json:"interfaceName"`
	Protocol      string `json:"protocol"`
	Upstream      struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	} `json:"upstream"`
	Description         string `json:"description,omitempty"`
	SelectionMode       string `json:"selectionMode,omitempty"`
	WriteProtocolSHA256 string `json:"writeProtocolSha256,omitempty"`
	RunningBlockSHA256  string `json:"runningBlockSha256,omitempty"`
	StartupBlockSHA256  string `json:"startupBlockSha256,omitempty"`
	UpdatedAt           string `json:"updatedAt,omitempty"`
}

func (s *service) receiptPath() string { return s.d.Path("config", "interface.json") }

// loadReceipt reads the receipt file (regular, not a symlink).
func (s *service) loadReceipt() (receipt, bool) {
	var r receipt
	st, err := os.Lstat(s.receiptPath())
	if err != nil || !st.Mode().IsRegular() {
		return r, false
	}
	b, err := os.ReadFile(s.receiptPath())
	if err != nil || json.Unmarshal(b, &r) != nil {
		return receipt{}, false
	}
	return r, true
}

// receiptPresent is ownershipReceiptPresent (keenetic-page.sh:870-881).
func (s *service) receiptPresent(name string) bool {
	r, ok := s.loadReceipt()
	return ok && (r.SchemaVersion == 1 || r.SchemaVersion == 2) && r.Owner == "BROray" && r.InterfaceName == name
}

// snapshots holds one running/startup capture for several checks.
type snapshots struct {
	running, startup []string
	err              error
}

func (s *service) capture(ctx context.Context) snapshots {
	var sn snapshots
	sn.running, sn.err = s.captureConfig(ctx, "running")
	if sn.err == nil {
		sn.startup, sn.err = s.captureConfig(ctx, "startup")
	}
	return sn
}

// ownerValid is broray_interface_owner_valid for name against the receipt.
//
// 3.5.0: the live blocks are compared with the endpoint recorded in the
// receipt, not with the current SOCKS inbound (3.x required both to be
// equal, so after a LAN/SOCKS change the interface lost its owner and could
// be neither repaired nor deleted). Expected-vs-live drift is reported by
// the status checks and fixed by repair.
func (s *service) ownerValid(sn snapshots, name string) (receipt, bool) {
	r, ok := s.loadReceipt()
	if !ok || sn.err != nil || r.InterfaceName != name || r.Owner != "BROray" || r.Protocol != "socks5" ||
		!validIPv4(r.Upstream.Host) || !validPort(r.Upstream.Port) || !validName(name) {
		return r, false
	}
	switch r.SchemaVersion {
	case 1:
		return r, legacyMigratable(sn, name, r.Upstream.Host, r.Upstream.Port)
	case 2:
		if !validDescription(r.Description) || r.RunningBlockSHA256 != r.StartupBlockSHA256 || !acceptedContracts[r.Contract] ||
			r.WriteProtocolSHA256 != writePolicySHA256 || len(r.RunningBlockSHA256) != 64 {
			return r, false
		}
		return r, sourceExact(sn.running, name, r.Upstream.Host, r.Upstream.Port, r.Description, r.RunningBlockSHA256) &&
			sourceExact(sn.startup, name, r.Upstream.Host, r.Upstream.Port, r.Description, r.StartupBlockSHA256)
	}
	return r, false
}

func legacyMigratable(sn snapshots, name, host string, port int) bool {
	rb, rc1 := extractBlock(sn.running, name)
	sb, rc2 := extractBlock(sn.startup, name)
	if rc1 != blockOK || rc2 != blockOK || !legacyBlockExact(rb, name, host, port) || !legacyBlockExact(sb, name, host, port) {
		return false
	}
	if declarationCount(sn.running, name) != 1 || declarationCount(sn.startup, name) != 1 {
		return false
	}
	a, ok1 := blockSHA256(rb)
	b, ok2 := blockSHA256(sb)
	return ok1 && ok2 && a == b
}

// ownerName is broray_interface_owner_name: the receipt's interface when
// the receipt fully validates.
func (s *service) ownerName(sn snapshots) string {
	r, ok := s.loadReceipt()
	if !ok || !validName(r.InterfaceName) {
		return ""
	}
	if _, valid := s.ownerValid(sn, r.InterfaceName); valid {
		return r.InterfaceName
	}
	return ""
}

// configuredName is routes/config.json .managedInterface when it is ProxyN.
func (s *service) configuredName() string {
	m, ok := compat.ReadObject(s.d.Path("routes", "config.json"))
	if !ok {
		return ""
	}
	n, _ := m["managedInterface"].(string)
	if validName(n) {
		return n
	}
	return ""
}

// selectedName is broray_interface_selected_name.
func (s *service) selectedName(sn snapshots) string {
	if n := s.ownerName(sn); n != "" {
		return n
	}
	if n := s.configuredName(); n != "" {
		return n
	}
	return "Proxy0"
}

// ownerWrite is broray_interface_owner_write: both configs must carry the
// exact signature with host:port and description; their block SHAs must agree.
func (s *service) ownerWrite(ctx context.Context, name, mode, host string, port int, desc string) error {
	if !validName(name) || !validDescription(desc) {
		return errors.New("invalid owner record")
	}
	sn := s.capture(ctx)
	if sn.err != nil {
		return sn.err
	}
	rb, rc1 := extractBlock(sn.running, name)
	sb, rc2 := extractBlock(sn.startup, name)
	if rc1 != blockOK || rc2 != blockOK || !signatureExact(rb, name, host, port, desc) || !signatureExact(sb, name, host, port, desc) {
		return errors.New("live block does not carry the exact BROray signature")
	}
	rs, ok1 := blockSHA256(rb)
	ss, ok2 := blockSHA256(sb)
	if !ok1 || !ok2 || rs != ss {
		return errors.New("running and startup blocks differ")
	}
	r := receipt{SchemaVersion: 2, Contract: ownerContract, Owner: "BROray", InterfaceName: name, Protocol: "socks5", Description: desc,
		SelectionMode: mode, WriteProtocolSHA256: writePolicySHA256, RunningBlockSHA256: rs, StartupBlockSHA256: ss, UpdatedAt: compat.Now()}
	r.Upstream.Host, r.Upstream.Port = host, port
	if err := os.MkdirAll(filepath.Dir(s.receiptPath()), 0o700); err != nil {
		return err
	}
	return compat.WriteJSON(s.receiptPath(), r)
}

// exactOwnedGrammar reports whether name carries the complete BROray
// grammar (valid "BROray…" description, socks5, upstream host:port,
// security-level public, up) identically in running and startup config.
// It returns the live description.
func exactOwnedGrammar(sn snapshots, name, host string, port int) (string, bool) {
	rb, rc := extractBlock(sn.running, name)
	if rc != blockOK {
		return "", false
	}
	desc, ok := blockDescription(rb)
	if !ok || !validDescription(desc) {
		return "", false
	}
	rs, ok1 := blockSHA256(rb)
	sb, rc2 := extractBlock(sn.startup, name)
	ss, ok2 := blockSHA256(sb)
	return desc, ok1 && ok2 && rc2 == blockOK && rs == ss &&
		sourceExact(sn.running, name, host, port, desc, "") && sourceExact(sn.startup, name, host, port, desc, "")
}

// selectForAction is broray_interface_select_safe.
//
// 3.5.0 adoption rule: an existing ProxyN without a valid receipt is adopted
// only when it carries the exact owned grammar (exactOwnedGrammar) in both
// configurations — the routes managedInterface first, then a unique match.
// Anything else (the 3.x "loose" signature included) is left untouched and a
// free ProxyN is allocated instead.
func (s *service) selectForAction(ctx context.Context, host string, port int) (string, error) {
	sn := s.capture(ctx)
	if sn.err != nil {
		return "", sn.err
	}
	if n := s.ownerName(sn); n != "" {
		return n, nil
	}
	adopt := func(name string) (string, bool) {
		desc, ok := exactOwnedGrammar(sn, name, host, port)
		if !ok {
			return "", false
		}
		if err := s.ownerWrite(ctx, name, "adopted-existing", host, port, desc); err != nil {
			return "", false
		}
		return name, true
	}
	if p := s.configuredName(); p != "" {
		if n, ok := adopt(p); ok {
			return n, nil
		}
	}
	var matches []string
	for _, l := range sn.running {
		if strings.HasPrefix(l, "interface Proxy") && validName(strings.TrimPrefix(l, "interface ")) {
			n := strings.TrimPrefix(l, "interface ")
			if _, ok := exactOwnedGrammar(sn, n, host, port); ok {
				matches = append(matches, n)
			}
		}
	}
	if len(matches) == 1 {
		if n, ok := adopt(matches[0]); ok {
			return n, nil
		}
	}
	for i := 0; i <= 255; i++ {
		n := "Proxy" + strconv.Itoa(i)
		if declarationCount(sn.running, n) == 0 {
			return n, nil
		}
	}
	return "", errors.New("no free ProxyN")
}

// syncRoutePolicy is broray_interface_sync_route_policy: the routes data
// follows the managed interface name (3.x file formats).
func (s *service) syncRoutePolicy(name string) error {
	cfgPath := s.d.Path("routes", "config.json")
	cfg, ok := compat.ReadObject(cfgPath)
	if !ok {
		return nil // no routes configuration yet: nothing references the interface
	}
	old, _ := cfg["managedInterface"].(string)
	changed := old != name
	setString := func(path, field, value string) error {
		m, ok := compat.ReadObject(path)
		if !ok {
			return errors.New(path + ": not a JSON object")
		}
		if cur, _ := m[field].(string); cur == value {
			return nil
		}
		m[field] = value
		return compat.WriteJSON(path, m)
	}
	if err := setString(cfgPath, "managedInterface", name); err != nil {
		return err
	}
	if files, _ := filepath.Glob(s.d.Path("routes", "manifests", "*.json")); files != nil {
		for _, f := range files {
			if err := setString(f, "targetInterface", name); err != nil {
				return err
			}
		}
	}
	catalogs, _ := filepath.Glob(s.d.Path("routes", "catalog", "*"))
	for _, c := range catalogs {
		if st, err := os.Lstat(c); err != nil || !st.IsDir() {
			continue
		}
		for _, f := range []string{"routes.json", "version.json"} {
			p := filepath.Join(c, f)
			if _, err := os.Lstat(p); err == nil {
				if err := setString(p, "targetInterface", name); err != nil {
					return err
				}
			}
		}
		if changed {
			for _, f := range []string{"export-plan.json", "keenetic-routes.bat", "router-preflight.json", "router-export-result.json",
				"router-delete-result.json", "router-export-missing.json", "router-export-conflicts.json"} {
				_ = os.Remove(filepath.Join(c, f))
			}
		}
	}
	if changed {
		now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
		if m, ok := compat.ReadObject(s.d.Path("routes", "installed", "routes.json")); ok {
			m["managedInterface"], m["routes"], m["updatedAt"] = name, []any{}, now
			if err := compat.WriteJSON(s.d.Path("routes", "installed", "routes.json"), m); err != nil {
				return err
			}
		}
		bundles, _ := filepath.Glob(s.d.Path("routes", "installed", "bundles", "*.json"))
		for _, b := range bundles {
			if m, ok := compat.ReadObject(b); ok {
				m["installedVersion"], m["routeKeys"], m["managedRouteKeys"], m["externalRouteKeys"] = nil, []any{}, []any{}, []any{}
				m["targetInterface"], m["managedMetric"], m["installedAt"], m["updatedAt"] = name, 1200, nil, now
				if err := compat.WriteJSON(b, m); err != nil {
					return err
				}
			}
		}
		states, _ := filepath.Glob(s.d.Path("routes", "state", "*.json"))
		for _, p := range states {
			if m, ok := compat.ReadObject(p); ok {
				status := "not_checked"
				if m["downloadedVersion"] != nil {
					status = "downloaded"
				}
				m["status"] = status
				for _, k := range []string{"installedVersion", "lastVerifiedAt", "verifyResult", "preflight", "exportResult", "deleteResult", "routerPresence", "lastError"} {
					m[k] = nil
				}
				if dr, ok := m["downloadResult"].(map[string]any); ok {
					dr["managedInterface"] = name
				}
				m["updatedAt"] = now
				if err := compat.WriteJSON(p, m); err != nil {
					return err
				}
			}
		}
	}
	_ = os.Remove(s.d.RunPath("routes-router-config-cache.json"))
	return nil
}
