package ipfinder

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ski-company/traefik-xrealip-fixer/internal/config"
	"github.com/ski-company/traefik-xrealip-fixer/internal/logger"
	"github.com/ski-company/traefik-xrealip-fixer/internal/mmdb"
	"github.com/ski-company/traefik-xrealip-fixer/internal/providers/geolite2"
)

type geoIPSettings struct {
	licenseKey  string
	downloadURL string
}

type geoIPCache struct {
	mu          sync.RWMutex
	refreshMu   sync.Mutex
	reader      *mmdb.Reader
	lastRefresh time.Time
	desired     geoIPSettings
	loaded      geoIPSettings
}

const defaultGeoTTL = 12 * time.Hour

var globalGeoIPCache geoIPCache

func newGeoIPSettings(cfg *config.Config) geoIPSettings {
	return geoIPSettings{
		licenseKey:  strings.TrimSpace(cfg.GeoLite2LicenseKey),
		downloadURL: strings.TrimSpace(cfg.GeoLite2DownloadURL),
	}
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
		ttl = defaultGeoTTL
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

	return refreshGeoIPBaseWithSettings(settings, ttl)
}

func forceRefreshGeoIPBase() {
	settings := getGlobalGeoIPSettings()
	if !settings.enabled() {
		return
	}

	refreshed, err := refreshGeoIPBaseWithSettings(settings, getGlobalInterval())
	if err != nil {
		logger.LogWarn("GeoLite2 Country database refresh failed", "error", err.Error())
	} else if refreshed {
		logger.LogInfo("GeoLite2 Country database refreshed")
	}
}

func refreshGeoIPBaseWithSettings(settings geoIPSettings, ttl time.Duration) (bool, error) {
	if !settings.enabled() {
		return false, nil
	}
	if ttl <= 0 {
		ttl = defaultGeoTTL
	}

	globalGeoIPCache.refreshMu.Lock()
	defer globalGeoIPCache.refreshMu.Unlock()

	now := time.Now()
	globalGeoIPCache.mu.RLock()
	fresh := globalGeoIPCache.reader != nil &&
		globalGeoIPCache.loaded == settings &&
		now.Sub(globalGeoIPCache.lastRefresh) < ttl
	globalGeoIPCache.mu.RUnlock()
	if fresh {
		return false, nil
	}

	data, err := geolite2.DownloadCountryDatabase(settings.providerConfig())
	if err != nil {
		return false, err
	}

	reader, err := mmdb.Open(data)
	if err != nil {
		return false, err
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
	globalGeoIPCache.mu.RLock()
	reader := globalGeoIPCache.reader
	if reader == nil {
		globalGeoIPCache.mu.RUnlock()
		return ""
	}

	ip := net.ParseIP(clientIP)
	if ip == nil || !isGeoIPLookupAddr(ip) {
		globalGeoIPCache.mu.RUnlock()
		return ""
	}

	code := reader.LookupCountryCode(ip)
	globalGeoIPCache.mu.RUnlock()

	return strings.ToUpper(strings.TrimSpace(code))
}

func isGeoIPLookupAddr(ip net.IP) bool {
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && ip.IsGlobalUnicast()
}
