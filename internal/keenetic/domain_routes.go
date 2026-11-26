package keenetic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"
)

// KeeneticOS exposes domain lists and route bindings as separate objects.
// The stable route index comes from the router and must be read back.
type DomainRouteBinding struct {
	Group     string `json:"group"`
	Interface string `json:"interface"`
	Index     string `json:"index"`
	Reject    bool   `json:"reject"`
	Gateway   string `json:"gateway"`
	Disabled  bool   `json:"disable"`
	Auto      bool   `json:"auto"`
	Policy    string `json:"policy"`
	Comment   string `json:"comment"`
}

type DomainRouteList struct {
	Description string `json:"description"`
	Include     []struct {
		Address string `json:"address"`
	} `json:"include"`
	Exclude []json.RawMessage `json:"exclude"`
}

type DomainRouteSnapshot struct {
	Lists       map[string]DomainRouteList
	Bindings    []DomainRouteBinding
	listImages  map[string]json.RawMessage
	routeImages []json.RawMessage
}

var (
	nativeDomainGroup = regexp.MustCompile(`^broray-[a-z0-9][a-z0-9-]{0,23}$`)
	nativeRouteIndex  = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

func ValidNativeDomainName(name string) bool {
	_, ipErr := netip.ParseAddr(name)
	return ipErr != nil && len(name) <= 253 && strings.Contains(name, ".") && reSNI.MatchString(name)
}

// DNSRouteTarget normalizes one object-group destination. Firmware may read
// back individual numeric addresses as host prefixes.
func DNSRouteTarget(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if ip, err := netip.ParseAddr(value); err == nil {
		return ip.Unmap().String(), ip.Zone() == ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		ip := prefix.Addr()
		return ip.Unmap().String(), prefix.Bits() == ip.BitLen()
	}
	value = strings.TrimSuffix(value, ".")
	if !ValidNativeDomainName(value) || strings.Trim(value, "0123456789.") == "" {
		return "", false
	}
	return value, true
}

func (r *RCI) domainCommands(ctx context.Context, commands []any) ([]json.RawMessage, error) {
	body, err := json.Marshal(commands)
	if err != nil || len(body) > 2<<20 {
		return nil, fmt.Errorf("%w: invalid domain route request", ErrTransport)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Base+"/rci/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: domain route request: %v", ErrTransport, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: domain route HTTP status %d", ErrTransport, resp.StatusCode)
	}
	var out []json.RawMessage
	data, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(data) > 8<<20 || json.Unmarshal(data, &out) != nil || len(out) != len(commands) {
		return nil, fmt.Errorf("%w: invalid domain route response", ErrTransport)
	}
	return out, nil
}

func (r *RCI) ReadDomainRoutes(ctx context.Context) (DomainRouteSnapshot, error) {
	var snap DomainRouteSnapshot
	var query []any
	if err := json.Unmarshal([]byte(`[{"show":{"sc":{"object-group":{"fqdn":{}}}}},{"show":{"sc":{"dns-proxy":{"route":{}}}}}]`), &query); err != nil {
		return snap, err
	}
	replies, err := r.domainCommands(ctx, query)
	if err != nil {
		return snap, err
	}
	var envelopes [2]struct {
		Show *struct {
			SC *struct {
				Groups *struct {
					FQDN map[string]json.RawMessage `json:"fqdn"`
				} `json:"object-group"`
				DNS *struct {
					Routes []json.RawMessage `json:"route"`
				} `json:"dns-proxy"`
			} `json:"sc"`
		} `json:"show"`
	}
	for i, reply := range replies {
		if json.Unmarshal(reply, &envelopes[i]) != nil || envelopes[i].Show == nil || envelopes[i].Show.SC == nil {
			return snap, fmt.Errorf("%w: unconfirmed domain route configuration", ErrTransport)
		}
	}
	if groups := envelopes[0].Show.SC.Groups; groups != nil {
		snap.listImages = groups.FQDN
		snap.Lists = make(map[string]DomainRouteList, len(groups.FQDN))
		for id, raw := range groups.FQDN {
			var list DomainRouteList
			if err := json.Unmarshal(raw, &list); err != nil {
				return DomainRouteSnapshot{}, err
			}
			snap.Lists[id] = list
		}
	}
	if dns := envelopes[1].Show.SC.DNS; dns != nil {
		snap.routeImages = dns.Routes
		for _, raw := range dns.Routes {
			var binding DomainRouteBinding
			if err := json.Unmarshal(raw, &binding); err != nil {
				return DomainRouteSnapshot{}, err
			}
			snap.Bindings = append(snap.Bindings, binding)
		}
	}
	return snap, nil
}

func onlyKnownDomainFields(raw json.RawMessage, out any) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out) == nil
}

