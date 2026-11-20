// Package geodata manages BROray's own native Xray data files.
// It does not modify third-party geoip.dat/geosite.dat files.
package geodata

type Kind string

const (
	GeoIP         Kind = "geoip"
	GeoSite       Kind = "geosite"
	MaxAssetBytes      = 32 << 20
	// AssetDirectory is a managed child of the existing Xray asset directory.
	AssetDirectory = "broray-geodata"
)

type Selection struct {
	CountryCodes []string `json:"countryCodes"`
	GeoSiteTags  []string `json:"geoSiteTags"`
}

type Revisions struct {
	GeoIP   string `json:"geoip"`
	GeoSite string `json:"geosite"`
}

type Source struct {
	Kind     Kind     `json:"kind"`
	Tags     []string `json:"tags"`
	Revision string   `json:"revision"`
	URL      string   `json:"url"`
	Name     string   `json:"name"`
}

// Asset hashes describe actual validated bytes, not publisher signatures.
type Asset struct {
	Source
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
