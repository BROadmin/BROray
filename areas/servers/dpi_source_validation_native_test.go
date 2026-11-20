package servers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestDPISourceBatchNativeHTTPS(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	control, roots := dpiProposalControl(t)
	cert, key, pin := selfSigned(t)
	port := freeTCP(t)
	cfg, err := json.Marshal(map[string]any{
		"log":       map[string]any{"loglevel": "warning"},
		"inbounds":  []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "trojan", "settings": map[string]any{"clients": []any{map[string]any{"password": "new-password"}}}, "streamSettings": map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"certificates": []any{map[string]any{"certificateFile": cert, "keyFile": key}}}}}},
		"outbounds": []any{map[string]any{"protocol": "freedom", "sendThrough": "127.0.0.3", "settings": map[string]any{"redirect": control.Listener.Addr().String(), "finalRules": []any{map[string]any{"action": "allow", "ip": []any{"127.0.0.0/8"}}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	startXray(t, os.Getenv("BRORAY_XRAY_BIN"), filepath.Join(t.TempDir(), "server.json"), cfg, port, nil)
	id := f.e.addServer(t, "trojan://old-password@127.0.0.1:"+strconv.Itoa(port)+"?type=tcp&security=tls&sni=localhost&pcs="+pin+"#DPI")
	rec, err := ReadRecord(f.e.d, id)
	if err != nil {
		t.Fatal(err)
	}
	rec["source"] = map[string]any{"type": "subscription", "subscriptionId": "dpi-source", "enabled": true}
	rec, err = prepareDPIRecord(rec, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteRecord(f.e.d, rec); err != nil {
		t.Fatal(err)
	}
	path := RecordPath(f.e.d, id)
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rec, err = ReadRecord(f.e.d, id)
	if err != nil {
		t.Fatal(err)
	}
	rec["password"] = "new-password"
	next, err := Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	snapshot := f.process.RuntimeSnapshot()
	lease, err := f.e.s.validateSourceDPIProposalsWith(f.ctx, "dpi-source", []compat.DPIProposal{{Path: path, PreviousSHA256: compat.SHA256Hex(old), Record: next}}, func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
		calls++
		start := time.Now()
		proof, err := reserve.VerifyProbeTarget(target)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := dialTestSOCKS(target.Endpoint.String(), "www.gstatic.com:443")
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err = conn.SetDeadline(time.Now().Add(8 * time.Second)); err != nil {
			t.Fatal(err)
		}
		secure := tls.Client(conn, &tls.Config{RootCAs: roots, ServerName: "www.gstatic.com", MinVersion: tls.VersionTLS12})
		if err = secure.HandshakeContext(ctx); err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, "GET", "https://www.gstatic.com/generate_204", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Close = true
		if err = req.Write(secure); err != nil {
			t.Fatal(err)
		}
		response, err := http.ReadResponse(bufio.NewReader(secure), req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 204 || response.Header.Get("X-DPI-Egress") != "127.0.0.3" {
			t.Fatal("native source proposal HTTPS/egress failed", response.Status, response.Header)
		}
		after, err := reserve.VerifyProbeTarget(target)
		if err != nil || after != proof {
			t.Fatal(err)
		}
		return reserve.ProbeOutcome{Verdict: "healthy", Identity: proof, StartedAt: start, FinishedAt: time.Now()}
	})
	if err != nil || lease == nil || calls != 1 {
		t.Fatal("batch native candidate failed", err, calls)
	}
	defer lease.Close()
	if err = lease.Check(f.ctx); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(old, after) || snapshot != f.process.RuntimeSnapshot() {
		t.Fatal("old/main mutated", err)
	}
	t.Log("changed managed credentials verified through actual native proxy HTTPS204; synthetic CA confined to callback")
}
