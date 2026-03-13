package git

import (
	"os"
	"testing"
)

// TestBuildDocAuth_NoneVariants exercises buildDocAuth for "none" and other fallback cases.
func TestBuildDocAuth_NoneVariants(t *testing.T) {
	tests := []struct {
		name     string
		authType string
	}{
		{"none", "none"},
		{"empty", ""},
		{"unknown falls through to nil", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := buildDocAuth(DocAuthConfig{AuthType: tt.authType})
			if err != nil {
				t.Fatalf("buildDocAuth(%q) error = %v", tt.authType, err)
			}
			if auth != nil {
				t.Errorf("buildDocAuth(%q) = %v, want nil", tt.authType, auth)
			}
		})
	}
}

func TestBuildDocAuth_SSHMissingKey(t *testing.T) {
	_, err := buildDocAuth(DocAuthConfig{AuthType: "ssh", SSHKeyPath: ""})
	if err == nil {
		t.Error("buildDocAuth ssh with empty key should error")
	}
}

func TestBuildDocAuth_SSHWithValidKey(t *testing.T) {
	keyPath := writeTempECKey(t)
	auth, err := buildDocAuth(DocAuthConfig{AuthType: "ssh", SSHKeyPath: keyPath})
	if err != nil {
		t.Fatalf("buildDocAuth ssh with valid key error = %v", err)
	}
	if auth == nil {
		t.Error("buildDocAuth ssh should return non-nil auth")
	}
}

func TestBuildDocAuth_SSHWithBadKey(t *testing.T) {
	badKey := t.TempDir() + "/bad_key"
	if err := os.WriteFile(badKey, []byte("not a pem"), 0600); err != nil {
		t.Fatalf("write bad key: %v", err)
	}
	_, err := buildDocAuth(DocAuthConfig{AuthType: "ssh", SSHKeyPath: badKey})
	if err == nil {
		t.Error("buildDocAuth ssh with bad key should error")
	}
}

func TestBuildDocAuth_HTTPSMissingToken(t *testing.T) {
	_, err := buildDocAuth(DocAuthConfig{AuthType: "https", HTTPToken: ""})
	if err == nil {
		t.Error("buildDocAuth https with empty token should error")
	}
}

func TestBuildDocAuth_HTTPSWithToken(t *testing.T) {
	auth, err := buildDocAuth(DocAuthConfig{AuthType: "https", HTTPToken: "tok"})
	if err != nil {
		t.Fatalf("buildDocAuth https error = %v", err)
	}
	if auth == nil {
		t.Error("buildDocAuth https should return non-nil auth")
	}
}
