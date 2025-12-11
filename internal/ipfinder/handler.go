package ipfinder

import (
	"net"
	"net/http"
	"strings"

	"github.com/ski-company/traefik-xrealip-fixer/internal/helper"
	"github.com/ski-company/traefik-xrealip-fixer/internal/logger"
	"github.com/ski-company/traefik-xrealip-fixer/internal/providers"
	"github.com/ski-company/traefik-xrealip-fixer/internal/providers/cloudflare"
	"github.com/ski-company/traefik-xrealip-fixer/internal/providers/cloudfront"
)

// ServeHTTP is the middleware entrypoint.
func (ipFinder *Ipfinder) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	socketIP := helper.ParseSocketIP(req.RemoteAddr)
	matched := detectProvider(req)

	// Step 1: direct path (no provider hints)
	if matched == providers.Unknown {
		clientIP := ipFinder.directClientIP(req, socketIP)
		helper.CleanInboundForwardingHeaders(req.Header)
		ipFinder.applyTrusted(req, providers.Unknown, clientIP)
		ipFinder.next.ServeHTTP(rw, req)
		return
	}

	helper.CleanInboundForwardingHeaders(req.Header)

	// Step 2: validate socket IP belongs to advertised provider
	if !ipFinder.isTrustedSocketIP(matched, socketIP) {
		ipFinder.rejectUntrusted(rw, req, socketIP)
		return
	}

	// Step 3: resolve client IP from provider header (fallback to socket)
	clientIP := ipFinder.resolveClientIP(matched, req, socketIP)
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
	xff := req.Header.Get(helper.XForwardFor)
	if xff == "" || ipFinder.directDepth <= 0 {
		return socketIP
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0 && (len(parts)-1-i) < ipFinder.directDepth; i-- {
		candidate := helper.ExtractClientIP(parts[i])
		if net.ParseIP(candidate) != nil {
			return candidate
		}
	}
	return socketIP
}

// isTrustedSocketIP checks if the socket IP is inside the allowlist for the detected provider.
func (ipFinder *Ipfinder) isTrustedSocketIP(provider providers.Provider, socketIP string) bool {
	ip := net.ParseIP(socketIP)
	if ip == nil {
		return false
	}
	switch provider {
	case providers.Cloudflare:
		return ipFinder.contains(providers.Cloudflare, ip)
	case providers.Cloudfront:
		return ipFinder.contains(providers.Cloudfront, ip)
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
}

// rejectUntrusted clears spoofable headers and stops the chain.
func (ipFinder *Ipfinder) rejectUntrusted(rw http.ResponseWriter, req *http.Request, socketIP string) {
	logger.LogWarn("Untrusted request from", "remote", socketIP)
	req.Header.Set(helper.XRealipFixerTrusted, "no")
	req.Header.Set(helper.XRealipFixerProvider, "unknown")
	req.Header.Del(cloudflare.ClientIPHeaderName)
	req.Header.Del(cloudfront.ClientIPHeaderName)
	http.Error(rw, "You didn't say the magic word", http.StatusGone)
}
