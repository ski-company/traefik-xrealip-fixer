package config

// Config the plugin configuration.
type Config struct {
	TrustIP             map[string][]string `json:"trustip"`
	AutoRefresh         bool                `json:"autoRefresh,omitempty"`         // enable periodic refresh
	RefreshInterval     string              `json:"refreshInterval,omitempty"`     // e.g. "12h", "1h"
	DirectDepth         int                 `json:"directDepth"`                   // number of hops to consider direct
	GeoLite2LicenseKey  string              `json:"geoLite2LicenseKey,omitempty"`  // MaxMind GeoLite2 license key
	GeoLite2DownloadURL string              `json:"geoLite2DownloadURL,omitempty"` // optional MaxMind-compatible download endpoint
	Debug               bool                `json:"debug,omitempty"`
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{
		TrustIP:             make(map[string][]string),
		AutoRefresh:         true,
		RefreshInterval:     "12h",
		DirectDepth:         0,
		GeoLite2LicenseKey:  "",
		GeoLite2DownloadURL: "",
		Debug:               false,
	}
}
