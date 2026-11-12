package installer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

type installWebStatus struct {
	State        string `json:"state"`
	Enabled      bool   `json:"enabled"`
	Consistent   bool   `json:"consistent"`
	CanEnable    bool   `json:"canEnable"`
	Address      string `json:"address"`
	LocalAddress string `json:"localAddress"`
	KeenDNS      struct {
		Available bool `json:"available"`
	} `json:"keenDns"`
	Reason struct {
		Code string `json:"code"`
	} `json:"reason"`
}

var errInitialWebBusy = errors.New("OPERATION_BUSY")

// publishInitialWebUI uses the same ownership-checked transaction as the UI.
// It is called only for a new installation, never from daemon startup/update.
// Failure of optional external publication does not undo a working local app.
func publishInitialWebUI(ctx context.Context, root platform.Root, rep *Report) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", root.DaemonSocket())
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(method, path string) (installWebStatus, error) {
		var result struct {
			Success bool             `json:"success"`
			Data    installWebStatus `json:"data"`
			Error   struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		req, err := http.NewRequestWithContext(ctx, method, "http://broray"+path, strings.NewReader("{}"))
		if err != nil {
			return result.Data, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-BROray-Request", "1")
		req.Header.Set("Origin", "http://broray")
		resp, err := client.Do(req)
		if err != nil {
			return result.Data, errors.New("WEB_ACCESS_CONTROL_UNAVAILABLE")
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
		if err != nil || len(b) > 65536 || json.Unmarshal(b, &result) != nil {
			return result.Data, errors.New("WEB_ACCESS_INVALID_RESPONSE")
		}
		if resp.StatusCode != http.StatusOK || !result.Success {
			code := safeWebCode(result.Error.Code)
			if resp.StatusCode == http.StatusConflict && !result.Success && code == "OPERATION_BUSY" {
				return result.Data, errInitialWebBusy
			}
			if code == "" {
				code = fmt.Sprintf("WEB_ACCESS_HTTP_%d", resp.StatusCode)
			}
			return result.Data, errors.New(code)
		}
		return result.Data, nil
	}
	warn := func(code string) {
		if safeWebCode(code) == "" {
			code = "WEB_ACCESS_NOT_CONFIRMED"
		}
		rep.warning("%s: BROray installed; external WebUI address not confirmed. Local WebUI remains available. Check KeenDNS and then run: %s ctl post /api/keenetic/web-access-enable.cgi '{}'", code, BinLink())
	}
	ready := func(s installWebStatus) bool {
		return s.State == "enabled" && s.Enabled && s.Consistent && s.KeenDNS.Available && strings.HasPrefix(s.Address, "https://broray.")
	}
	waiting := false
	for {
		status, err := call(http.MethodGet, "/api/keenetic/web-access-status.cgi")
		if err != nil {
			warn(err.Error())
			return
		}
		if !waiting && status.LocalAddress != "" {
			rep.step("local WebUI: %s", status.LocalAddress)
		}
		if ready(status) {
			rep.step("KeenDNS WebUI: %s (enabled, consistent)", status.Address)
			return
		}
		// Only create a missing record. Never auto-repair drifted/foreign records.
		if status.State != "disabled" || !status.CanEnable || !status.KeenDNS.Available {
			warn(status.Reason.Code)
			return
		}
		if _, err = call(http.MethodPost, "/api/keenetic/web-access-enable.cgi"); err != nil {
			if errors.Is(err, errInitialWebBusy) {
				// HTTP 409 is returned before any mutation. Bootstrap Xray may
				// own the global operation lock. Never retry uncertain writes.
				if !waiting {
					rep.step("KeenDNS: waiting for startup operation to finish")
					waiting = true
				}
				timer := time.NewTimer(2 * time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					warn("OPERATION_BUSY")
					return
				case <-timer.C:
					continue
				}
			}
			warn(err.Error())
			return
		}
		status, err = call(http.MethodGet, "/api/keenetic/web-access-status.cgi")
		if err != nil {
			warn(err.Error())
			return
		}
		if !ready(status) {
			warn("WEB_ACCESS_NOT_CONFIRMED")
			return
		}
		rep.step("KeenDNS WebUI: %s (enabled, consistent)", status.Address)
		return
	}
}

func safeWebCode(code string) string {
	if code == "" || len(code) > 96 {
		return ""
	}
	for _, c := range code {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return ""
		}
	}
	return code
}
