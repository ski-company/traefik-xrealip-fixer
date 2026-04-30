// Package geolite2 downloads MaxMind GeoLite2 Country databases.
package geolite2

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	CountryEditionID   = "GeoLite2-Country"
	DefaultDownloadURL = "https://download.maxmind.com/app/geoip_download"
	maxDatabaseBytes   = 128 << 20
)

var httpClient = &http.Client{Timeout: 2 * time.Minute}

// Config contains the MaxMind download settings.
type Config struct {
	LicenseKey  string
	DownloadURL string
}

// DownloadCountryDatabase fetches and extracts the GeoLite2 Country MMDB.
func DownloadCountryDatabase(cfg Config) ([]byte, error) {
	licenseKey := strings.TrimSpace(cfg.LicenseKey)
	if licenseKey == "" {
		return nil, errors.New("MaxMind GeoLite2 license key is required")
	}

	endpoint := strings.TrimSpace(cfg.DownloadURL)
	if endpoint == "" {
		endpoint = DefaultDownloadURL
	}

	downloadURL, err := countryDownloadURL(endpoint, licenseKey)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Get(downloadURL)
	if err != nil {
		return nil, fmt.Errorf("download GeoLite2 Country: %w", sanitizedDownloadError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("download GeoLite2 Country: unexpected status %s", resp.Status)
	}

	return ExtractCountryDatabase(resp.Body)
}

// ExtractCountryDatabase returns the first MMDB file found in a MaxMind tar.gz archive.
func ExtractCountryDatabase(r io.Reader) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("open GeoLite2 archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read GeoLite2 archive: %w", err)
		}
		if header == nil || header.Typeflag != tar.TypeReg {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(header.Name), ".mmdb") {
			continue
		}

		data, err := io.ReadAll(io.LimitReader(tr, maxDatabaseBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read GeoLite2 database: %w", err)
		}
		if len(data) > maxDatabaseBytes {
			return nil, fmt.Errorf("GeoLite2 database exceeds %d bytes", maxDatabaseBytes)
		}
		if len(data) == 0 {
			return nil, errors.New("GeoLite2 database is empty")
		}
		return data, nil
	}

	return nil, errors.New("GeoLite2 Country database not found in archive")
}

func countryDownloadURL(endpoint string, licenseKey string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse GeoLite2 download URL: %w", err)
	}

	q := u.Query()
	q.Set("edition_id", CountryEditionID)
	q.Set("license_key", licenseKey)
	q.Set("suffix", "tar.gz")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func sanitizedDownloadError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}

	clone := *urlErr
	clone.URL = redactLicenseKey(clone.URL)
	return &clone
}

func redactLicenseKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "<redacted>"
	}

	q := u.Query()
	if q.Has("license_key") {
		q.Set("license_key", "redacted")
		u.RawQuery = q.Encode()
	}

	return u.String()
}
