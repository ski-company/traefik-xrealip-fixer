package ipfinder

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ski-company/traefik-xrealip-fixer/internal/providers"
)

// Ipfinder is a plugin that overwrites the true IP.
type Ipfinder struct {
	next        http.Handler
	name        string
	TrustIP     map[providers.Provider][]*net.IPNet
	cfCIDRsQty  int
	cfnCIDRsQty int
	directDepth int
	refreshTTL  time.Duration

	mu        sync.RWMutex        // guards TrustIP
	userTrust map[string][]string // keep user-supplied CIDRs for merges on refresh
}
