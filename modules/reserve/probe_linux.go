package reserve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
)

type ProbeTarget struct {
	Process                  ProcessIdentity
	ConfigPath, ConfigSHA256 string
	Endpoint                 netip.AddrPort
}

type ProbeOutcome struct {
	Verdict, Identity, ErrorCode string
	StartedAt, FinishedAt        time.Time
}

// Probe checks fixed HTTPS control URLs through an already owned Xray input.
// The caller must retain its lifecycle fence and bind the record generation.
func Probe(ctx context.Context, target ProbeTarget) ProbeOutcome {
	return probeWithRoots(ctx, target, nil)
}

// VerifyProbeTarget checks the existing read-only process/config/listener proof.
// It does not establish traffic health or authorize signaling a process.
func VerifyProbeTarget(target ProbeTarget) (string, error) {
	return probeProof(target)
}

var errProbeProof = errors.New("RESERVE_PROBE_UNCONFIRMED")

func probeConfigBytes(target ProbeTarget) ([]byte, error) {
	if !filepath.IsAbs(target.ConfigPath) || filepath.Clean(target.ConfigPath) != target.ConfigPath || len(target.ConfigSHA256) != 64 {
		return nil, errProbeProof
	}
	parent, err := platform.OpenDirNoFollow(filepath.Dir(target.ConfigPath))
	if err != nil {
		return nil, errProbeProof
	}
	defer parent.Close()
	data, err := procRead(parent, filepath.Base(target.ConfigPath), 2<<20)
	if err != nil {
		return nil, errProbeProof
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != target.ConfigSHA256 {
		return nil, errProbeProof
	}
	return data, nil
}

// Reconstruct the exact owned addition rather than recognizing only a tag.
// Extra route conditions, a redirecting outbound or an altered input are not
// accepted as the diagnostic path emitted by AttachHealthInbound.
func validProbeConfig(data []byte, endpoint netip.AddrPort) bool {
	v, err := strictjson.Decode(data)
	if err != nil {
		return false
	}
	cfg, ok := v.(map[string]any)
	if !ok {
		return false
	}
	original, err := json.Marshal(cfg)
	if err != nil {
		return false
	}
	ins, ok := cfg["inbounds"].([]any)
	if !ok || len(ins) < 2 {
		return false
	}
	outs, ok := cfg["outbounds"].([]any)
	if !ok || len(outs) < 2 {
		return false
	}
	routing, ok := cfg["routing"].(map[string]any)
	if !ok {
		return false
	}
	rules, ok := routing["rules"].([]any)
	if !ok || len(rules) < 2 {
		return false
	}
	cfg["inbounds"], cfg["outbounds"], routing["rules"] = ins[:len(ins)-1], outs[:len(outs)-1], rules[2:]
	base, err := json.Marshal(cfg)
	if err != nil {
		return false
	}
	rebuilt, err := AttachHealthInbound(base, endpoint)
	return err == nil && bytes.Equal(original, rebuilt)
}

func probeProof(target ProbeTarget) (string, error) {
	data, err := probeConfigBytes(target)
	if err != nil || !validProbeConfig(data, target.Endpoint) {
		return "", errProbeProof
	}
	dir, err := openProcess(target.Process.PID)
	if err != nil {
		return "", errProbeProof
	}
	defer dir.Close()
	cmdline, err := procRead(dir, "cmdline", 16384)
	if err != nil {
		return "", errProbeProof
	}
	want := []byte(target.Process.Executable + "\x00run\x00-c\x00" + target.ConfigPath + "\x00")
	if !bytes.Equal(cmdline, want) {
		return "", errProbeProof
	}
	proof, err := VerifyListener(target.Process, target.Endpoint)
	if err != nil {
		return "", errProbeProof
	}
	if _, err := probeConfigBytes(target); err != nil {
		return "", errProbeProof
	}
	identity, err := json.Marshal(struct {
		Listener                 ListenerProof
		ConfigPath, ConfigSHA256 string
	}{proof, target.ConfigPath, target.ConfigSHA256})
	if err != nil {
		return "", errProbeProof
	}
	sum := sha256.Sum256(identity)
	return hex.EncodeToString(sum[:]), nil
}

func probeWithRoots(ctx context.Context, target ProbeTarget, roots *x509.CertPool) (result ProbeOutcome) {
	result = ProbeOutcome{Verdict: "unknown", ErrorCode: "RESERVE_PROBE_UNCONFIRMED", StartedAt: time.Now()}
	defer func() { result.FinishedAt = time.Now() }()
	if ctx.Err() != nil {
		result.ErrorCode = "RESERVE_PROBE_CANCELLED"
		return
	}
	before, err := probeProof(target)
	if err != nil {
		return
	}
	proxy := &url.URL{Scheme: "socks5h", Host: target.Endpoint.String()}
	transport := &http.Transport{Proxy: http.ProxyURL(proxy), DialContext: (&net.Dialer{Timeout: 6 * time.Second}).DialContext,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, TLSHandshakeTimeout: 8 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: DefaultPolicy().URLTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	healthy := false
	for _, targetURL := range []string{"https://cp.cloudflare.com/generate_204", "https://www.gstatic.com/generate_204"} {
		if ctx.Err() != nil {
			break
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if err != nil {
			return
		}
		response, err := client.Do(req)
		if err != nil {
			continue
		}
		_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		closeErr := response.Body.Close()
		if response.StatusCode == http.StatusNoContent && readErr == nil && closeErr == nil {
			healthy = true
			break
		}
	}
	if ctx.Err() != nil {
		result.ErrorCode = "RESERVE_PROBE_CANCELLED"
		return
	}
	after, err := probeProof(target)
	if err != nil || after != before {
		result.ErrorCode = "RESERVE_PROBE_CONTEXT_CHANGED"
		return
	}
	result.Identity = before
	result.Verdict, result.ErrorCode = "unhealthy", "RESERVE_CONTROL_FAILED"
	if healthy {
		result.Verdict, result.ErrorCode = "healthy", ""
	}
	return
}
