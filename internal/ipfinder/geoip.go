package ipfinder

import (
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
	"github.com/ski-company/traefik-xrealip-fixer/internal/config"
	"github.com/ski-company/traefik-xrealip-fixer/internal/logger"
	"github.com/ski-company/traefik-xrealip-fixer/internal/providers/geolite2"
)

type geoIPSettings struct {
	licenseKey  string
	downloadURL string
}

type geoIPCache struct {
	mu          sync.RWMutex
	refreshMu   sync.Mutex
	reader      *maxminddb.Reader
	lastRefresh time.Time
	desired     geoIPSettings
	loaded      geoIPSettings
}

type geoIPCountryRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	RegisteredCountry struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"registered_country"`
	RepresentedCountry struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"represented_country"`
}

var globalGeoIPCache geoIPCache

func newGeoIPSettings(cfg *config.Config) geoIPSettings {
	settings := geoIPSettings{
		licenseKey:  strings.TrimSpace(cfg.GeoLite2LicenseKey),
		downloadURL: strings.TrimSpace(cfg.GeoLite2DownloadURL),
	}

	return settings
}

func (settings geoIPSettings) enabled() bool {
	return settings.licenseKey != ""
}

func (settings geoIPSettings) providerConfig() geolite2.Config {
	return geolite2.Config{
		LicenseKey:  settings.licenseKey,
		DownloadURL: settings.downloadURL,
	}
}

func setGlobalGeoIPSettings(settings geoIPSettings) {
	if !settings.enabled() {
		return
	}

	globalGeoIPCache.mu.Lock()
	globalGeoIPCache.desired = settings
	globalGeoIPCache.mu.Unlock()
}

func getGlobalGeoIPSettings() geoIPSettings {
	globalGeoIPCache.mu.RLock()
	defer globalGeoIPCache.mu.RUnlock()
	return globalGeoIPCache.desired
}

func (ipFinder *Ipfinder) refreshGeoIPCountry() (bool, error) {
	return getGeoIPBase(ipFinder.geoIP, ipFinder.refreshTTL)
}

func getGeoIPBase(settings geoIPSettings, ttl time.Duration) (bool, error) {
	if !settings.enabled() {
		return false, nil
	}
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}

	setGlobalGeoIPSettings(settings)

	now := time.Now()
	globalGeoIPCache.mu.RLock()
	fresh := globalGeoIPCache.reader != nil &&
		globalGeoIPCache.loaded == settings &&
		now.Sub(globalGeoIPCache.lastRefresh) < ttl
	globalGeoIPCache.mu.RUnlock()
	if fresh {
		return false, nil
	}

	return refreshGeoIPBaseWithSettings(settings, ttl, false)
}

func forceRefreshGeoIPBase() {
	settings := getGlobalGeoIPSettings()
	if !settings.enabled() {
		return
	}

	refreshed, err := refreshGeoIPBaseWithSettings(settings, getGlobalInterval(), false)
	if err != nil {
		logger.LogWarn("GeoLite2 Country database refresh failed", "error", err.Error())
	} else if refreshed {
		logger.LogInfo("GeoLite2 Country database refreshed")
	}
}

func refreshGeoIPBaseWithSettings(settings geoIPSettings, ttl time.Duration, force bool) (bool, error) {
	if !settings.enabled() {
		return false, nil
	}
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}

	globalGeoIPCache.refreshMu.Lock()
	defer globalGeoIPCache.refreshMu.Unlock()

	if !force {
		now := time.Now()
		globalGeoIPCache.mu.RLock()
		fresh := globalGeoIPCache.reader != nil &&
			globalGeoIPCache.loaded == settings &&
			now.Sub(globalGeoIPCache.lastRefresh) < ttl
		globalGeoIPCache.mu.RUnlock()
		if fresh {
			return false, nil
		}
	}

	data, err := geolite2.DownloadCountryDatabase(settings.providerConfig())
	if err != nil {
		return false, err
	}

	reader, err := maxminddb.OpenBytes(data)
	if err != nil {
		return false, fmt.Errorf("open GeoLite2 Country database: %w", err)
	}

	globalGeoIPCache.mu.Lock()
	oldReader := globalGeoIPCache.reader
	globalGeoIPCache.reader = reader
	globalGeoIPCache.lastRefresh = time.Now()
	globalGeoIPCache.loaded = settings
	if oldReader != nil {
		_ = oldReader.Close()
	}
	globalGeoIPCache.mu.Unlock()

	return true, nil
}

func lookupCountryCode(clientIP string) string {
	addr, err := netip.ParseAddr(clientIP)
	if err != nil {
		return ""
	}

	var record geoIPCountryRecord
	globalGeoIPCache.mu.RLock()
	if globalGeoIPCache.reader == nil {
		globalGeoIPCache.mu.RUnlock()
		return ""
	}
	result := globalGeoIPCache.reader.Lookup(addr)
	if err := result.Err(); err != nil {
		globalGeoIPCache.mu.RUnlock()
		return ""
	}
	if !result.Found() {
		globalGeoIPCache.mu.RUnlock()
		return ""
	}
	err = result.Decode(&record)
	globalGeoIPCache.mu.RUnlock()
	if err != nil {
		return ""
	}

	if code := strings.TrimSpace(record.Country.ISOCode); code != "" {
		return strings.ToUpper(code)
	}
	if code := strings.TrimSpace(record.RegisteredCountry.ISOCode); code != "" {
		return strings.ToUpper(code)
	}
	return strings.ToUpper(strings.TrimSpace(record.RepresentedCountry.ISOCode))
}
