package routepolicy

type Action string

const (
	Deny   Action = "deny"
	Direct Action = "direct"
	VPN    Action = "vpn"
)
const DomainStrategy = "IPOnDemand"

type Rule struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Enabled      bool     `json:"enabled"`
	Action       Action   `json:"action"`
	Domains      []string `json:"domains"`
	GeoSiteTags  []string `json:"geoSiteTags"`
	CountryCodes []string `json:"countryCodes"`
}
type OutboundTags struct{ VPN, Direct, Deny string }

func NewRule(id, name string) Rule { return Rule{ID: id, Name: name, Action: Deny} }
