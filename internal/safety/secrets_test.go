package safety

import (
	"encoding/base64"
	"testing"
)

func TestRedactSecretsFromResponse_NoSecrets(t *testing.T) {
	text := "The deployment has 3 replicas running in namespace payments."
	result, changed := RedactSecretsFromResponse(text, nil)
	if changed {
		t.Error("should not report changed when no secrets provided")
	}
	if result != text {
		t.Errorf("text should be unchanged, got %q", result)
	}
}

func TestRedactSecretsFromResponse_EmptyText(t *testing.T) {
	result, changed := RedactSecretsFromResponse("", []string{"password123"})
	if changed {
		t.Error("should not report changed for empty text")
	}
	if result != "" {
		t.Errorf("should return empty string, got %q", result)
	}
}

func TestRedactSecretsFromResponse_ContainsRawValue(t *testing.T) {
	text := "The database password is supersecret and the connection string is postgres://admin:supersecret@db:5432"
	result, changed := RedactSecretsFromResponse(text, []string{"supersecret"})
	if !changed {
		t.Error("should report changed when secret value found")
	}
	if result == text {
		t.Error("text should be modified")
	}
	if contains(result, "supersecret") {
		t.Errorf("result should not contain secret value, got %q", result)
	}
	if !contains(result, secretRedactionMarker) {
		t.Errorf("result should contain redaction marker, got %q", result)
	}
}

func TestRedactSecretsFromResponse_ContainsBase64Value(t *testing.T) {
	secret := "hunter2"
	encoded := base64.StdEncoding.EncodeToString([]byte(secret))
	text := "The base64-encoded secret is " + encoded + " in the config."

	result, changed := RedactSecretsFromResponse(text, []string{secret})
	if !changed {
		t.Error("should report changed when base64 secret value found")
	}
	if contains(result, encoded) {
		t.Errorf("result should not contain base64-encoded secret, got %q", result)
	}
}

func TestRedactSecretsFromResponse_MetadataNotRedacted(t *testing.T) {
	text := "The secret db-credentials in namespace payments has keys: DB_PASSWORD, DB_USER"
	result, changed := RedactSecretsFromResponse(text, []string{"actual-password-value"})
	if changed {
		t.Error("should not redact when secret values are not in text")
	}
	if result != text {
		t.Errorf("metadata text should be unchanged, got %q", result)
	}
}

func TestRedactSecretsFromResponse_MultipleSecrets(t *testing.T) {
	text := "password is hunter2 and api key is sk-live-abc123"
	result, changed := RedactSecretsFromResponse(text, []string{"hunter2", "sk-live-abc123"})
	if !changed {
		t.Error("should report changed")
	}
	if contains(result, "hunter2") || contains(result, "sk-live-abc123") {
		t.Errorf("all secret values should be redacted, got %q", result)
	}
}

func TestRedactSecretsFromResponse_EmptySecretValue(t *testing.T) {
	text := "some text"
	result, changed := RedactSecretsFromResponse(text, []string{""})
	if changed {
		t.Error("empty secret value should be skipped")
	}
	if result != text {
		t.Errorf("text should be unchanged, got %q", result)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
