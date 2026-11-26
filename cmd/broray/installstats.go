package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/installstats"
	"github.com/BROadmin/broray/internal/update"
)

func runInstallStatistics(ctx context.Context, a *app.App, listenAddress string) {
	queue := installstats.NewQueue(a.Root)
	var sender *installstats.Sender
	if installstats.Endpoint != "" {
		sender, _ = installstats.NewSender(installstats.Endpoint)
	}
	client := &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true},
		Timeout:   3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	build := installstats.Build{Version: app.Version, Revision: app.Revision}
	arch := update.Platform()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for ctx.Err() == nil {
		_ = queue.ConfirmFirstInstall(ctx, build, arch, time.Now(), func(ctx context.Context, expected string) (installstats.Identity, bool) {
			sum, err := installstats.ExecutableDigest("/proc/self/exe")
			if err != nil || sum != expected || !installStatisticsWebReady(ctx, client, listenAddress) {
				return installstats.Identity{}, false
			}
			return readInstallStatisticsIdentity(ctx, a, 4*time.Second), true
		})
		installer.RetryCommittedStatistics(a.Root)
		_, _ = queue.Flush(ctx, sender, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func observeInstallStatisticsBuild(ctx context.Context, a *app.App, expected string) (control.BuildObservation, bool) {
	if len(expected) != 64 || app.Revision == "" {
		return control.BuildObservation{}, false
	}
	sum, err := installstats.ExecutableDigest("/proc/self/exe")
	if err != nil || sum != expected {
		return control.BuildObservation{}, false
	}
	identity := readInstallStatisticsIdentity(ctx, a, 450*time.Millisecond)
	return control.BuildObservation{
		Version: app.Version, Revision: app.Revision, Architecture: update.Platform(), SHA256: sum,
		Model: identity.Model, DeviceKey: identity.DeviceKey,
	}, true
}

func readInstallStatisticsIdentity(ctx context.Context, a *app.App, budget time.Duration) installstats.Identity {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var version struct {
		Hardware string `json:"hw_id"`
		Model    string `json:"model"`
	}
	var identification struct {
		Serial string `json:"serial"`
	}
	if a.T.Show(ctx, "show/version", &version) != nil {
		version.Hardware, version.Model = "", ""
	}
	if a.T.Show(ctx, "show/identification", &identification) != nil {
		identification.Serial = ""
	}
	identity := installstats.RouterIdentity(version.Hardware, identification.Serial)
	if identity.Model == "" {
		identity.Model = installstats.RouterIdentity(version.Model, "").Model
	}
	return identity
}

func installStatisticsWebReady(ctx context.Context, client *http.Client, address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	switch host {
	case "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	target := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/"}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return err == nil && bytes.Contains(body, []byte("<title>BROray"))
}
