package ipfinder

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/ski-company/traefik-xrealip-fixer/internal/helper"
	"github.com/ski-company/traefik-xrealip-fixer/internal/logger"
	"github.com/ski-company/traefik-xrealip-fixer/internal/providers"
	"github.com/ski-company/traefik-xrealip-fixer/internal/providers/cloudflare"
	"github.com/ski-company/traefik-xrealip-fixer/internal/providers/cloudfront"
)

// ServeHTTP is the middleware entrypoint.
func (ipFinder *Ipfinder) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	socketIPStr := helper.ParseSocketIP(req.RemoteAddr)
	matched := detectProvider(req)

	// Step 1: direct path (no provider hints)
	if matched == providers.Unknown {
		clientIP := ipFinder.directClientIP(req, socketIPStr)
		helper.CleanInboundForwardingHeaders(req.Header)
		ipFinder.applyTrusted(req, providers.Unknown, clientIP)
		ipFinder.next.ServeHTTP(rw, req)
		return
	}

	providerEdgeIPStr := ipFinder.providerEdgeIP(req, socketIPStr)
	providerEdgeIP := net.ParseIP(providerEdgeIPStr)

	helper.CleanInboundForwardingHeaders(req.Header)

	// Step 2: validate socket IP belongs to advertised provider
	if providerEdgeIP == nil || !ipFinder.isTrustedSocketIP(matched, providerEdgeIP) {
		ipFinder.rejectUntrusted(rw, req, providerEdgeIPStr)
		return
	}

	// Step 3: resolve client IP from provider header (fallback to socket)
	clientIP := ipFinder.resolveClientIP(matched, req, providerEdgeIPStr)
	ipFinder.applyTrusted(req, matched, clientIP)

	ipFinder.next.ServeHTTP(rw, req)
}

// detectProvider inspects headers to decide which provider hinted the request.
func detectProvider(req *http.Request) providers.Provider {
	switch {
	case req.Header.Get(cloudflare.ClientIPHeaderName) != "":
		return providers.Cloudflare
	case req.Header.Get(cloudfront.ClientIPHeaderName) != "":
		return providers.Cloudfront
	default:
		return providers.Unknown
	}
}

// directClientIP walks X-Forwarded-For from the tail using configured depth, falling back to socket.
func (ipFinder *Ipfinder) directClientIP(req *http.Request, socketIP string) string {
	xff := req.Header.Get(helper.XForwardedFor)
	if xff == "" || ipFinder.directDepth <= 0 {
		logger.LogWarn("Direct path: no XFF or invalid directDepth; using socket IP", "socketIP", socketIP)
		return socketIP
	}

	depth := ipFinder.directDepth
	if ip, ok, seen := ipFinder.scanXFFTail(xff, depth); ok {
		return ip
	} else {
		if seen < depth {
			logger.LogWarn(
				"Direct path: directDepth exceeds XFF length; using socket IP",
				"socketIP", socketIP,
				"directDepth", strconv.Itoa(depth),
				"xffLen", strconv.Itoa(seen),
			)
		} else {
			logger.LogWarn("Direct path: no valid IP found in XFF; using socket IP", "socketIP", socketIP)
		}
		return socketIP
	}
}

// providerEdgeIP picks the closest hop IP (tail of XFF) using directDepth, fallback to socket.
func (ipFinder *Ipfinder) providerEdgeIP(req *http.Request, socketIP string) string {
	xff := req.Header.Get(helper.XForwardedFor)
	if ip, ok, _ := ipFinder.scanXFFTail(xff, ipFinder.directDepth); ok {
		return ip
	}
	return socketIP
}

// scanXFFTail returns the last valid IP within the given depth from the end of XFF.
// ok=false if none found; seen returns how many hops were inspected.
func (ipFinder *Ipfinder) scanXFFTail(xff string, depth int) (ip string, ok bool, seen int) {
	if xff == "" || depth <= 0 {
		return "", false, 0
	}
	start := len(xff)
	for i := len(xff) - 1; i >= -1 && seen < depth; i-- {
		if i == -1 || xff[i] == ',' {
			token := strings.TrimSpace(xff[i+1 : start])
			seen++
			if token != "" {
				if candidate := helper.ExtractClientIP(token); net.ParseIP(candidate) != nil {
					return candidate, true, seen
				}
			}
			start = i
		}
	}
	return "", false, seen
}

// isTrustedSocketIP checks if the socket IP is inside the allowlist for the detected provider.
func (ipFinder *Ipfinder) isTrustedSocketIP(provider providers.Provider, socketIP net.IP) bool {
	if socketIP == nil {
		return false
	}
	switch provider {
	case providers.Cloudflare:
		return ipFinder.contains(providers.Cloudflare, socketIP)
	case providers.Cloudfront:
		return ipFinder.contains(providers.Cloudfront, socketIP)
	default:
		return false
	}
}

// resolveClientIP pulls the client IP from the provider header; falls back to the socket IP.
func (ipFinder *Ipfinder) resolveClientIP(provider providers.Provider, req *http.Request, fallback string) string {
	var headerName string
	switch provider {
	case providers.Cloudflare:
		headerName = cloudflare.ClientIPHeaderName
	case providers.Cloudfront:
		headerName = cloudfront.ClientIPHeaderName
	}
	if headerName != "" {
		if ip := helper.ExtractClientIP(req.Header.Get(headerName)); net.ParseIP(ip) != nil {
			return ip
		}
	}
	return fallback
}

// applyTrusted writes trusted headers and forwarding info.
func (ipFinder *Ipfinder) applyTrusted(req *http.Request, provider providers.Provider, clientIP string) {
	req.Header.Set(helper.XRealipFixerTrusted, "yes")
	switch provider {
	case providers.Cloudflare:
		req.Header.Set(helper.XRealipFixerProvider, "cloudflare")
	case providers.Cloudfront:
		req.Header.Set(helper.XRealipFixerProvider, "cloudfront")
	default:
		req.Header.Set(helper.XRealipFixerProvider, "direct")
	}
	helper.AppendXFF(req.Header, clientIP)
	req.Header.Set(helper.XRealIP, clientIP)
	if countryCode := lookupCountryCode(clientIP); countryCode != "" {
		req.Header.Set(helper.XCountry, countryCode)
	}
}

// rejectUntrusted clears spoofable headers and stops the chain.
func (ipFinder *Ipfinder) rejectUntrusted(rw http.ResponseWriter, req *http.Request, socketIP string) {
	logger.LogWarn("Untrusted request from", "remote", socketIP)
	req.Header.Set(helper.XRealipFixerTrusted, "no")
	req.Header.Set(helper.XRealipFixerProvider, "unknown")
	req.Header.Del(helper.XCountry)
	req.Header.Del(cloudflare.ClientIPHeaderName)
	req.Header.Del(cloudfront.ClientIPHeaderName)
	http.Error(rw, "You didn't say the magic word", http.StatusGone)
}
