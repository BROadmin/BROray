//go:build dev

package main

import (
	"net/http"
	"os"
	"time"

	akeenetic "github.com/BROadmin/broray/areas/keenetic"
	aroutes "github.com/BROadmin/broray/areas/routes"
	axray "github.com/BROadmin/broray/areas/xray"
)

// Dev builds honour HTTPS_PROXY for Xray downloads (sandboxed CI hosts).
// Router builds never use a proxy for the Xray archive.
func init() {
	axray.DownloadClient = &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 60 * time.Second}, Timeout: 30 * time.Minute}
	axray.CatalogClient = &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}, Timeout: 60 * time.Second}
}

func init() {
	if v := os.Getenv("BRORAY_DEV_RAW_BASE"); v != "" {
		aroutes.RawBase = v
	}
	// A local fake GitHub API for the scheduled route catalog check.
	if v := os.Getenv("BRORAY_DEV_ROUTES_API"); v != "" {
		aroutes.GitHubAPI = v
	}
	if d, err := time.ParseDuration(os.Getenv("BRORAY_DEV_ROUTES_TICK")); err == nil && d > 0 {
		aroutes.ScheduleTick = d
	}
	// A local fake GitHub (API at <v>/api, downloads at <v>/XTLS/Xray-core/releases/download).
	if v := os.Getenv("BRORAY_DEV_GITHUB"); v != "" {
		axray.GitHubAPI = v + "/api"
		axray.GitHubDownload = v + "/XTLS/Xray-core/releases/download"
		axray.DownloadBase = v
	}
}

// BRORAY_DEV_IPV6_TTL (e.g. "2s") shortens the IPv6 fact cache so UI tests
// can switch fakerci's IPv6 on and off without waiting five minutes.
func init() {
	if d, err := time.ParseDuration(os.Getenv("BRORAY_DEV_IPV6_TTL")); err == nil && d > 0 {
		akeenetic.IPv6CacheTTL = d
	}
}
