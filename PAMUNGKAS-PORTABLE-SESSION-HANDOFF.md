# PAMUNGKAS Brave Portable — Portable Session Handoff

## START-HERE

Lifecycle: `EXISTING_PRODUCT_MAINTENANCE`

Primary repository: `pribadimartabat2/brave-portable`

Working branch: `pamungkas/portable-session-root-cause`

Fallback engine POC: `pribadimartabat2/brave-core`, branch `pamungkas/portable-oscrypt-poc`.

Goal: keep the official/native Brave binary, Chrome-extension compatibility, and one portable profile whose locally encrypted sessions remain decryptable when the same portable media moves between Windows computers.

Status remains **NO-GO for release** until real PC A -> PC B -> PC A evidence passes.

## ROOT CAUSE — PROVEN

Portapps already launches Brave with an explicit portable `--user-data-dir`, `--disable-machine-id`, and historical `--disable-encryption-win`.

For Chromium `153.0.8010.28`, which is the Chromium version pinned by the current Brave Core baseline used during this investigation:

1. App-Bound Encryption is not supported for a command-line-overridden/non-default user-data directory. This means a normal Portapps profile should not use App-Bound v20 for new encryption.
2. The active Windows provider is therefore primarily DPAPI v10.
3. `Local State -> os_crypt.encrypted_key` is Base64 of `DPAPI` plus a Windows-DPAPI-wrapped random 32-byte AES-256 master key.
4. Cookie/password ciphertext uses that master key.
5. On another Windows context, the DPAPI wrapper cannot be opened.
6. `os_crypt_async::Init()` treats DPAPI decryption failure as recoverable by generating a **new random 32-byte key and overwriting `os_crypt.encrypted_key`**.
7. The browser can therefore appear to start normally on PC B while the old cookie/session ciphertext has lost access to its original AES key.

This precisely explains the observed pattern: portable profile files move correctly, but logged-in sessions disappear after moving to another Windows installation.

## SMALLEST FIX — PRIMARY PATH

A custom Brave binary is not required for the first proof.

The launcher can preserve the same Chromium v10 AES master key and only change its Windows DPAPI wrapper for the current computer before Brave starts.

### Existing profile on original/decrypt-capable PC

1. Read only `Local State -> os_crypt.encrypted_key`.
2. Validate Base64 + `DPAPI` format.
3. Use Windows `CryptUnprotectData` to recover the exact 32-byte Chromium master key.
4. Save that master key into the portable security vault.
5. Do not read or export cookie/password values.

### Every later launch

1. Load the portable master key.
2. Protect the **same key** with the current Windows context using `CryptProtectData`.
3. Replace only `os_crypt.encrypted_key` with Base64(`DPAPI` + current wrapper).
4. Write `Local State` through an atomic same-directory replacement.
5. Verify that current DPAPI immediately decrypts the rewritten key back to the same 32 bytes.
6. Only then start Brave.

Result expected on PC B: Chromium sees a DPAPI blob it can open locally, but the plaintext AES master key is still exactly the key that encrypted the existing v10 cookies on PC A.

## FAIL-CLOSED CONTRACT

A moved profile with no portable vault is dangerous because starting Brave would allow Chromium to generate a replacement master key.

Therefore:

- if `Local State` contains a DPAPI key that the current Windows context cannot decrypt **and no portable vault exists**, the launcher must stop before `app.Launch()`;
- `Local State` must remain byte-untouched in that failure path;
- corrupt vaults must never be silently replaced;
- a vault containing a different key must never be overwritten;
- master-key values must never be written to logs or diagnostics.

## IMPLEMENTED

### Diagnostic layer

`brave-portable.exe --pamungkas-portability-diagnostic`

writes presence-only metadata to `diagnostics/portable-session.json`. It does not serialize cookie contents, password contents, or encryption-key values.

### DPAPI portability layer

`internal/portablecrypto/dpapi_windows.go`

Implements:

- current-Windows DPAPI protect/unprotect;
- Chromium `DPAPI` wrapper parsing;
- 32-byte master-key validation;
- portable vault creation/load;
- vault corruption detection using SHA-256;
- no-overwrite vault semantics;
- atomic `Local State` replacement with `MoveFileEx(..., REPLACE_EXISTING | WRITE_THROUGH)`;
- `PrepareProfile()` before browser start;
- `CaptureProfileKey()` after browser exit.

