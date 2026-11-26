package installstats

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/BROadmin/broray/internal/strictjson"
)

type Sender struct {
	endpoint string
	client   *http.Client
}

// Endpoint is a link-time setting for the approved HTTPS receiver.
var Endpoint string

func NewSender(endpoint string) (*Sender, error) {
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		MaxIdleConnsPerHost: 1, IdleConnTimeout: 15 * time.Second}
	return newSender(endpoint, transport)
}

func newSender(endpoint string, transport http.RoundTripper) (*Sender, error) {
	u, err := url.Parse(endpoint)
	if err != nil || transport == nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		u.Path == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("invalid installation statistics destination")
	}
	return &Sender{endpoint: u.String(), client: &http.Client{
		Transport: transport, Timeout: 8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (s *Sender) send(ctx context.Context, event Event) bool {
	raw, err := json.Marshal(event)
	if err != nil {
		return false
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(raw))
	if err != nil {
		return false
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "BROray-InstallStats/1")
	response, err := s.client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	receipt, err := io.ReadAll(io.LimitReader(response.Body, 257))
	if err != nil || len(receipt) > 256 {
		return false
	}
	data, err := strictjson.Decode(receipt)
	ack, ok := data.(map[string]any)
	return err == nil && ok && len(ack) == 2 && ack["eventId"] == event.EventID && ack["accepted"] == true
}
