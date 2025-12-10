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
	hasCFHeader := req.Header.Get(cloudflare.ClientIPHeaderName) != ""
	hasCFNHeader := req.Header.Get(cloudfront.ClientIPHeaderName) != ""

	// Step 1: detect provider from headers, otherwise direct path
	matched := providers.Unknown
	if hasCFHeader {
		matched = providers.Cloudflare
	} else if hasCFNHeader {
		matched = providers.Cloudfront
	}

	// Fast-path direct: no provider hints at all
	if matched == providers.Unknown {
		// Walk X-Forwarded-For from the end according to directDepth.
		xff := req.Header.Get(helper.XForwardFor)
		clientIP := socketIP
		if xff != "" && ipFinder.directDepth > 0 {
			parts := strings.Split(xff, ",")
			for i := len(parts) - 1; i >= 0 && (len(parts)-1-i) < ipFinder.directDepth; i-- {
				candidate := helper.ExtractClientIP(parts[i])
				if net.ParseIP(candidate) != nil {
					clientIP = candidate
					break
				}
			}
		}

		helper.CleanInboundForwardingHeaders(req.Header)
		req.Header.Set(helper.XRealipFixerTrusted, "yes")
		req.Header.Set(helper.XRealipFixerProvider, "direct")
		helper.AppendXFF(req.Header, clientIP)
		req.Header.Set(helper.XRealIP, clientIP)
		ipFinder.next.ServeHTTP(rw, req)
		return
	}

	helper.CleanInboundForwardingHeaders(req.Header)

	// Step 2: check socket IP matches the advertised provider
	trusted := false
	clientIPHeaderName := ""
	switch matched {
	case providers.Cloudflare:
		trusted = ipFinder.contains(providers.Cloudflare, net.ParseIP(socketIP))
		clientIPHeaderName = cloudflare.ClientIPHeaderName
		req.Header.Set(helper.XRealipFixerProvider, "cloudflare")
	case providers.Cloudfront:
		trusted = ipFinder.contains(providers.Cloudfront, net.ParseIP(socketIP))
		clientIPHeaderName = cloudfront.ClientIPHeaderName
		req.Header.Set(helper.XRealipFixerProvider, "cloudfront")
	}

	if trusted {
		req.Header.Set(helper.XRealipFixerTrusted, "yes")

		var clientIP string
		if clientIPHeaderName != "" {
			clientIP = helper.ExtractClientIP(req.Header.Get(clientIPHeaderName))
			if net.ParseIP(clientIP) == nil {
				clientIP = ""
			}
		}
		if clientIP == "" {
			clientIP = socketIP
		}

		helper.AppendXFF(req.Header, clientIP)
		req.Header.Set(helper.XRealIP, clientIP)
	} else {
		logger.LogWarn("Untrusted request from", "remote", socketIP)
		req.Header.Set(helper.XRealipFixerTrusted, "no")
		req.Header.Set(helper.XRealipFixerProvider, "unknown")

		// Drop any spoofed provider headers on untrusted requests
		req.Header.Del(cloudflare.ClientIPHeaderName)
		req.Header.Del(cloudfront.ClientIPHeaderName)

		http.Error(rw, "You didn't say the magic word", http.StatusGone)
		return
	}

	ipFinder.next.ServeHTTP(rw, req)
}
