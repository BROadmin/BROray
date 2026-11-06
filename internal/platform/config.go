package platform

import (
	"errors"
	"net/netip"
	"os"
)

// Config is config.v1.json: the owner's settings. Secrets never live here.
type Config struct {
	SchemaVersion int    `json:"schemaVersion"`
	ListenAddress string `json:"listenAddress"` // LAN IP the WebUI and Xray SOCKS bind to; "" = auto (Bridge0)
	WebPort       int    `json:"webPort"`       // 8080 like 3.x
	SocksPort     int    `json:"socksPort"`     // 2080 like 3.x
	LogLevel      string `json:"logLevel"`      // xray loglevel
	Keenetic      struct {
		ManagementHost string `json:"managementHost"` // for /auth; "" = ListenAddress
		ManagementPort int    `json:"managementPort"` // 80
		RCIURL         string `json:"rciUrl"`         // http://127.0.0.1:79
		WriteTransport string `json:"writeTransport"` // rci | ndmc | auto (rci, ndmc fallback)
		Interface      string `json:"interface"`      // preferred ProxyN name; "" = allocate
		RouteMetric    int    `json:"routeMetric"`    // 1200 like 3.x
		LANInterface   string `json:"lanInterface"`   // Bridge0
	} `json:"keenetic"`
	Health struct {
		ProbeURLs     []string `json:"probeUrls"`
		IntervalSec   int      `json:"intervalSec"`
		AutoSwitch    bool     `json:"autoSwitch"`
		FailThreshold int      `json:"failureThreshold"`
		CooldownMin   int      `json:"cooldownMinutes"`
		SelectionRule string   `json:"selectionRule"` // best-quality | lowest-ping | preferred
		PreferredID   string   `json:"preferredServerId"`
	} `json:"health"`
	Xray struct {
		Version string `json:"version"` // pinned release tag, e.g. v25.9.11
		Repo    string `json:"repo"`    // XTLS/Xray-core
	} `json:"xray"`
	Routes struct {
		Repository string `json:"repository"` // RockBlack-VPN/ip-address
		Commit     string `json:"commit"`     // pinned commit
	} `json:"routes"`
	// Update selects the release channel (docs/RELEASE-CHANNEL.md). The URL
	// override is only as good as the signature check: a manifest from any
	// URL must still be signed by a key built into the binary.
	Update struct {
		Channel    string `json:"channel"`    // stable (default) | test | …
		ChannelURL string `json:"channelUrl"` // "" = built-in URL of the channel; https:// or file://
	} `json:"update"`
}

func DefaultConfig() Config {
	var c Config
	c.SchemaVersion = 1
	c.WebPort = 8080
	c.SocksPort = 2080
	c.LogLevel = "warning"
	c.Keenetic.ManagementPort = 80
	c.Keenetic.RCIURL = "http://127.0.0.1:79"
	c.Keenetic.WriteTransport = "auto"
	c.Keenetic.RouteMetric = 1200
	c.Keenetic.LANInterface = "Bridge0"
	c.Health.ProbeURLs = []string{"http://cp.cloudflare.com/", "http://www.gstatic.com/generate_204"}
	c.Health.IntervalSec = 15
	c.Health.FailThreshold = 3
	c.Health.CooldownMin = 10
	c.Health.SelectionRule = "best-quality"
	c.Xray.Version = "" // "" = newest version pinned in the build (modules/xray.Pinned)
	c.Xray.Repo = "XTLS/Xray-core"
	c.Routes.Repository = "RockBlack-VPN/ip-address"
	c.Routes.Commit = "65a895d1e8cf30653e94a00f2e374991a6838e36"
	c.Update.Channel = "stable"
	return c
}

func LoadConfig(r Root) (Config, error) {
	c := DefaultConfig()
	err := ReadJSON(r.ConfigFile(), &c)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if c.SchemaVersion != 1 {
		return c, errors.New("CONFIG_SCHEMA_UNSUPPORTED")
	}
	if c.WebPort == 0 {
		c.WebPort = 8080
	}
	if c.SocksPort == 0 {
		c.SocksPort = 2080
	}
	if c.Keenetic.RouteMetric == 0 {
		c.Keenetic.RouteMetric = 1200
	}
	if c.Update.Channel == "" {
		c.Update.Channel = "stable"
	}
	if c.Keenetic.RCIURL == "" {
		c.Keenetic.RCIURL = "http://127.0.0.1:79"
	}
	if c.ListenAddress != "" {
		if _, err := netip.ParseAddr(c.ListenAddress); err != nil {
			return c, errors.New("CONFIG_LISTEN_ADDRESS_INVALID")
		}
	}
	return c, nil
}

func SaveConfig(r Root, c Config) error {
	c.SchemaVersion = 1
	return WriteJSONAtomic(r.ConfigFile(), c, 0o600)
}
