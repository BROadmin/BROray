package health

import (
	"context"
	"errors"
	"net"
	"strings"
)

type ControlTarget struct {
	Name string
	URL  string
}

func ActiveControls() [2]ControlTarget {
	return [2]ControlTarget{
		{Name: "cloudflare", URL: "https://cp.cloudflare.com/generate_204"},
		{Name: "google", URL: "https://www.gstatic.com/generate_204"},
	}
}

// ControlObservation holds only diagnostic values from the fixed controls.
// Keep raw transport errors and addresses out of persisted diagnostics.
type ControlObservation struct {
	Target     string `json:"target"`
	Stage      string `json:"stage"`
	CurlCode   int    `json:"curlCode"`
	HTTPStatus int    `json:"httpStatus"`
}

func (target ControlTarget) Observe(status int, err error) ControlObservation {
	code := CurlCode(err)
	return ControlObservation{Target: target.Name, Stage: ProbeStage(code), CurlCode: code, HTTPStatus: status}
}

// CurlCode preserves the probe codes consumed by the legacy state and WebUI.
func CurlCode(err error) int {
	if err == nil {
		return 0
	}
	var ne net.Error
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		return 28
	case strings.Contains(msg, "no such host"):
		return 6
	case strings.Contains(msg, "socks") && strings.Contains(msg, "connection refused"):
		return 7
	case strings.Contains(msg, "socks"):
		return 97
	case strings.Contains(msg, "tls:") || strings.Contains(msg, "x509:") || strings.Contains(msg, "certificate"):
		return 35
	case strings.Contains(msg, "connection refused"):
		return 7
	}
	return 56
}

func ProbeStage(code int) string {
	switch code {
	case 5, 6:
		return "proxy-dns"
	case 7:
		return "proxy-connect"
	case 28:
		return "proxy-timeout"
	case 35, 51, 53, 58, 59, 60, 64, 66, 77, 80, 82, 83, 90, 91:
		return "proxy-tls"
	case 97:
		return "socks-handshake"
	}
	return "proxy-https"
}
