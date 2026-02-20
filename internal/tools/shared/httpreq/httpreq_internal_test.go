// Internal white-box tests for unexported helpers in httpreq.
// Uses package httpreq (not httpreq_test) to access unexported functions.
package httpreq

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"
)

func TestExtractTLSInfo_Valid(t *testing.T) {
	notAfter := time.Now().Add(30 * 24 * time.Hour)
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.example.com"},
		Issuer:       pkix.Name{CommonName: "Test CA"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}

	state := &tls.ConnectionState{
		Version:          tls.VersionTLS13,
		PeerCertificates: []*x509.Certificate{cert},
	}

	info := extractTLSInfo(state)
	if info == nil {
		t.Fatal("extractTLSInfo() returned nil for valid state")
	}
	if info.Version != "TLS 1.3" {
		t.Errorf("Version = %q, want TLS 1.3", info.Version)
	}
	if info.Subject != "test.example.com" {
		t.Errorf("Subject = %q, want test.example.com", info.Subject)
	}
	if info.Issuer != "Test CA" {
		t.Errorf("Issuer = %q, want Test CA", info.Issuer)
	}
	if info.DaysUntilEx <= 0 {
		t.Errorf("DaysUntilEx = %d, want > 0", info.DaysUntilEx)
	}
}

func TestExtractTLSInfo_NoCerts(t *testing.T) {
	state := &tls.ConnectionState{
		Version:          tls.VersionTLS12,
		PeerCertificates: nil,
	}
	info := extractTLSInfo(state)
	if info != nil {
		t.Errorf("extractTLSInfo(no certs) = %v, want nil", info)
	}
}

func TestExtractTLSInfo_AllTLSVersions(t *testing.T) {
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.com"},
		Issuer:       pkix.Name{CommonName: "CA"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}

	tests := []struct {
		version uint16
		want    string
	}{
		{tls.VersionTLS10, "TLS 1.0"},
		{tls.VersionTLS11, "TLS 1.1"},
		{tls.VersionTLS12, "TLS 1.2"},
		{tls.VersionTLS13, "TLS 1.3"},
		{0x0200, "0x0200"}, // unknown version → hex fallback
	}

	for _, tt := range tests {
		state := &tls.ConnectionState{
			Version:          tt.version,
			PeerCertificates: []*x509.Certificate{cert},
		}
		info := extractTLSInfo(state)
		if info == nil {
			t.Fatalf("extractTLSInfo(version=0x%04X) returned nil", tt.version)
		}
		if info.Version != tt.want {
			t.Errorf("Version(0x%04X) = %q, want %q", tt.version, info.Version, tt.want)
		}
	}
}

func TestExtractHost_WithPort(t *testing.T) {
	host := extractHost("http://example.com:8080/path")
	if host != "example.com" {
		t.Errorf("extractHost with port = %q, want example.com", host)
	}
}

func TestExtractHost_WithoutPort(t *testing.T) {
	host := extractHost("https://example.com/path")
	if host != "example.com" {
		t.Errorf("extractHost without port = %q, want example.com", host)
	}
}

func TestExtractHost_Empty(t *testing.T) {
	host := extractHost("")
	_ = host // should not panic
}

func TestCheckSSRF_Localhost(t *testing.T) {
	// localhost is allowed (useful for local service checks).
	err := checkSSRF("http://localhost:8080/health")
	if err != nil {
		t.Errorf("checkSSRF(localhost) = %v, want nil (localhost is allowed)", err)
	}
}

func TestCheckSSRF_NormalHost(t *testing.T) {
	err := checkSSRF("https://api.example.com/v1/status")
	if err != nil {
		t.Errorf("checkSSRF(normal host) = %v, want nil", err)
	}
}
