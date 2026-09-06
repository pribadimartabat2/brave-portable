package portablecrypto

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	masterKeySize      = 32
	localStateFilename = "Local State"
	vaultFilename      = "portable-dpapi-master.pdp"
)

var (
	dpapiPrefix = []byte("DPAPI")
	vaultMagic  = []byte("PDP1")

	ErrVaultCorrupt               = errors.New("portable DPAPI vault is corrupt")
	ErrForeignProfileWithoutVault = errors.New("profile key belongs to another Windows context and no portable vault exists")
	errDPAPIUnprotect             = errors.New("DPAPI could not decrypt Chromium master key")
	errEncryptedKeyMissing        = errors.New("Local State does not contain os_crypt.encrypted_key")
)

type PrepareStatus int

const (
	StatusReady PrepareStatus = iota
	StatusBootstrapNeeded
)

func zeroBytes(data []byte) {
	for i := range data {
		data[i] = 0
	}
}

func dataBlob(data []byte) windows.DataBlob {
	if len(data) == 0 {
		return windows.DataBlob{}
	}
	return windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
}

func copyAndFreeBlob(blob *windows.DataBlob) []byte {
	if blob == nil || blob.Data == nil || blob.Size == 0 {
		return nil
	}
	defer func() {
		_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(blob.Data)))
	}()
	return append([]byte(nil), unsafe.Slice(blob.Data, int(blob.Size))...)
}

func protectDPAPI(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, errors.New("cannot DPAPI-protect empty data")
	}
	input := dataBlob(plain)
	var output windows.DataBlob
	if err := windows.CryptProtectData(&input, nil, nil, 0, nil, 0, &output); err != nil {
		return nil, fmt.Errorf("DPAPI protect failed: %w", err)
	}
	protected := copyAndFreeBlob(&output)
	if len(protected) == 0 {
		return nil, errors.New("DPAPI protect returned empty output")
	}
	return protected, nil
}

func unprotectDPAPI(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, fmt.Errorf("%w: empty ciphertext", errDPAPIUnprotect)
	}
	input := dataBlob(ciphertext)
	var output windows.DataBlob
	if err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, 0, &output); err != nil {
		return nil, fmt.Errorf("%w: %v", errDPAPIUnprotect, err)
	}
	plain := copyAndFreeBlob(&output)
	if len(plain) == 0 {
		return nil, fmt.Errorf("%w: empty plaintext", errDPAPIUnprotect)
	}
	return plain, nil
}

func vaultPayload(master []byte) ([]byte, error) {
	if len(master) != masterKeySize {
		return nil, fmt.Errorf("portable master key must be %d bytes, got %d", masterKeySize, len(master))
	}
	payload := make([]byte, 0, len(vaultMagic)+masterKeySize+sha256.Size)
	payload = append(payload, vaultMagic...)
	payload = append(payload, master...)
	digest := sha256.Sum256(payload)
	payload = append(payload, digest[:]...)
	return payload, nil
}

func loadVault(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	expectedLen := len(vaultMagic) + masterKeySize + sha256.Size
	if len(data) != expectedLen || !bytes.Equal(data[:len(vaultMagic)], vaultMagic) {
		return nil, ErrVaultCorrupt
	}
	bodyEnd := len(vaultMagic) + masterKeySize
	expectedDigest := sha256.Sum256(data[:bodyEnd])
	if !bytes.Equal(data[bodyEnd:], expectedDigest[:]) {
		return nil, ErrVaultCorrupt
	}
	master := append([]byte(nil), data[len(vaultMagic):bodyEnd]...)
	return master, nil
}

func storeVault(path string, master []byte) error {
	payload, err := vaultPayload(master)
	if err != nil {
		return err
	}
	defer zeroBytes(payload)

	if existing, err := loadVault(path); err == nil {
		defer zeroBytes(existing)
		if !bytes.Equal(existing, master) {
			return errors.New("portable DPAPI vault contains a different master key")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create portable security directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			existing, loadErr := loadVault(path)
			if loadErr != nil {
				return loadErr
			}
			defer zeroBytes(existing)
			if !bytes.Equal(existing, master) {
				return errors.New("portable DPAPI vault was concurrently created with a different master key")
			}
			return nil
		}
		return fmt.Errorf("create portable DPAPI vault: %w", err)
	}

	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(payload); err != nil {
		return fmt.Errorf("write portable DPAPI vault: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("flush portable DPAPI vault: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close portable DPAPI vault: %w", err)
	}
	ok = true
	_ = windows.SetFileAttributes(windows.StringToUTF16Ptr(path), windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_NOT_CONTENT_INDEXED)
	return nil
}

type rawLocalState struct {
	top     map[string]json.RawMessage
	osCrypt map[string]json.RawMessage
}

func readRawLocalState(path string) (*rawLocalState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("parse Local State: %w", err)
	}
	osCrypt := make(map[string]json.RawMessage)
	if raw, ok := top["os_crypt"]; ok && len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &osCrypt); err != nil {
			return nil, fmt.Errorf("parse Local State os_crypt: %w", err)
		}
	}
	return &rawLocalState{top: top, osCrypt: osCrypt}, nil
}