func (snap DomainRouteSnapshot) groupObjects(group string) (DomainRouteList, bool, []DomainRouteBinding, error) {
	list, exists := snap.Lists[group]
	if exists && !onlyKnownDomainFields(snap.listImages[group], &list) {
		return DomainRouteList{}, false, nil, fmt.Errorf("unknown native domain list settings")
	}
	var bindings []DomainRouteBinding
	for i, b := range snap.Bindings {
		if b.Group == group {
			if !onlyKnownDomainFields(snap.routeImages[i], &b) {
				return DomainRouteList{}, false, nil, fmt.Errorf("unknown native domain route settings")
			}
			bindings = append(bindings, b)
		}
	}
	return list, exists, bindings, nil
}

func nativeListMatches(list DomainRouteList, description string, addresses []string) bool {
	got := make([]string, 0, len(list.Include))
	for _, d := range list.Include {
		value, ok := DNSRouteTarget(d.Address)
		if !ok {
			return false
		}
		got = append(got, value)
	}
	slices.Sort(got)
	want := slices.Clone(addresses)
	slices.Sort(want)
	return list.Description == description && len(list.Exclude) == 0 && slices.Equal(want, got)
}

func nativeManagedBinding(b DomainRouteBinding, group, iface string) bool {
	return b.Auto && b.Reject && nativeBindingIdentity(b, group, iface)
}

func (snap DomainRouteSnapshot) ConfirmsDomainRoute(description string, domains []string, expected DomainRouteBinding) bool {
	list, exists, bindings, err := snap.groupObjects(expected.Group)
	return err == nil && exists && nativeListMatches(list, description, domains) &&
		len(bindings) == 1 && bindings[0] == expected && nativeManagedBinding(expected, expected.Group, expected.Interface)
}

func nativeBindingIdentity(b DomainRouteBinding, group, iface string) bool {
	return b.Group == group && b.Interface == iface && nativeRouteIndex.MatchString(b.Index) &&
		!b.Disabled && b.Policy == "" && b.Gateway == "" && b.Comment == ""
}

func (r *RCI) nativeDomainCommand(ctx context.Context, command string) error {
	results, err := r.Parse(ctx, []string{command})
	if err != nil {
		return err
	}
	if len(results) != 1 || !results[0].OK || len(results[0].Status) == 0 {
		return fmt.Errorf("native domain command %q was unconfirmed", command)
	}
	return nil
}

