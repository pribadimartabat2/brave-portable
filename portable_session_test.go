package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestInspectPortableProfileReportsPortableKeyStateWithoutReadingKey(t *testing.T) {
	root := t.TempDir()

	report := inspectPortableProfile(root)
	if report.PortableKeyPresent || report.PortableKeySizeValid {
		t.Fatalf("unexpected portable key state for missing key: %#v", report)
	}

	keyPath := filepath.Join(root, "Portable Encryption Key")
	if err := os.WriteFile(keyPath, []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	report = inspectPortableProfile(root)
	if !report.PortableKeyPresent {
		t.Fatal("expected portable key presence")
	}
	if report.PortableKeySizeValid {
		t.Fatal("malformed portable key must not be reported as size-valid")
	}

	if err := os.WriteFile(keyPath, make([]byte, 36), 0o600); err != nil {
		t.Fatal(err)
	}
	report = inspectPortableProfile(root)
	if !report.PortableKeyPresent || !report.PortableKeySizeValid {
		t.Fatalf("expected valid portable key metadata: %#v", report)
	}
	if report.PortableKeyBytesRead != 0 {
		t.Fatal("diagnostic must not read portable key contents")
	}
}

func TestValidatePortableRuntimePostflightRejectsStockOrBrokenEngine(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Local State"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validatePortableRuntimePostflight(root); err == nil {
		t.Fatal("expected missing portable key to fail postflight")
	}

	keyPath := filepath.Join(root, "Portable Encryption Key")
	if err := os.WriteFile(keyPath, []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validatePortableRuntimePostflight(root); err == nil {
		t.Fatal("expected malformed portable key to fail postflight")
	}

	if err := os.WriteFile(keyPath, make([]byte, 36), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validatePortableRuntimePostflight(root); err != nil {
		t.Fatalf("expected valid portable runtime postflight: %v", err)
	}
}

func TestSplitPortableArgsConsumesDiagnosticSwitch(t *testing.T) {
	browserArgs, diagnostic := splitPortableArgs([]string{
		"--incognito",
		portableDiagnosticSwitch,
		"https://example.com",
	})
	if !diagnostic {
		t.Fatal("expected diagnostic mode")
	}
	if len(browserArgs) != 2 || browserArgs[0] != "--incognito" || browserArgs[1] != "https://example.com" {
		t.Fatalf("unexpected forwarded browser args: %#v", browserArgs)
	}
}

func TestWritePortableSessionDiagnosticNeverSerializesSecretValues(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "Local State"),
		[]byte(`{"os_crypt":{"encrypted_key":"SECRET-DPAPI-BLOB","app_bound_encrypted_key":"SECRET-APP-BOUND-BLOB"},"profile":{"last_used":"Default"}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(t.TempDir(), "portable-session.json")
	if err := writePortableSessionDiagnostic(root, output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "SECRET-DPAPI-BLOB") || strings.Contains(text, "SECRET-APP-BOUND-BLOB") {
		t.Fatal("diagnostic serialized an encryption key value")
	}
	if !strings.Contains(text, `"has_encrypted_key": true`) || !strings.Contains(text, `"has_app_bound_encrypted_key": true`) {
		t.Fatalf("expected presence metadata in report: %s", text)
	}
}
