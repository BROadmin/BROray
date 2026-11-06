package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

func ctlClient(root platform.Root) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", root.DaemonSocket())
	}}, Timeout: 10 * time.Minute}
}

func ctlCall(root platform.Root, method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://broray"+path, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// The 3.x CSRF markers; the socket is root-only.
	req.Header.Set("X-BROray-Request", "1")
	if strings.HasPrefix(path, "/api/operations/") {
		req.Header.Set("X-BROray-Request", "operations")
	}
	req.Header.Set("Origin", "http://broray")
	resp, err := ctlClient(root).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp.StatusCode, b, err
}

// runCtl is a thin client over the root-only control socket. It speaks the
// same /api/**/*.cgi contracts as the WebUI (no session on the socket).
func runCtl(root platform.Root, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, `usage: broray ctl ping | stop | status | ops | recover [--restart]
       broray ctl get <cgi-path>          e.g. get /api/servers/summary.cgi
       broray ctl post <cgi-path> [json]  e.g. post /api/xray/restart.cgi '{}'`)
		return 2
	}
	arg := func(i int) string {
		if len(args) > i {
			return args[i]
		}
		return ""
	}
	if args[0] == "recover" {
		// Local: runs before the daemon starts (S24broray start).
		return runRecover(root, args[1:])
	}
	var status int
	var body []byte
	var err error
	switch args[0] {
	case "ping":
		status, body, err = ctlCall(root, http.MethodGet, "/api/v1/daemon/ping", nil)
	case "stop":
		status, body, err = ctlCall(root, http.MethodPost, "/api/v1/daemon/stop", nil)
		if err == nil && status == 200 {
			deadline := time.Now().Add(30 * time.Second)
			for {
				held, lockErr := platform.HeldChecked(root.DaemonLock())
				if lockErr != nil {
					fmt.Fprintln(os.Stderr, "broray ctl: cannot verify daemon stop:", lockErr)
					return 1
				}
				if !held {
					break
				}
				if !time.Now().Before(deadline) {
					fmt.Fprintln(os.Stderr, "broray ctl: daemon did not stop in 30 s")
					return 1
				}
				time.Sleep(200 * time.Millisecond)
			}
		}
	case "status":
		status, body, err = ctlCall(root, http.MethodGet, "/api/home/summary.cgi", nil)
	case "ops":
		status, body, err = ctlCall(root, http.MethodGet, "/api/operations/status.cgi", nil)
	case "get":
		status, body, err = ctlCall(root, http.MethodGet, arg(1), nil)
	case "post":
		var payload any = map[string]any{}
		if j := arg(2); j != "" {
			if json.Unmarshal([]byte(j), &payload) != nil {
				fmt.Fprintln(os.Stderr, "invalid json")
				return 2
			}
		}
		status, body, err = ctlCall(root, http.MethodPost, arg(1), payload)
	default:
		fmt.Fprintln(os.Stderr, "unknown ctl command: "+args[0])
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "broray ctl: "+err.Error()+" (is the daemon running?)")
		return 1
	}
	os.Stdout.Write(body)
	if len(body) > 0 && body[len(body)-1] != '\n' {
		fmt.Println()
	}
	if status >= 400 {
		return 1
	}
	return 0
}