func (r *RCI) waitDomainRoute(ctx context.Context, group string, matches func(DomainRouteList, bool, []DomainRouteBinding) bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		snap, err := r.ReadDomainRoutes(ctx)
		if err != nil {
			return err
		}
		list, exists, bindings, err := snap.groupObjects(group)
		if err != nil {
			return err
		}
		if matches(list, exists, bindings) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("native domain route readback did not converge: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// RemoveJournaledDomainRoute rolls back installation or completes removal
// after an interrupted request. A caller must own a previously persisted
// unique group name, list image and optional verified route index.
func (r *RCI) RemoveJournaledDomainRoute(ctx context.Context, group, description, iface string, domains []string, expected *DomainRouteBinding) error {
	if !nativeDomainGroup.MatchString(group) || !reDesc.MatchString(description) || !ValidProxyName(iface) || len(domains) == 0 {
		return fmt.Errorf("invalid journaled domain route")
	}
	snap, err := r.ReadDomainRoutes(ctx)
	if err != nil {
		return err
	}
	list, exists, bindings, err := snap.groupObjects(group)
	if err != nil {
		return err
	}
	if (exists && !nativeListMatches(list, description, domains)) || len(bindings) > 1 ||
		(len(bindings) == 1 && (!nativeBindingIdentity(bindings[0], group, iface) || (expected != nil && bindings[0] != *expected))) {
		return fmt.Errorf("journaled native domain objects differ from the router")
	}
	if len(bindings) == 1 {
		if err := r.nativeDomainCommand(ctx, "no dns-proxy route object-group "+group+" "+iface); err != nil {
			return err
		}
		// Keenetic can retain the deleted binding in its configuration
		// projection until the deletion has been saved.
		if err := r.nativeDomainCommand(ctx, "system configuration save"); err != nil {
			return err
		}
		if err := r.waitDomainRoute(ctx, group, func(list DomainRouteList, present bool, routes []DomainRouteBinding) bool {
			return len(routes) == 0 && present == exists && (!present || nativeListMatches(list, description, domains))
		}); err != nil {
			return err
		}
	}
	if exists {
		commands := []any{map[string]any{"object-group": map[string]any{"fqdn": []any{map[string]any{"name": group, "no": true}}}}}
		if _, err := r.domainCommands(ctx, commands); err != nil {
			return err
		}
	}
	absent := func(_ DomainRouteList, present bool, routes []DomainRouteBinding) bool {
		return !present && len(routes) == 0
	}
	if err := r.nativeDomainCommand(ctx, "system configuration save"); err != nil {
		return err
	}
	return r.waitDomainRoute(ctx, group, absent)
}

// ChangeDomainRoute creates a fresh group or removes the exact group and
// binding recorded by the caller. A durable transaction must retain the
// caller's expected image until readback and save have both been confirmed.
func (r *RCI) ChangeDomainRoute(ctx context.Context, group, description, iface string, domains []string, expected *DomainRouteBinding) (DomainRouteBinding, error) {
	return r.changeDomainRoute(ctx, group, description, iface, domains, expected, false)
}

// CompleteJournaledDomainRoute resumes a recorded apply using the original
// group. A list or route that differs from the approved image is preserved.
func (r *RCI) CompleteJournaledDomainRoute(ctx context.Context, group, description, iface string, domains []string) (DomainRouteBinding, error) {
	return r.changeDomainRoute(ctx, group, description, iface, domains, nil, true)
}

func (r *RCI) changeDomainRoute(ctx context.Context, group, description, iface string, domains []string, expected *DomainRouteBinding, resume bool) (DomainRouteBinding, error) {
	var empty DomainRouteBinding
	if !nativeDomainGroup.MatchString(group) || !reDesc.MatchString(description) || !ValidProxyName(iface) ||
		len(domains) == 0 || len(domains) > 50000 {
		return empty, fmt.Errorf("invalid managed domain route")
	}
	addresses := slices.Clone(domains)
	slices.Sort(addresses)
	for i, name := range addresses {
		if value, ok := DNSRouteTarget(name); !ok || value != name ||
			(i > 0 && addresses[i-1] == name) {
			return empty, fmt.Errorf("invalid or duplicate native domain name")
		}
	}
	read := func() (DomainRouteList, bool, []DomainRouteBinding, error) {
		snap, err := r.ReadDomainRoutes(ctx)
		if err != nil {
			return DomainRouteList{}, false, nil, err
		}
		return snap.groupObjects(group)
	}
	list, exists, bindings, err := read()
	if err != nil {
		return empty, err
	}
	matchingList := func(list DomainRouteList) bool {
		return nativeListMatches(list, description, addresses)
	}
	managedBinding := func(b DomainRouteBinding) bool {
		return nativeManagedBinding(b, group, iface)
	}
	if expected != nil {
		if !exists || !matchingList(list) || len(bindings) != 1 || bindings[0] != *expected ||
			!managedBinding(*expected) {
			return empty, fmt.Errorf("native domain list or binding ownership is unconfirmed")
		}
		return empty, r.RemoveJournaledDomainRoute(ctx, group, description, iface, domains, expected)
	}
	if !resume && (exists || len(bindings) != 0) {
		return empty, fmt.Errorf("native domain name is already in use")
	}
	if exists && !matchingList(list) || len(bindings) > 1 ||
		len(bindings) == 1 && (!exists || !managedBinding(bindings[0])) {
		return empty, fmt.Errorf("journaled native domain objects differ from the router")
	}
	if !exists {
		include := make([]map[string]string, 0, len(addresses))
		for _, d := range addresses {
			include = append(include, map[string]string{"address": d})
		}
		commands := []any{
			map[string]any{"object-group": map[string]any{"fqdn": map[string]any{group: map[string]any{"description": description, "include": include}}}},
		}
		if _, err := r.domainCommands(ctx, commands); err != nil {
			return empty, err
		}
		// Keenetic may project the newly created list through show/sc only
		// after saving. Confirm its exact image before binding a route.
		if err := r.nativeDomainCommand(ctx, "system configuration save"); err != nil {
			return empty, err
		}
		if err := r.waitDomainRoute(ctx, group, func(list DomainRouteList, present bool, routes []DomainRouteBinding) bool {
			return present && matchingList(list) && len(routes) == 0
		}); err != nil {
			return empty, err
		}
	}
	if len(bindings) == 0 {
		if err := r.nativeDomainCommand(ctx, "dns-proxy route object-group "+group+" "+iface+" auto reject"); err != nil {
			return empty, err
		}
	}
	if err := r.nativeDomainCommand(ctx, "system configuration save"); err != nil {
		return empty, err
	}
	var binding DomainRouteBinding
	present := func(list DomainRouteList, exists bool, bindings []DomainRouteBinding) bool {
		if !exists || !matchingList(list) || len(bindings) != 1 || !managedBinding(bindings[0]) {
			return false
		}
		binding = bindings[0]
		return true
	}
	err = r.waitDomainRoute(ctx, group, present)
	return binding, err
}