Portable vault format for this POC:

`PDP1` + 32-byte Chromium master key + SHA-256 integrity digest.

The vault intentionally detects corruption but does **not** claim theft resistance. The portable media should be protected by full-volume encryption. A later passphrase-wrapped vault can be added after portability itself is proven.

### Launcher integration

`main.go` now:

1. keeps diagnostic mode read-only;
2. calls `PrepareProfile()` before Brave startup;
3. blocks startup on foreign/unrecoverable existing profiles without a vault;
4. allows a genuinely fresh profile to bootstrap normally;
5. calls `CaptureProfileKey()` after Brave exits to create/verify the portable vault.

Existing Portapps extension/profile/registry mechanics remain otherwise unchanged.

## TDD EVIDENCE

The production implementation was preceded by Windows tests covering:

1. DPAPI protect/unprotect round trip;
2. portable-vault round trip;
3. corrupt vault rejection;
4. Local State rewrite preserves unrelated state and preserves the same AES master key;
5. existing original-PC profile creates the vault;
6. existing vault re-wraps the same key;
7. foreign profile without vault is blocked without mutating Local State;
8. fresh profile returns bootstrap-needed and captures the key after first-run simulation.

Latest observed targeted result before launcher integration:

- portable DPAPI tests: PASS;
- portable-session diagnostic tests: PASS;
- `go vet` portable DPAPI: PASS;
- `go vet` diagnostics: PASS.

Full launcher build after integration must still be confirmed before progressing to package/runtime evidence.

## BRAVE-CORE FALLBACK POC

A deeper `OSCryptAsync` portable provider POC exists in `pribadimartabat2/brave-core` PR #1.

It adds a custom `brp1` provider while retaining v10/v20 for legacy decryption. Its targeted helper/patch gate passed, including applying the hook against Chromium `153.0.8010.28`.

This path is now **fallback only** because the launcher DPAPI re-wrap approach is smaller, retains the official Brave binary, and directly addresses the proven Portapps v10 failure mode.

Do not merge or release the Brave Core fork unless launcher-only evidence proves insufficient.

## HARD RUNTIME GATE

No release-ready claim until tested on two physically distinct Windows contexts.

### PC A

1. Use a dedicated test copy/profile first.
2. Start through the modified portable launcher.
3. Confirm `security/portable-dpapi-master.pdp` exists after clean browser exit.
4. Install a Chrome Web Store extension.
5. Login to at least two controlled/ordinary sites with persistent cookies.
6. Close Brave normally and confirm no Brave process remains.

### PC B

1. Move the exact same portable folder/drive.
2. Start only through the portable launcher.
3. Confirm startup is not blocked.
4. Confirm extension remains enabled.
5. Confirm both controlled test-site sessions remain authenticated without re-login.
6. Close Brave normally.

### Return to PC A

1. Start through the launcher again.
2. Confirm sessions still work.
3. Confirm extension/settings remain intact.
4. Confirm no Local State/profile corruption.

Google, GitHub and ChatGPT may still perform server-side risk/device verification. That is separate from local cookie decryption. First prove controlled persistent-cookie sessions survive A -> B -> A.

## NON-REGRESSION

Must preserve:

- official Brave binary;
- Brave Shields;
- Chrome Web Store extensions;
- portable `user-data-dir`;
- Portapps registry import/export;
- diagnostic mode without mutation;
- normal clean shutdown/profile flush;
- existing Portapps package/update mechanics unless a bounded packaging change is proven necessary.

## CURRENT RELEASE STATUS

`NO-GO`.

Required next evidence:

1. full Go launcher build PASS after DPAPI integration;
2. Portapps package build PASS;
3. fresh-profile bootstrap runtime PASS;
4. original existing-profile vault capture PASS;
5. real PC A -> PC B -> PC A session retention PASS;
6. Chrome extension non-regression PASS;
7. corrupt/missing-vault failure behavior PASS;
8. only after those gates, prepare a user-test artifact and release/checksum set.
