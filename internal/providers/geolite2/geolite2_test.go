package geolite2

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestExtractCountryDatabase(t *testing.T) {
	want := []byte("mmdb-bytes")
	archive := geoLite2Archive(t, map[string][]byte{
		"GeoLite2-Country_20260429/LICENSE.txt":           []byte("license"),
		"GeoLite2-Country_20260429/GeoLite2-Country.mmdb": want,
	})

	got, err := ExtractCountryDatabase(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("ExtractCountryDatabase() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ExtractCountryDatabase() = %q, want %q", got, want)
	}
}

func TestExtractCountryDatabaseMissingMMDB(t *testing.T) {
	archive := geoLite2Archive(t, map[string][]byte{
		"GeoLite2-Country_20260429/LICENSE.txt": []byte("license"),
	})

	if _, err := ExtractCountryDatabase(bytes.NewReader(archive)); err == nil {
		t.Fatal("ExtractCountryDatabase() error = nil, want error")
	}
}

func TestSanitizedDownloadErrorRedactsLicenseKey(t *testing.T) {
	err := &url.Error{
		Op:  "Get",
		URL: "https://download.maxmind.com/app/geoip_download?edition_id=GeoLite2-Country&license_key=secret-key&suffix=tar.gz",
		Err: errors.New("network down"),
	}

	got := sanitizedDownloadError(err).Error()
	if strings.Contains(got, "secret-key") {
		t.Fatalf("sanitizedDownloadError() leaked license key: %q", got)
	}
	if !strings.Contains(got, "license_key=redacted") {
		t.Fatalf("sanitizedDownloadError() = %q, want redacted license key", got)
	}
}

func geoLite2Archive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o600,
			Size: int64(len(data)),
		}); err != nil {
			t.Fatalf("WriteHeader() error = %v", err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close() error = %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip Close() error = %v", err)
	}

	return buf.Bytes()
}
