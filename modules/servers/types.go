// Package servers stores proxy servers (imported from share links or
// subscriptions) in the 3.x record format and renders Xray outbounds for them.
package servers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"regexp"
)

// Server is the stored record: field names follow BROray 3.x servers/<id>.json
// (schemaVersion 2) so migrate-legacy can import those files unchanged.
type Server struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Source        Source `json:"source"`
	URI           string `json:"uri"`
	Protocol      string `json:"protocol"` // vless | vmess | trojan | shadowsocks | hysteria2
	Address       string `json:"address"`
	Port          int    `json:"port"`
	UUID          string `json:"uuid,omitempty"`
	Password      string `json:"password,omitempty"`
	Method        string `json:"method,omitempty"` // shadowsocks
	AlterID       int    `json:"alterId,omitempty"`
	Encryption    string `json:"encryption,omitempty"`
	Flow          string `json:"flow,omitempty"`
	Network       string `json:"network"`  // raw | ws | grpc | httpupgrade | xhttp | kcp | hysteria
	Security      string `json:"security"` // none | tls | reality
	TLS           struct {
		ServerName    string   `json:"serverName"`
		Fingerprint   string   `json:"fingerprint"`
		AllowInsecure bool     `json:"allowInsecure"`
		ALPN          []string `json:"alpn"`
	} `json:"tls"`
	Reality struct {
		ServerName  string `json:"serverName"`
		Fingerprint string `json:"fingerprint"`
		PublicKey   string `json:"publicKey"`
		ShortID     string `json:"shortId"`
		SpiderX     string `json:"spiderX"`
	} `json:"reality"`
	Transport struct {
		Host        string          `json:"host"`
		Path        string          `json:"path"`
		ServiceName string          `json:"serviceName"`
		Mode        string          `json:"mode"`
		HeaderType  string          `json:"headerType"`
		Extra       json.RawMessage `json:"extra,omitempty"`
	} `json:"transport"`
	Hysteria struct {
		Obfs         string `json:"obfs,omitempty"`
		ObfsPassword string `json:"obfsPassword,omitempty"`
		Ports        string `json:"ports,omitempty"`
	} `json:"hysteria,omitempty"`
	Auth      string `json:"auth,omitempty"` // hysteria2 password
	CreatedAt string `json:"createdAt,omitempty"`
}

type Source struct {
	Type           string `json:"type"` // manual | subscription
	SubscriptionID string `json:"subscriptionId,omitempty"`
	NodeIndex      int    `json:"nodeIndex,omitempty"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{3,63}$`)

func ValidID(s string) bool { return idPattern.MatchString(s) }

func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "srv-" + hex.EncodeToString(b)
}

// Summary is the public view: no credentials.
type Summary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Network  string `json:"network"`
	Security string `json:"security"`
	Source   Source `json:"source"`
	Active   bool   `json:"active"`
}

func (s Server) Summary(active bool) Summary {
	return Summary{ID: s.ID, Name: s.Name, Protocol: s.Protocol, Address: s.Address, Port: s.Port, Network: s.Network, Security: s.Security, Source: s.Source, Active: active}
}
