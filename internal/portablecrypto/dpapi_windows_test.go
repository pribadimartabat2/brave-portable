package portablecrypto

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testMasterKey() []byte {
	key := make([]byte, masterKeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func writeTestLocalState(t *testing.T, path string, master []byte) []byte {
	t.Helper()
	wrapped, err := protectDPAPI(master)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(append(append([]byte{}, dpapiPrefix...), wrapped...))
	state := map[string]any{
		"browser": map[string]any{"preserve_me": "yes"},
		"os_crypt": map[string]any{
			"audit_enabled": true,
			"encrypted_key": encoded,
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDPAPIProtectUnprotectRoundTrip(t *testing.T) {
	master := testMasterKey()
	wrapped, err := protectDPAPI(master)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := unprotectDPAPI(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, master) {
		t.Fatal("DPAPI round trip changed master key")
	}
}

func TestPortableVaultRoundTripRejectsCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, vaultFilename)
	master := testMasterKey()

	if err := storeVault(path, master); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadVault(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded, master) {
		t.Fatal("vault changed master key")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(vaultMagic)+3] ^= 0xff
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadVault(path); !errors.Is(err, ErrVaultCorrupt) {
		t.Fatalf("expected ErrVaultCorrupt, got %v", err)
	}
}

func TestRewriteLocalStatePreservesOtherDataAndMasterKey(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, localStateFilename)
	master := testMasterKey()
	writeTestLocalState(t, statePath, master)

	if err := rewriteLocalStateForCurrentWindows(statePath, master); err != nil {
		t.Fatal(err)
	}

	loaded, err := decryptMasterKeyFromLocalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded, master) {
		t.Fatal("rewrap changed Chromium master key")
	}

	var state struct {
		Browser struct {
			PreserveMe string `json:"preserve_me"`
		} `json:"browser"`
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Browser.PreserveMe != "yes" {
		t.Fatalf("unrelated Local State data was not preserved: %q", state.Browser.PreserveMe)
	}
}

func TestPrepareExistingProfileCreatesVault(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	securityDir := filepath.Join(root, "security")
	master := testMasterKey()
	writeTestLocalState(t, filepath.Join(dataDir, localStateFilename), master)

	status, err := PrepareProfile(dataDir, securityDir)
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusReady {
		t.Fatalf("expected ready, got %v", status)
	}
	loaded, err := loadVault(filepath.Join(securityDir, vaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded, master) {
		t.Fatal("captured vault master key differs from Chromium key")
	}
}

func TestPrepareWithVaultRewrapsSameKey(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	securityDir := filepath.Join(root, "security")
	statePath := filepath.Join(dataDir, localStateFilename)
	master := testMasterKey()
	writeTestLocalState(t, statePath, master)
	if err := os.MkdirAll(securityDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := storeVault(filepath.Join(securityDir, vaultFilename), master); err != nil {
		t.Fatal(err)
	}

	status, err := PrepareProfile(dataDir, securityDir)
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusReady {
		t.Fatalf("expected ready, got %v", status)
	}
	loaded, err := decryptMasterKeyFromLocalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded, master) {
		t.Fatal("rewrapped Local State does not contain portable master key")
	}
}

func TestPrepareBlocksForeignProfileWithoutVaultWithoutMutation(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	securityDir := filepath.Join(root, "security")
	statePath := filepath.Join(dataDir, localStateFilename)
	badWrapped := append(append([]byte{}, dpapiPrefix...), []byte("not-valid-dpapi-on-this-user")...)
	encoded := base64.StdEncoding.EncodeToString(badWrapped)
	original := []byte(`{"browser":{"keep":1},"os_crypt":{"encrypted_key":"` + encoded + `"}}`)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, original, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := PrepareProfile(dataDir, securityDir)
	if !errors.Is(err, ErrForeignProfileWithoutVault) {
		t.Fatalf("expected ErrForeignProfileWithoutVault, got %v", err)
	}
	after, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, original) {
		t.Fatal("foreign profile Local State was mutated before a portable vault existed")
	}
}

func TestFreshProfileBootstrapsThenCaptures(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	securityDir := filepath.Join(root, "security")

	status, err := PrepareProfile(dataDir, securityDir)
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusBootstrapNeeded {
		t.Fatalf("expected bootstrap-needed, got %v", status)
	}

	master := testMasterKey()
	writeTestLocalState(t, filepath.Join(dataDir, localStateFilename), master)
	if err := CaptureProfileKey(dataDir, securityDir); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadVault(filepath.Join(securityDir, vaultFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded, master) {
		t.Fatal("bootstrap capture changed master key")
	}
}
