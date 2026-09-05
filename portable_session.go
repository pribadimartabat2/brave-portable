package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const portableDiagnosticSwitch = "--pamungkas-portability-diagnostic"

type localStateReport struct {
	HasEncryptedKey           bool   `json:"has_encrypted_key"`
	HasAppBoundEncryptedKey   bool   `json:"has_app_bound_encrypted_key"`
	LastUsedProfile           string `json:"last_used_profile,omitempty"`
	EncryptedKeyValue         string `json:"-"`
	AppBoundEncryptedKeyValue string `json:"-"`
}

type portableProfileReport struct {
	CookieDBPresent       bool `json:"cookie_db_present"`
	ExtensionsDirPresent bool `json:"extensions_dir_present"`
	LocalStatePresent    bool `json:"local_state_present"`
	CookieBytesRead      int  `json:"-"`
}

type portableSessionDiagnostic struct {
	Profile    portableProfileReport `json:"profile"`
	LocalState localStateReport      `json:"local_state"`
}

func splitPortableArgs(args []string) ([]string, bool) {
	browserArgs := make([]string, 0, len(args))
	diagnostic := false
	for _, arg := range args {
		if arg == portableDiagnosticSwitch {
			diagnostic = true
			continue
		}
		browserArgs = append(browserArgs, arg)
	}
	return browserArgs, diagnostic
}

func analyzeLocalState(path string) (localStateReport, error) {
	var report localStateReport

	f, err := os.Open(path)
	if err != nil {
		return report, err
	}
	defer f.Close()

	var state struct {
		OSCrypt map[string]json.RawMessage `json:"os_crypt"`
		Profile struct {
			LastUsed string `json:"last_used"`
		} `json:"profile"`
	}
	if err := json.NewDecoder(io.LimitReader(f, 16<<20)).Decode(&state); err != nil {
		return report, err
	}

	_, report.HasEncryptedKey = state.OSCrypt["encrypted_key"]
	_, report.HasAppBoundEncryptedKey = state.OSCrypt["app_bound_encrypted_key"]
	report.LastUsedProfile = state.Profile.LastUsed
	return report, nil
}

func inspectPortableProfile(root string) portableProfileReport {
	report := portableProfileReport{}

	if _, err := os.Stat(filepath.Join(root, "Local State")); err == nil {
		report.LocalStatePresent = true
	}

	profiles := []string{"Default"}
	if entries, err := os.ReadDir(root); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && len(entry.Name()) > len("Profile ") && entry.Name()[:len("Profile ")] == "Profile " {
				profiles = append(profiles, entry.Name())
			}
		}
	}

	for _, profile := range profiles {
		if _, err := os.Stat(filepath.Join(root, profile, "Network", "Cookies")); err == nil {
			report.CookieDBPresent = true
		}
		if info, err := os.Stat(filepath.Join(root, profile, "Extensions")); err == nil && info.IsDir() {
			report.ExtensionsDirPresent = true
		}
	}

	return report
}

func writePortableSessionDiagnostic(root, output string) error {
	report := portableSessionDiagnostic{Profile: inspectPortableProfile(root)}
	state, err := analyzeLocalState(filepath.Join(root, "Local State"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else {
		report.LocalState = state
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(output, data, 0o600)
}
