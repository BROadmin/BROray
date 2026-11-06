package keenetic

// Account rights, derived exactly as KeeneticOS's own WebUI derives them.
//
// Source: the native WebUI bundle served by the router (KeeneticOS 5.2 Beta 1,
// KN-2710, main-3390981.js). With the authenticated session it reads
//   - RCI `whoami`        -> {"whoami":{"user":"<name>",...}}
//   - RCI `show.sc.user`  -> {"show":{"sc":{"user":{"<name>":{"tag":[...]}}}}}
// and applies:
//   - administrator     = tag "cli" OR tag "http"
//   - readonly          = tag "readonly"
//   - manager           = tag "manager"
//   - pages marked adminRightsRequired (Users and access, ISP provisioning,
//     OPKG) are hidden from readonly and manager accounts.
//
// BROray (like BROmihomo) is an OPKG (Entware) application that controls proxy routing for
// the whole network. It therefore admits exactly the accounts KeeneticOS
// admits to its OPKG page: administrators without "readonly" or "manager".
// No account name is special; "admin" is not hard-coded.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"unicode/utf8"
)

// ErrForbidden: the password is correct, but KeeneticOS itself would not give
// this account the OPKG page (no full administrator rights).
var ErrForbidden = errors.New("KEENETIC_ADMIN_RIGHTS_REQUIRED")

const (
	tagCLI      = "cli"
	tagHTTP     = "http"
	tagReadonly = "readonly"
	tagManager  = "manager"

	rciMaxResponse = 256 << 10
)

// Rights is the KeeneticOS account identity and its tag list.
type Rights struct {
	User string
	Tags []string
}

func (r Rights) has(tag string) bool {
	for _, t := range r.Tags {
		if t == tag { // exact element match, never a substring
			return true
		}
	}
	return false
}

// Administrator mirrors WebUI isAdministratorUser: tag cli or http.
func (r Rights) Administrator() bool { return r.has(tagCLI) || r.has(tagHTTP) }

// FullAdministrator mirrors WebUI admission to adminRightsRequired pages.
func (r Rights) FullAdministrator() bool {
	return r.Administrator() && !r.has(tagReadonly) && !r.has(tagManager)
}

// parseTags accepts the configuration list as a JSON array of strings, or a
// single string for a one-element list. Anything else fails closed.
func parseTags(raw json.RawMessage) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return []string{}, nil
	}
	var one string
	if raw[0] == '"' {
		if json.Unmarshal(raw, &one) != nil {
			return nil, ErrProtocol
		}
		return []string{one}, nil
	}
	// Decode elements individually: []string would silently turn null into "".
	var items []json.RawMessage
	if raw[0] != '[' || json.Unmarshal(raw, &items) != nil {
		return nil, ErrProtocol
	}
	list := make([]string, 0, len(items))
	for _, it := range items {
		it = bytes.TrimSpace(it)
		if len(it) == 0 || it[0] != '"' || json.Unmarshal(it, &one) != nil {
			return nil, ErrProtocol
		}
		list = append(list, one)
	}
	return list, nil
}

func validUser(u string) bool {
	if u == "" || !utf8.ValidString(u) || utf8.RuneCountInString(u) > 128 {
		return false
	}
	for _, r := range u {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// rciURL is the same origin as the /auth endpoint, path /rci/ (as the WebUI).
func rciURL(endpoint url.URL) string {
	u := endpoint
	u.Path, u.RawPath, u.RawQuery, u.Fragment = "/rci/", "", "", ""
	return u.String()
}

func rciRead(ctx context.Context, c *http.Client, target string, query, out any) error {
	body, err := json.Marshal(query)
	if err != nil {
		return ErrInternal
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return ErrInternal
	}
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, rciMaxResponse+1))
	defer clear(data)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrForbidden // KeeneticOS refuses management queries for this account
	case resp.StatusCode != http.StatusOK || err != nil || len(data) > rciMaxResponse:
		return ErrProtocol
	}
	if json.Unmarshal(data, out) != nil {
		return ErrProtocol
	}
	return nil
}

// readRights runs with the cookie of a session KeeneticOS has just accepted.
func readRights(ctx context.Context, c *http.Client, endpoint url.URL) (Rights, error) {
	target := rciURL(endpoint)
	var who struct {
		Whoami struct {
			User string `json:"user"`
		} `json:"whoami"`
	}
	if err := rciRead(ctx, c, target, map[string]any{"whoami": map[string]any{}}, &who); err != nil {
		return Rights{}, err
	}
	if !validUser(who.Whoami.User) {
		return Rights{}, ErrProtocol
	}
	var sc struct {
		Show struct {
			SC struct {
				User map[string]json.RawMessage `json:"user"`
			} `json:"sc"`
		} `json:"show"`
	}
	q := map[string]any{"show": map[string]any{"sc": map[string]any{"user": map[string]any{}}}}
	if err := rciRead(ctx, c, target, q, &sc); err != nil {
		return Rights{}, err
	}
	entry, ok := sc.Show.SC.User[who.Whoami.User]
	if !ok {
		return Rights{}, ErrProtocol
	}
	var cfg struct {
		Tag json.RawMessage `json:"tag"`
	}
	if json.Unmarshal(entry, &cfg) != nil {
		return Rights{}, ErrProtocol
	}
	tags, err := parseTags(cfg.Tag)
	if err != nil {
		return Rights{}, err
	}
	return Rights{User: who.Whoami.User, Tags: tags}, nil
}

// closeSession is the WebUI logout (DELETE /auth). Best effort: the local
// cookie jar is discarded regardless; this also ends the router-side session.
func closeSession(ctx context.Context, c *http.Client, endpoint url.URL) {
	req, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodDelete, endpoint.String(), nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "*/*")
	if resp, err := c.Do(req); err == nil {
		discard(resp)
	}
}

// authorize is called only after KeeneticOS accepted the password.
func (v *Verifier) authorize(ctx context.Context, c *http.Client) error {
	defer closeSession(ctx, c, v.endpoint)
	rights, err := readRights(ctx, c, v.endpoint)
	if err != nil {
		return err
	}
	if !rights.FullAdministrator() {
		return ErrForbidden
	}
	return nil
}
