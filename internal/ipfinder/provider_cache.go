package ipfinder

import (
	"sync"
	"time"

	"github.com/ski-company/traefik-xrealip-fixer/internal/logger"
	"github.com/ski-company/traefik-xrealip-fixer/internal/providers/cloudflare"
	"github.com/ski-company/traefik-xrealip-fixer/internal/providers/cloudfront"
)

type providerCache struct {
	mu          sync.RWMutex
	cfCIDRs     []string
	cfnCIDRs    []string
	lastRefresh time.Time
}

var globalProviderCache providerCache
var globalRefreshOnce sync.Once
var globalIntervalMu sync.RWMutex
var globalInterval = 12 * time.Hour

func setGlobalInterval(interval time.Duration) {
	if interval <= 0 {
		return
	}
	globalIntervalMu.Lock()
	if interval < globalInterval {
		globalInterval = interval
	}
	globalIntervalMu.Unlock()
}

func getGlobalInterval() time.Duration {
	globalIntervalMu.RLock()
	defer globalIntervalMu.RUnlock()
	return globalInterval
}

func startGlobalRefresh(interval time.Duration) {
	setGlobalInterval(interval)
	globalRefreshOnce.Do(func() {
		go refreshProvidersLoop()
	})
}

func refreshProvidersLoop() {
	jitter := time.Duration(int64(time.Second) * (int64(time.Now().UnixNano()) % 7))
	t := time.NewTimer(getGlobalInterval() + jitter)
	defer t.Stop()

	for {
		<-t.C
		forceRefreshProviderBase()
		interval := getGlobalInterval()
		t.Reset(interval)
	}
}

func forceRefreshProviderBase() {
	cfCIDRs := cloudflare.TrustedIPS()
	cfnCIDRs := cloudfront.TrustedIPS()

	globalProviderCache.mu.Lock()
	globalProviderCache.cfCIDRs = cfCIDRs
	globalProviderCache.cfnCIDRs = cfnCIDRs
	globalProviderCache.lastRefresh = time.Now()
	globalProviderCache.mu.Unlock()

	logger.LogInfo("providers IPS cache refreshed")
}

func getProviderBase(ttl time.Duration) (cfCIDRs []string, cfnCIDRs []string, refreshed bool) {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}

	now := time.Now()
	globalProviderCache.mu.RLock()
	fresh := len(globalProviderCache.cfCIDRs) > 0 &&
		len(globalProviderCache.cfnCIDRs) > 0 &&
		now.Sub(globalProviderCache.lastRefresh) < ttl
	if fresh {
		cfCIDRs = append([]string(nil), globalProviderCache.cfCIDRs...)
		cfnCIDRs = append([]string(nil), globalProviderCache.cfnCIDRs...)
		globalProviderCache.mu.RUnlock()
		return cfCIDRs, cfnCIDRs, false
	}
	globalProviderCache.mu.RUnlock()

	globalProviderCache.mu.Lock()
	defer globalProviderCache.mu.Unlock()
	now = time.Now()
	fresh = len(globalProviderCache.cfCIDRs) > 0 &&
		len(globalProviderCache.cfnCIDRs) > 0 &&
		now.Sub(globalProviderCache.lastRefresh) < ttl
	if !fresh {
		globalProviderCache.cfCIDRs = cloudflare.TrustedIPS()
		globalProviderCache.cfnCIDRs = cloudfront.TrustedIPS()
		globalProviderCache.lastRefresh = now
		refreshed = true
	}

	cfCIDRs = append([]string(nil), globalProviderCache.cfCIDRs...)
	cfnCIDRs = append([]string(nil), globalProviderCache.cfnCIDRs...)
	return cfCIDRs, cfnCIDRs, refreshed
}
