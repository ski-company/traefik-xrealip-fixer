package config

// Config the plugin configuration.
type Config struct {
	TrustIP         map[string][]string `json:"trustip"`
	AutoRefresh     bool                `json:"autoRefresh,omitempty"`     // enable periodic refresh
	RefreshInterval string              `json:"refreshInterval,omitempty"` // e.g. "12h", "1h"
	DirectDepth     int                 `json:"directDepth"`               // number of hops to consider direct
	Debug           bool                `json:"debug,omitempty"`
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{
		TrustIP:         make(map[string][]string),
		AutoRefresh:     true,
		RefreshInterval: "12h",
		DirectDepth:     0,
		Debug:           false,
	}
}
