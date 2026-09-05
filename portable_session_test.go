package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeLocalStateReportsEncryptionMetadataWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Local State")
	data := `{"os_crypt":{"encrypted_key":"SECRET-DPAPI-BLOB","app_bound_encrypted_key":"SECRET-APP-BOUND-BLOB"},"profile":{"last_used":"Default"}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := analyzeLocalState(path)
	if err != nil {
		t.Fatal(err)
	}
	if !report.HasEncryptedKey {
		t.Fatal("expected encrypted_key metadata")
	}
	if !report.HasAppBoundEncryptedKey {
		t.Fatal("expected app_bound_encrypted_key metadata")
	}
	if report.LastUsedProfile != "Default" {
		t.Fatalf("unexpected last profile: %q", report.LastUsedProfile)
	}
	if report.EncryptedKeyValue != "" || report.AppBoundEncryptedKeyValue != "" {
		t.Fatal("diagnostic must never expose encryption key values")
	}
}

func TestInspectPortableProfileReportsOnlyPresence(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Default", "Network"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Default", "Network", "Cookies"), []byte("do-not-read"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Default", "Extensions"), 0o755); err != nil {
		t.Fatal(err)
	}

	report := inspectPortableProfile(root)
	if !report.CookieDBPresent {
		t.Fatal("expected cookie database presence")
	}
	if !report.ExtensionsDirPresent {
		t.Fatal("expected extensions directory presence")
	}
	if report.CookieBytesRead != 0 {
		t.Fatal("diagnostic must not read cookie database contents")
	}
}
