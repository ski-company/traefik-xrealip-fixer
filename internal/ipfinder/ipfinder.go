package ipfinder

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ski-company/traefik-xrealip-fixer/internal/config"
	"github.com/ski-company/traefik-xrealip-fixer/internal/logger"
	"github.com/ski-company/traefik-xrealip-fixer/internal/providers"
)

var logInitOnce sync.Once

// New builds a Ipfinder handler from the given config.
func New(ctx context.Context, next http.Handler, cfg *config.Config, name string) (*Ipfinder, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	logger.EnableDebug(cfg.Debug)

	ipFinder := &Ipfinder{
		next:        next,
		name:        name,
		TrustIP:     make(map[providers.Provider][]*net.IPNet),
		userTrust:   cfg.TrustIP,
		directDepth: cfg.DirectDepth,
	}

	ival, err := time.ParseDuration(cfg.RefreshInterval)
	if err != nil || ival <= 0 {
		ival = 12 * time.Hour
	}
	ipFinder.refreshTTL = ival

	logInitOnce.Do(func() {
		logger.LogInfo("ipfinder initialized")
	})

	refreshed, err := ipFinder.refreshProvidersIPS()
	if err != nil {
		logger.LogWarn("initial providers IPS refresh load had issues", "error", err.Error(), "middleware", name)
	} else if refreshed {
		cfCIDRsQty, cfnCIDRsQty := ipFinder.cidrCounts()
		logger.LogInfo("providers IPS loaded", "cloudflare", fmt.Sprintf("%d", cfCIDRsQty), "cloudfront", fmt.Sprintf("%d", cfnCIDRsQty), "middleware", name)
	}

	if cfg.AutoRefresh {
		startGlobalRefresh(ival)
	}

	return ipFinder, nil
}
