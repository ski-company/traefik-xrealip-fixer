package helper

import (
	"net"
	"net/http"
	"strconv"
	"strings"
)

const (
	XRealIP              = "X-Real-Ip"
	XForwardFor          = "X-Forwarded-For"
	Forwarded            = "Forwarded"
	XRealipFixerTrusted  = "X-Realip-Fixer-Trusted"
	XRealipFixerProvider = "X-Realip-Fixer-Provider"
)

// cleanInboundForwardingHeaders removes spoofable forwarding headers.
// We always set trusted values ourselves after validation.
func CleanInboundForwardingHeaders(h http.Header) {
	h.Del(XRealIP)
	h.Del(XForwardFor)
	h.Del(Forwarded)
	h.Del(XRealipFixerTrusted)
	h.Del(XRealipFixerProvider)
}

// appendXFF appends client to X-Forwarded-For per common proxy behavior.
func AppendXFF(h http.Header, client string) {
	if client == "" {
		return
	}
	if prior := h.Get(XForwardFor); prior != "" {
		h.Set(XForwardFor, prior+", "+client)
	} else {
		h.Set(XForwardFor, client)
	}
}

// parseSocketIP extracts the remote IP from a net/http RemoteAddr string (ip:port or [ip]:port).
func ParseSocketIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil && host != "" {
		return host
	}
	return remoteAddr
}

// extractClientIP tries to normalize a header value that might be "ip:port" or just "ip".
func ExtractClientIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if host, _, err := net.SplitHostPort(raw); err == nil && host != "" {
		return host
	}

	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		return strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]")
	}

	if ip := net.ParseIP(raw); ip != nil {
		return raw
	}

	if i := strings.LastIndexByte(raw, ':'); i > 0 {
		hostPart := raw[:i]
		portPart := raw[i+1:]
		if _, err := strconv.Atoi(portPart); err == nil {
			if ip := net.ParseIP(hostPart); ip != nil {
				return hostPart
			}
		}
	}

	return ""
}
