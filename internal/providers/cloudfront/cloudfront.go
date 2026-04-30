// Package ips contains a list of current cloud flare IP ranges
package cloudfront

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/ski-company/traefik-xrealip-fixer/internal/logger"
)

const (
	requestTimeout = 10 * time.Second
	maxBodyBytes   = 1 << 20
)

var httpClient = &http.Client{Timeout: requestTimeout}

// CFIPs is the CloudFlare Server IP list (this is checked on build).
func TrustedIPS() []string {

	// Found at https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/LocationsOfEdgeServers.html
	url := "https://d7uri8nf7uskq.cloudfront.net/tools/list-cloudfront-ips"

	resp, err := httpClient.Get(url)
	if err != nil {
		logger.LogWarn("CloudFront IP list fetch failed", "url", url, "error", err.Error())
		return nil
	}
	defer resp.Body.Close() // Ensure the response body is closed

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		logger.LogWarn("CloudFront IP list fetch returned unexpected status", "url", url, "status", resp.Status)
		return nil
	}

	// Read the response body
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		logger.LogWarn("CloudFront IP list read failed", "url", url, "error", err.Error())
		return nil
	}
	if len(body) > maxBodyBytes {
		logger.LogWarn("CloudFront IP list response too large", "url", url)
		return nil
	}
	// Define a map to hold the JSON data
	var data map[string][]string

	// Parse the JSON response
	err = json.Unmarshal(body, &data)
	if err != nil {
		logger.LogWarn("CloudFront IP list JSON parse failed", "url", url, "error", err.Error())
		return nil
	}

	// Extract the arrays
	globalIPList, globalExists := data["CLOUDFRONT_GLOBAL_IP_LIST"]
	regionalIPList, regionalExists := data["CLOUDFRONT_REGIONAL_EDGE_IP_LIST"]

	if !globalExists && !regionalExists {
		logger.LogWarn("CloudFront IP list response missing expected keys", "url", url)
		return nil
	}

	// Merge the arrays
	mergedIPList := make([]string, 0, len(globalIPList)+len(regionalIPList))
	mergedIPList = append(mergedIPList, globalIPList...)
	mergedIPList = append(mergedIPList, regionalIPList...)

	return mergedIPList
}

const ClientIPHeaderName = "Cloudfront-Viewer-Address"
