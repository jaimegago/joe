package git

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
)

// --- buildAuth internal tests ---

func TestBuildAuth_None(t *testing.T) {
	cfg := Config{AuthType: "none"}
	auth, err := buildAuth(cfg)
	if err != nil {
		t.Fatalf("buildAuth(none) error = %v", err)
	}
	if auth != nil {
		t.Error("buildAuth(none) should return nil auth")
	}
}

func TestBuildAuth_EmptyAuthType(t *testing.T) {
	cfg := Config{AuthType: ""}
	auth, err := buildAuth(cfg)
	if err != nil {
		t.Fatalf("buildAuth(\"\") error = %v", err)
	}
	if auth != nil {
		t.Error("buildAuth(\"\") should return nil auth")
	}
}

func TestBuildAuth_SSHMissingKey(t *testing.T) {
	cfg := Config{AuthType: "ssh", SSHKeyPath: ""}
	_, err := buildAuth(cfg)
	if err == nil {
		t.Error("buildAuth ssh with empty key path should return error")
	}
}

func TestBuildAuth_SSHWithValidKey(t *testing.T) {
	keyPath := writeTempECKey(t)
	cfg := Config{AuthType: "ssh", SSHKeyPath: keyPath}
	auth, err := buildAuth(cfg)
	if err != nil {
		t.Fatalf("buildAuth ssh with valid key error = %v", err)
	}
	if auth == nil {
		t.Error("buildAuth ssh should return non-nil auth")
	}
}

func TestBuildAuth_SSHWithBadKey(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "badkey")
	if err := os.WriteFile(bad, []byte("not a pem key"), 0600); err != nil {
		t.Fatalf("write bad key: %v", err)
	}
	cfg := Config{AuthType: "ssh", SSHKeyPath: bad}
	_, err := buildAuth(cfg)
	if err == nil {
		t.Error("buildAuth ssh with bad key content should return error")
	}
}

func TestBuildAuth_HTTPSMissingToken(t *testing.T) {
	cfg := Config{AuthType: "https", HTTPToken: ""}
	_, err := buildAuth(cfg)
	if err == nil {
		t.Error("buildAuth https with empty token should return error")
	}
}

func TestBuildAuth_HTTPSWithToken(t *testing.T) {
	cfg := Config{AuthType: "https", HTTPToken: "mytoken"}
	auth, err := buildAuth(cfg)
	if err != nil {
		t.Fatalf("buildAuth https error = %v", err)
	}
	if auth == nil {
		t.Error("buildAuth https should return non-nil auth")
	}
}

func TestBuildAuth_Unknown(t *testing.T) {
	cfg := Config{AuthType: "kerberos"}
	_, err := buildAuth(cfg)
	if err == nil {
		t.Error("buildAuth with unknown auth type should return error")
	}
	if !strings.Contains(err.Error(), "unknown auth_type") {
		t.Errorf("error should mention unknown auth_type, got: %v", err)
	}
}

// --- repoDir ---

func TestRepoDir_Success(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := repoDir("https://github.com/example/repo.git")
	if err != nil {
		t.Fatalf("repoDir() error = %v", err)
	}
	if path == "" {
		t.Error("repoDir() should return non-empty path")
	}
}

// TestResolveCommit_HeadError tests resolveCommit("HEAD") on an empty repo (no commits).
func TestResolveCommit_HeadError(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init empty repo: %v", err)
	}
	a := &Adapter{repo: repo, connected: true}
	_, err = a.resolveCommit("HEAD")
	if err == nil {
		t.Error("resolveCommit(HEAD) on empty repo should return error")
	}
}

// TestLog_EmptyRepo tests Log on an empty repo where repo.Log fails.
func TestLog_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init empty repo: %v", err)
	}
	a := &Adapter{repo: repo, connected: true}
	_, err = a.Log(context.Background(), 10)
	if err == nil {
		t.Error("Log() on empty repo should return error")
	}
}

// TestReadFile_EmptyRepo tests ReadFile on an empty repo where Head fails.
func TestReadFile_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init empty repo: %v", err)
	}
	a := &Adapter{repo: repo, connected: true}
	_, err = a.ReadFile(context.Background(), "README.md")
	if err == nil {
		t.Error("ReadFile() on empty repo should return error")
	}
}

// TestListFiles_EmptyRepo tests ListFiles on an empty repo where Head fails.
func TestListFiles_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init empty repo: %v", err)
	}
	a := &Adapter{repo: repo, connected: true}
	_, err = a.ListFiles(context.Background(), "")
	if err == nil {
		t.Error("ListFiles() on empty repo should return error")
	}
}

// writeTempECKey generates an EC private key and writes it as PEM.
func writeTempECKey(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	block := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	path := filepath.Join(t.TempDir(), "id_ecdsa")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatalf("create key file: %v", err)
	}
	defer f.Close()
	if err := pem.Encode(f, block); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}
