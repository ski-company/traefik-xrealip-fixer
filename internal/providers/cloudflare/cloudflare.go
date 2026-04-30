// Package cloudflare contains a list of current Cloudflare IP ranges
package cloudflare

import (
	"bufio"
	"net/http"
	"time"

	"github.com/ski-company/traefik-xrealip-fixer/internal/logger"
)

const requestTimeout = 10 * time.Second

// TrustedIPS fetches Cloudflare's current IP ranges (IPv4 + IPv6).
func TrustedIPS() []string {
	urls := []string{
		"https://www.cloudflare.com/ips-v4",
		"https://www.cloudflare.com/ips-v6",
	}

	var ipList []string
	client := http.Client{Timeout: requestTimeout}
	for _, url := range urls {
		resp, err := client.Get(url)
		if err != nil {
			logger.LogWarn("Cloudflare IP list fetch failed", "url", url, "error", err.Error())
			continue
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			logger.LogWarn("Cloudflare IP list fetch returned unexpected status", "url", url, "status", resp.Status)
			_ = resp.Body.Close()
			continue
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			ip := scanner.Text()
			if ip != "" {
				ipList = append(ipList, ip)
			}
		}

		if err := scanner.Err(); err != nil {
			logger.LogWarn("Cloudflare IP list read failed", "url", url, "error", err.Error())
		}
		_ = resp.Body.Close()
	}

	return ipList
}

const ClientIPHeaderName = "CF-Connecting-IP"