func encryptedKeyString(state *rawLocalState) (string, error) {
	raw, ok := state.osCrypt["encrypted_key"]
	if !ok {
		return "", errEncryptedKeyMissing
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return "", fmt.Errorf("parse os_crypt.encrypted_key: %w", err)
	}
	if encoded == "" {
		return "", errEncryptedKeyMissing
	}
	return encoded, nil
}

func decryptMasterKeyFromLocalState(path string) ([]byte, error) {
	state, err := readRawLocalState(path)
	if err != nil {
		return nil, err
	}
	encoded, err := encryptedKeyString(state)
	if err != nil {
		return nil, err
	}
	wrapped, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode os_crypt.encrypted_key: %w", err)
	}
	if len(wrapped) <= len(dpapiPrefix) || !bytes.Equal(wrapped[:len(dpapiPrefix)], dpapiPrefix) {
		return nil, errors.New("os_crypt.encrypted_key does not use Chromium DPAPI format")
	}
	master, err := unprotectDPAPI(wrapped[len(dpapiPrefix):])
	if err != nil {
		return nil, err
	}
	if len(master) != masterKeySize {
		zeroBytes(master)
		return nil, fmt.Errorf("Chromium master key must be %d bytes", masterKeySize)
	}
	return master, nil
}

func atomicReplace(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".portable-local-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create Local State temp file: %w", err)
	}
	tempPath := file.Name()
	removeTemp := true
	defer func() {
		_ = file.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := file.Chmod(mode.Perm()); err != nil {
		return fmt.Errorf("set Local State temp permissions: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write Local State temp file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("flush Local State temp file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close Local State temp file: %w", err)
	}

	from, err := windows.UTF16PtrFromString(tempPath)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return fmt.Errorf("atomically replace Local State: %w", err)
	}
	removeTemp = false
	return nil
}

func rewriteLocalStateForCurrentWindows(path string, master []byte) error {
	if len(master) != masterKeySize {
		return fmt.Errorf("portable master key must be %d bytes", masterKeySize)
	}
	state, err := readRawLocalState(path)
	if err != nil {
		return err
	}
	protected, err := protectDPAPI(master)
	if err != nil {
		return err
	}
	defer zeroBytes(protected)
	wrapped := make([]byte, 0, len(dpapiPrefix)+len(protected))
	wrapped = append(wrapped, dpapiPrefix...)
	wrapped = append(wrapped, protected...)
	defer zeroBytes(wrapped)
	encoded := base64.StdEncoding.EncodeToString(wrapped)
	rawEncoded, err := json.Marshal(encoded)
	if err != nil {
		return err
	}
	state.osCrypt["encrypted_key"] = rawEncoded
	osCryptJSON, err := json.Marshal(state.osCrypt)
	if err != nil {
		return fmt.Errorf("marshal Local State os_crypt: %w", err)
	}
	state.top["os_crypt"] = osCryptJSON
	data, err := json.Marshal(state.top)
	if err != nil {
		return fmt.Errorf("marshal Local State: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return atomicReplace(path, data, info.Mode())
}

func PrepareProfile(dataDir, securityDir string) (PrepareStatus, error) {
	statePath := filepath.Join(dataDir, localStateFilename)
	vaultPath := filepath.Join(securityDir, vaultFilename)

	vaultMaster, vaultErr := loadVault(vaultPath)
	if vaultErr == nil {
		defer zeroBytes(vaultMaster)
		if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
			return StatusBootstrapNeeded, nil
		} else if err != nil {
			return StatusReady, err
		}
		if err := rewriteLocalStateForCurrentWindows(statePath, vaultMaster); err != nil {
			return StatusReady, err
		}
		verified, err := decryptMasterKeyFromLocalState(statePath)
		if err != nil {
			return StatusReady, fmt.Errorf("verify rewrapped Chromium master key: %w", err)
		}
		defer zeroBytes(verified)
		if !bytes.Equal(verified, vaultMaster) {
			return StatusReady, errors.New("rewrapped Chromium master key verification mismatch")
		}
		return StatusReady, nil
	}
	if !errors.Is(vaultErr, os.ErrNotExist) {
		return StatusReady, vaultErr
	}

	if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
		return StatusBootstrapNeeded, nil
	} else if err != nil {
		return StatusReady, err
	}

	master, err := decryptMasterKeyFromLocalState(statePath)
	if err != nil {
		if errors.Is(err, errEncryptedKeyMissing) {
			return StatusBootstrapNeeded, nil
		}
		if errors.Is(err, errDPAPIUnprotect) {
			return StatusReady, fmt.Errorf("%w: %v", ErrForeignProfileWithoutVault, err)
		}
		return StatusReady, err
	}
	defer zeroBytes(master)
	if err := storeVault(vaultPath, master); err != nil {
		return StatusReady, err
	}
	return StatusReady, nil
}

func CaptureProfileKey(dataDir, securityDir string) error {
	statePath := filepath.Join(dataDir, localStateFilename)
	vaultPath := filepath.Join(securityDir, vaultFilename)

	master, err := decryptMasterKeyFromLocalState(statePath)
	if err != nil {
		return fmt.Errorf("capture Chromium master key: %w", err)
	}
	defer zeroBytes(master)

	vaultMaster, err := loadVault(vaultPath)
	if err == nil {
		defer zeroBytes(vaultMaster)
		if !bytes.Equal(vaultMaster, master) {
			return errors.New("Chromium master key changed unexpectedly; portable vault was not overwritten")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return storeVault(vaultPath, master)
}
