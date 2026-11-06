// Package control is the daemon lifecycle part of the root-only control
// socket: ping (readiness) and the authenticated shutdown the installer
// uses to stop the daemon without a PID (design §6).
package control

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// TokenHeader carries the shutdown token (state/control.token, 0600).
const TokenHeader = "X-BROray-Control-Token"

const (
	PingPath     = "/api/v1/daemon/ping"
	StopPath     = "/api/v1/daemon/stop" // `broray ctl stop` (root-only socket)
	ShutdownPath = "/api/v1/daemon/shutdown"
)

// EnsureToken returns the control token, creating it (32 random bytes,
// hex, mode 0600) when missing. The token survives daemon restarts and
// updates, so an installer of any version can stop any daemon version.
func EnsureToken(root platform.Root) (string, error) {
	if t, err := ReadToken(root); err == nil {
		return t, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	t := hex.EncodeToString(b)
	if err := os.MkdirAll(root.State(), 0o700); err != nil {
		return "", err
	}
	if err := platform.WriteFileAtomic(root.ControlToken(), []byte(t+"\n"), 0o600); err != nil {
		return "", err
	}
	return t, nil
}

// ReadToken reads the token; a file readable by others is refused.
func ReadToken(root platform.Root) (string, error) {
	st, err := os.Lstat(root.ControlToken())
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0o077 != 0 {
		return "", errors.New("control token must be a regular file with mode 0600")
	}
	b, err := os.ReadFile(root.ControlToken())
	if err != nil {
		return "", err
	}
	t := strings.TrimSpace(string(b))
	if len(t) != 64 {
		return "", errors.New("control token malformed")
	}
	return t, nil
}

// Mount adds ping, stop and shutdown to the control mux. stop is called
// once per accepted request (non-blocking for the handler).
func Mount(mux *http.ServeMux, version, token string, stop func()) {
	mux.HandleFunc(PingPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"version":` + strconv.Quote(version) + `,"pid":` + strconv.Itoa(os.Getpid()) + `}}`))
	})
	mux.HandleFunc(StopPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"stopping":true}}`))
		stop()
	})
	mux.HandleFunc(ShutdownPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		got := r.Header.Get(TokenHeader)
		if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"success":false,"error":{"code":"CONTROL_TOKEN_INVALID","message":"Shutdown token rejected."}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"stopping":true}}`))
		stop()
	})
}

func client(root platform.Root, timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", root.DaemonSocket())
	}}}
}

// Ping returns the version the daemon reports on its control socket.
func Ping(ctx context.Context, root platform.Root) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://broray"+PingPath, nil)
	if err != nil {
		return "", err
	}
	resp, err := client(root, 5*time.Second).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != 200 || json.Unmarshal(b, &body) != nil || !body.Success {
		return "", fmt.Errorf("ping: HTTP %d", resp.StatusCode)
	}
	return body.Data.Version, nil
}

// Shutdown asks the daemon to stop, authenticated by the token file.
// It returns once the request is accepted; the caller then waits for the
// daemon lock (the kernel releases it only when the process has exited).
func Shutdown(ctx context.Context, root platform.Root) error {
	token, err := ReadToken(root)
	if err != nil {
		return fmt.Errorf("control token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://broray"+ShutdownPath, nil)
	if err != nil {
		return err
	}
	req.Header.Set(TokenHeader, token)
	resp, err := client(root, 10*time.Second).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("shutdown: HTTP %d", resp.StatusCode)
	}
	return nil
}

// WaitLock waits until path can be locked (the holder exited) and returns
// the held lock, or platform.ErrLocked after timeout.
func WaitLock(path string, timeout time.Duration) (*platform.Lock, error) {
	deadline := time.Now().Add(timeout)
	for {
		l, err := platform.TryLock(path)
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, platform.ErrLocked) || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(200 * time.Millisecond)
	}
}
